package comments

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/microcosm-cc/bluemonday"
	"gorm.io/gorm"

	authmod "github.com/lsy/blog/internal/modules/auth"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/ids"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Repository provides database access for comments.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, c *domain.Comment) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *Repository) FindByPublicID(ctx context.Context, publicID string) (*domain.Comment, error) {
	var c domain.Comment
	err := r.db.WithContext(ctx).
		Preload("Author").
		Where("public_id = ? AND deleted_at IS NULL", publicID).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
}

func (r *Repository) ListByPost(ctx context.Context, postID uint64, page, pageSize int) ([]domain.Comment, int64, error) {
	var comments []domain.Comment
	var total int64

	query := r.db.WithContext(ctx).
		Model(&domain.Comment{}).
		Where("post_id = ? AND status = 'approved' AND deleted_at IS NULL AND parent_id IS NULL", postID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.
		Preload("Author").
		Preload("Children", func(db *gorm.DB) *gorm.DB {
			return db.Where("status = 'approved' AND deleted_at IS NULL").
				Preload("Author").
				Order("created_at ASC")
		}).
		Order("created_at ASC").
		Offset(offset).
		Limit(pageSize).
		Find(&comments).Error
	return comments, total, err
}

func (r *Repository) Update(ctx context.Context, c *domain.Comment) error {
	return r.db.WithContext(ctx).Save(c).Error
}

func (r *Repository) SoftDelete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).
		Model(&domain.Comment{}).
		Where("id = ?", id).
		Update("deleted_at", gorm.Expr("NOW(6)")).Error
}

// Service implements comment business logic.
type Service struct {
	repo *Repository
	html *bluemonday.Policy
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo, html: bluemonday.UGCPolicy()}
}

type CreateCommentInput struct {
	PostPublicID string `json:"post_public_id"`
	BodyMarkdown string `json:"body_markdown"`
	ParentID     string `json:"parent_id"`
}

func (s *Service) Create(ctx *gin.Context, input CreateCommentInput) (*domain.Comment, error) {
	claims := authmod.GetClaims(ctx)
	if claims == nil {
		return nil, apperr.Unauthorized("")
	}

	body := strings.TrimSpace(input.BodyMarkdown)
	if body == "" || len(body) > 5000 {
		return nil, apperr.Validation("comment must be 1-5000 characters", nil)
	}

	// Find user numeric ID from claims
	// For now we use the public ID; the repository resolves it via the user lookup
	now := time.Now().UTC()
	comment := &domain.Comment{
		PublicID:     ids.MustNewULID(),
		BodyMarkdown: body,
		BodyHTML:     s.html.Sanitize(body),
		Status:       "pending",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.repo.Create(ctx.Request.Context(), comment); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return comment, nil
}

func (s *Service) Update(ctx *gin.Context, commentID string, body string) (*domain.Comment, error) {
	c, err := s.repo.FindByPublicID(ctx.Request.Context(), commentID)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if c == nil {
		return nil, apperr.NotFound("comment not found")
	}

	claims := authmod.GetClaims(ctx)
	if claims == nil || (claims.UserID != c.PublicID && claims.Role != "admin") {
		return nil, apperr.Forbidden("")
	}

	body = strings.TrimSpace(body)
	if body == "" || len(body) > 5000 {
		return nil, apperr.Validation("comment must be 1-5000 characters", nil)
	}

	c.BodyMarkdown = body
	c.BodyHTML = s.html.Sanitize(body)
	c.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx.Request.Context(), c); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return c, nil
}

func (s *Service) Delete(ctx *gin.Context, commentID string) error {
	c, err := s.repo.FindByPublicID(ctx.Request.Context(), commentID)
	if err != nil {
		return apperr.Internal(err, "")
	}
	if c == nil {
		return apperr.NotFound("comment not found")
	}

	claims := authmod.GetClaims(ctx)
	if claims == nil || (claims.UserID != c.PublicID && claims.Role != "admin") {
		return apperr.Forbidden("")
	}
	return s.repo.SoftDelete(ctx.Request.Context(), c.ID)
}

func (s *Service) ListByPost(ctx *gin.Context, postID uint64, page, pageSize int) (*domain.PagedComments, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}

	comments, total, err := s.repo.ListByPost(ctx.Request.Context(), postID, page, pageSize)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	return &domain.PagedComments{
		Comments: comments,
		Pagination: domain.Pagination{
			Page:       page,
			PageSize:   pageSize,
			Total:      int(total),
			TotalPages: int(math.Ceil(float64(total) / float64(pageSize))),
		},
	}, nil
}
