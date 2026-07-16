package comments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/lsy/blog/internal/domain"
	authmod "github.com/lsy/blog/internal/modules/auth"
	"github.com/lsy/blog/internal/modules/posts"
	"github.com/lsy/blog/internal/platform/ids"
	"github.com/lsy/blog/internal/platform/jobs"
	"github.com/lsy/blog/internal/platform/markdown"
	"github.com/lsy/blog/internal/shared/apperr"
)

const ModerationJobType = "comment_moderation"

type ModerationPayload struct {
	CommentID string    `json:"comment_id"`
	Revision  time.Time `json:"revision"`
}

type Repository struct {
	db       *gorm.DB
	producer *jobs.Producer
}

func NewRepository(db *gorm.DB, maxAttempts int) *Repository {
	return &Repository{db: db, producer: jobs.NewProducer(db, maxAttempts)}
}

// Create inserts the pending comment and its moderation task atomically.
func (r *Repository) Create(ctx context.Context, comment *domain.Comment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(comment).Error; err != nil {
			return err
		}
		_, err := r.producer.EnqueueTx(ctx, tx, ModerationJobType, ModerationPayload{
			CommentID: comment.PublicID,
			Revision:  comment.UpdatedAt,
		}, jobs.WithDedupKey("comment:"+comment.PublicID))
		return err
	})
}

func (r *Repository) FindByPublicID(ctx context.Context, publicID string) (*domain.Comment, error) {
	var comment domain.Comment
	err := r.db.WithContext(ctx).Preload("Author").Where("public_id = ? AND deleted_at IS NULL", publicID).First(&comment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &comment, err
}

func (r *Repository) ListByPost(ctx context.Context, postID uint64, page, pageSize int) ([]domain.Comment, int64, error) {
	var comments []domain.Comment
	var total int64
	query := r.db.WithContext(ctx).Model(&domain.Comment{}).
		Where("post_id = ? AND status = 'approved' AND deleted_at IS NULL AND parent_id IS NULL", postID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Preload("Author").Preload("Children", func(db *gorm.DB) *gorm.DB {
		return db.Where("status = 'approved' AND deleted_at IS NULL").Preload("Author").Order("created_at ASC, id ASC")
	}).Order("created_at ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&comments).Error
	return comments, total, err
}

// Update saves an edited comment and creates a new moderation task in the same
// transaction. The update timestamp gives each edited version a distinct key.
func (r *Repository) Update(ctx context.Context, comment *domain.Comment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(comment).Error; err != nil {
			return err
		}
		dedupKey := fmt.Sprintf("comment:%s:%d", comment.PublicID, comment.UpdatedAt.UnixNano())
		_, err := r.producer.EnqueueTx(ctx, tx, ModerationJobType, ModerationPayload{
			CommentID: comment.PublicID,
			Revision:  comment.UpdatedAt,
		}, jobs.WithDedupKey(dedupKey))
		return err
	})
}

// Moderate applies the Stage 1 deterministic policy: sanitized, non-empty
// comments are approved. Deleted or already moderated comments are idempotent.
func (r *Repository) Moderate(ctx context.Context, payloadJSON []byte) error {
	var payload ModerationPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return fmt.Errorf("comments: decode moderation payload: %w", err)
	}
	if payload.CommentID == "" || payload.Revision.IsZero() {
		return errors.New("comments: moderation payload requires comment_id and revision")
	}

	var comment domain.Comment
	err := r.db.WithContext(ctx).Where("public_id = ?", payload.CommentID).First(&comment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if comment.DeletedAt != nil || comment.Status != "pending" || !comment.UpdatedAt.Equal(payload.Revision) {
		return nil
	}
	if strings.TrimSpace(comment.BodyHTML) == "" {
		return errors.New("comments: sanitized comment body is empty")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	result := r.db.WithContext(ctx).Model(&domain.Comment{}).
		Where("id = ? AND status = 'pending' AND deleted_at IS NULL AND updated_at = ?", comment.ID, payload.Revision).
		Updates(map[string]interface{}{
			"status": "approved", "moderated_at": now, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	// A concurrent edit owns a newer moderation job; this stale execution is
	// intentionally a no-op rather than approving content it did not inspect.
	return nil
}

func (r *Repository) SoftDelete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&domain.Comment{}).Where("id = ?", id).Update("deleted_at", gorm.Expr("NOW(6)")).Error
}

type Service struct {
	repo      *Repository
	postsRepo *posts.Repository
	renderer  *markdown.Renderer
}

func NewService(repo *Repository, postsRepo *posts.Repository) *Service {
	return &Service{repo: repo, postsRepo: postsRepo, renderer: markdown.NewRenderer()}
}

type CreateCommentInput struct {
	BodyMarkdown string `json:"body_markdown"`
	ParentID     string `json:"parent_id"`
}

func (s *Service) Create(ctx *gin.Context, postSlug string, input CreateCommentInput) (*domain.Comment, error) {
	user := authmod.GetCurrentUser(ctx)
	if user == nil {
		return nil, apperr.Unauthorized("")
	}
	post, err := s.postsRepo.FindPostForComments(ctx.Request.Context(), postSlug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if post == nil || post.Status != "published" || post.Visibility != "public" {
		return nil, apperr.NotFound("post not found")
	}
	body := strings.TrimSpace(input.BodyMarkdown)
	if utf8.RuneCountInString(body) < 1 || utf8.RuneCountInString(body) > 5000 {
		return nil, apperr.Validation("comment must be 1-5000 characters", nil)
	}
	html, err := s.renderer.Render(body)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if strings.TrimSpace(html) == "" {
		return nil, apperr.Validation("comment must contain visible content", nil)
	}

	var parentID *uint64
	if input.ParentID != "" {
		parent, err := s.repo.FindByPublicID(ctx.Request.Context(), input.ParentID)
		if err != nil {
			return nil, apperr.Internal(err, "")
		}
		if parent == nil || parent.PostID != post.ID {
			return nil, apperr.Validation("parent comment does not belong to this post", nil)
		}
		if parent.ParentID != nil {
			return nil, apperr.Validation("comments support one reply level", nil)
		}
		parentID = &parent.ID
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	comment := &domain.Comment{
		PublicID: ids.MustNewULID(), PostID: post.ID, UserID: user.ID, ParentID: parentID,
		BodyMarkdown: body, BodyHTML: html, Status: "pending", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Create(ctx.Request.Context(), comment); err != nil {
		return nil, apperr.Internal(err, "")
	}
	comment.Author = user
	return comment, nil
}

func (s *Service) Update(ctx *gin.Context, commentID, body string) (*domain.Comment, error) {
	comment, err := s.repo.FindByPublicID(ctx.Request.Context(), commentID)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if comment == nil {
		return nil, apperr.NotFound("comment not found")
	}
	if err := s.authorizeComment(ctx, comment); err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	if utf8.RuneCountInString(body) < 1 || utf8.RuneCountInString(body) > 5000 {
		return nil, apperr.Validation("comment must be 1-5000 characters", nil)
	}
	html, err := s.renderer.Render(body)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if strings.TrimSpace(html) == "" {
		return nil, apperr.Validation("comment must contain visible content", nil)
	}
	comment.BodyMarkdown, comment.BodyHTML = body, html
	comment.Status = "pending"
	comment.ModeratedAt, comment.ModeratedBy = nil, nil
	comment.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	if err := s.repo.Update(ctx.Request.Context(), comment); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return comment, nil
}

func (s *Service) Delete(ctx *gin.Context, commentID string) error {
	comment, err := s.repo.FindByPublicID(ctx.Request.Context(), commentID)
	if err != nil {
		return apperr.Internal(err, "")
	}
	if comment == nil {
		return apperr.NotFound("comment not found")
	}
	if err := s.authorizeComment(ctx, comment); err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx.Request.Context(), comment.ID); err != nil {
		return apperr.Internal(err, "")
	}
	return nil
}

func (s *Service) ListByPost(ctx *gin.Context, postSlug string, page, pageSize int) (*domain.PagedComments, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	maxPage := int(^uint(0)>>1)/pageSize + 1
	if page > maxPage {
		return nil, apperr.Validation("page is too large", nil)
	}
	post, err := s.postsRepo.FindPostForComments(ctx.Request.Context(), postSlug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if post == nil || post.Status != "published" || post.Visibility != "public" {
		return nil, apperr.NotFound("post not found")
	}
	comments, total, err := s.repo.ListByPost(ctx.Request.Context(), post.ID, page, pageSize)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	return &domain.PagedComments{Comments: comments, Pagination: domain.Pagination{
		Page: page, PageSize: pageSize, Total: int(total), TotalPages: int(math.Ceil(float64(total) / float64(pageSize))),
	}}, nil
}

func (s *Service) authorizeComment(ctx *gin.Context, comment *domain.Comment) error {
	user := authmod.GetCurrentUser(ctx)
	if user == nil {
		return apperr.Unauthorized("")
	}
	if user.Role == "admin" || user.ID == comment.UserID {
		return nil
	}
	return apperr.Forbidden("")
}
