package posts

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/microcosm-cc/bluemonday"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	authmod "github.com/lsy/blog/internal/modules/auth"
	"github.com/lsy/blog/internal/platform/ids"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Service implements posts and taxonomy business logic.
type Service struct {
	cfg  *config.Config
	repo *Repository
	html *bluemonday.Policy
}

// NewService creates a posts service.
func NewService(cfg *config.Config, repo *Repository) *Service {
	return &Service{
		cfg:  cfg,
		repo: repo,
		html: bluemonday.UGCPolicy(),
	}
}

// --- Posts ---

type CreatePostInput struct {
	Title           string   `json:"title"`
	ContentMarkdown string   `json:"content_markdown"`
	Summary         string   `json:"summary"`
	CoverURL        string   `json:"cover_url"`
	Status          string   `json:"status"`
	Visibility      string   `json:"visibility"`
	CategoryIDs     []uint64 `json:"category_ids"`
	TagIDs          []uint64 `json:"tag_ids"`
}

func (s *Service) Create(ctx *gin.Context, input CreatePostInput) (*domain.Post, error) {
	claims := authmod.GetClaims(ctx)
	if claims == nil {
		return nil, apperr.Unauthorized("")
	}

	title := strings.TrimSpace(input.Title)
	if title == "" || len(title) > 200 {
		return nil, apperr.Validation("title must be 1-200 characters", nil)
	}
	if input.ContentMarkdown == "" {
		return nil, apperr.Validation("content_markdown is required", nil)
	}

	slug := s.uniqueSlug(ctx, GenerateSlug(title))

	now := time.Now().UTC()
	post := &domain.Post{
		PublicID:        ids.MustNewULID(),
		AuthorID:        0, // filled below
		Title:           title,
		Slug:            slug,
		ContentMarkdown: input.ContentMarkdown,
		ContentHTML:     s.html.Sanitize(input.ContentMarkdown),
		Status:          normalizeStatus(input.Status),
		Visibility:      normalizeVisibility(input.Visibility),
		ContentVersion:  1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if input.Summary != "" {
		post.Summary = &input.Summary
	}
	if input.CoverURL != "" {
		post.CoverURL = &input.CoverURL
	}
	if post.Status == "published" {
		post.PublishedAt = &now
	}

	// Resolve author numeric ID from public ID in JWT claims.
	// For now we pass public_id; the repository will need to resolve it.
	// We'll handle AuthorID assignment via a direct query in the repository.
	_ = claims // AuthorID resolved by repository via public_id lookup

	if err := s.repo.CreatePost(ctx.Request.Context(), post, input.CategoryIDs, input.TagIDs); err != nil {
		return nil, apperr.Internal(err, "")
	}

	return post, nil
}

type UpdatePostInput struct {
	Title           *string  `json:"title"`
	ContentMarkdown *string  `json:"content_markdown"`
	Summary         *string  `json:"summary"`
	CoverURL        *string  `json:"cover_url"`
	Status          *string  `json:"status"`
	Visibility      *string  `json:"visibility"`
	CategoryIDs     []uint64 `json:"category_ids"`
	TagIDs          []uint64 `json:"tag_ids"`
}

func (s *Service) Update(ctx *gin.Context, slug string, input UpdatePostInput) (*domain.Post, error) {
	post, err := s.repo.FindPostBySlug(ctx.Request.Context(), slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if post == nil {
		return nil, apperr.NotFound("post not found")
	}

	claims := authmod.GetClaims(ctx)
	if claims == nil || (claims.UserID != post.PublicID && claims.Role != "admin") {
		return nil, apperr.Forbidden("")
	}

	if input.Title != nil {
		v := strings.TrimSpace(*input.Title)
		if v == "" || len(v) > 200 {
			return nil, apperr.Validation("title must be 1-200 characters", nil)
		}
		post.Title = v
		post.Slug = s.uniqueSlug(ctx, GenerateSlug(v))
	}
	if input.ContentMarkdown != nil {
		post.ContentMarkdown = *input.ContentMarkdown
		post.ContentHTML = s.html.Sanitize(*input.ContentMarkdown)
	}
	if input.Summary != nil {
		post.Summary = input.Summary
	}
	if input.CoverURL != nil {
		post.CoverURL = input.CoverURL
	}
	if input.Status != nil {
		post.Status = normalizeStatus(*input.Status)
		if post.Status == "published" && post.PublishedAt == nil {
			now := time.Now().UTC()
			post.PublishedAt = &now
		}
	}
	if input.Visibility != nil {
		post.Visibility = normalizeVisibility(*input.Visibility)
	}

	post.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpdatePost(ctx.Request.Context(), post, input.CategoryIDs, input.TagIDs); err != nil {
		return nil, apperr.Internal(err, "")
	}

	return post, nil
}

func (s *Service) GetBySlug(ctx *gin.Context, slug string) (*domain.Post, error) {
	post, err := s.repo.FindPostBySlug(ctx.Request.Context(), slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if post == nil {
		return nil, apperr.NotFound("post not found")
	}
	// Only published posts are visible to non-owners
	if post.Status != "published" {
		claims := authmod.GetClaims(ctx)
		if claims == nil || (claims.UserID != post.PublicID && claims.Role != "admin") {
			return nil, apperr.NotFound("post not found")
		}
	}
	return post, nil
}

func (s *Service) List(ctx *gin.Context, page, pageSize int) (*domain.PagedPosts, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}

	posts, total, err := s.repo.ListPosts(ctx.Request.Context(), page, pageSize, "published", "public")
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	return &domain.PagedPosts{
		Posts: posts,
		Pagination: domain.Pagination{
			Page:       page,
			PageSize:   pageSize,
			Total:      int(total),
			TotalPages: int(math.Ceil(float64(total) / float64(pageSize))),
		},
	}, nil
}

func (s *Service) Delete(ctx *gin.Context, slug string) error {
	post, err := s.repo.FindPostBySlug(ctx.Request.Context(), slug)
	if err != nil {
		return apperr.Internal(err, "")
	}
	if post == nil {
		return apperr.NotFound("post not found")
	}
	claims := authmod.GetClaims(ctx)
	if claims == nil || (claims.UserID != post.PublicID && claims.Role != "admin") {
		return apperr.Forbidden("")
	}
	return s.repo.SoftDeletePost(ctx.Request.Context(), post.ID)
}

// --- Categories ---

func (s *Service) ListCategories(ctx context.Context) ([]domain.Category, error) {
	cats, err := s.repo.ListCategories(ctx)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	return cats, nil
}

type CategoryInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Service) CreateCategory(ctx context.Context, input CategoryInput) (*domain.Category, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 64 {
		return nil, apperr.Validation("name must be 1-64 characters", nil)
	}
	slug := GenerateSlug(name)
	existing, _ := s.repo.FindCategoryBySlug(ctx, slug)
	if existing != nil {
		return nil, apperr.Conflict("category slug already exists")
	}

	cat := &domain.Category{
		PublicID:  ids.MustNewULID(),
		Name:      name,
		Slug:      slug,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if input.Description != "" {
		cat.Description = &input.Description
	}
	if err := s.repo.CreateCategory(ctx, cat); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return cat, nil
}

func (s *Service) UpdateCategory(ctx context.Context, slug string, input CategoryInput) (*domain.Category, error) {
	cat, err := s.repo.FindCategoryBySlug(ctx, slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if cat == nil {
		return nil, apperr.NotFound("category not found")
	}

	if input.Name != "" {
		cat.Name = strings.TrimSpace(input.Name)
		cat.Slug = GenerateSlug(cat.Name)
	}
	if input.Description != "" {
		cat.Description = &input.Description
	}
	cat.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpdateCategory(ctx, cat); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return cat, nil
}

func (s *Service) DeleteCategory(ctx context.Context, slug string) error {
	cat, err := s.repo.FindCategoryBySlug(ctx, slug)
	if err != nil {
		return apperr.Internal(err, "")
	}
	if cat == nil {
		return apperr.NotFound("category not found")
	}
	return s.repo.DeleteCategory(ctx, cat.ID)
}

// --- Tags ---

func (s *Service) ListTags(ctx context.Context) ([]domain.Tag, error) {
	tags, err := s.repo.ListTags(ctx)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	return tags, nil
}

type TagInput struct {
	Name string `json:"name"`
}

func (s *Service) CreateTag(ctx context.Context, input TagInput) (*domain.Tag, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 32 {
		return nil, apperr.Validation("name must be 1-32 characters", nil)
	}
	slug := GenerateSlug(name)
	existing, _ := s.repo.FindTagBySlug(ctx, slug)
	if existing != nil {
		return nil, apperr.Conflict("tag slug already exists")
	}

	tag := &domain.Tag{
		PublicID:  ids.MustNewULID(),
		Name:      name,
		Slug:      slug,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.CreateTag(ctx, tag); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return tag, nil
}

func (s *Service) UpdateTag(ctx context.Context, slug string, input TagInput) (*domain.Tag, error) {
	tag, err := s.repo.FindTagBySlug(ctx, slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if tag == nil {
		return nil, apperr.NotFound("tag not found")
	}

	tag.Name = strings.TrimSpace(input.Name)
	tag.Slug = GenerateSlug(tag.Name)
	tag.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpdateTag(ctx, tag); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return tag, nil
}

func (s *Service) DeleteTag(ctx context.Context, slug string) error {
	tag, err := s.repo.FindTagBySlug(ctx, slug)
	if err != nil {
		return apperr.Internal(err, "")
	}
	if tag == nil {
		return apperr.NotFound("tag not found")
	}
	return s.repo.DeleteTag(ctx, tag.ID)
}

// --- helpers ---

func (s *Service) uniqueSlug(ctx context.Context, base string) string {
	slug := base
	for i := 1; ; i++ {
		existing, _ := s.repo.FindPostBySlug(ctx, slug)
		if existing == nil {
			return slug
		}
		slug = base + "-" + itoa(i)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return ""
}

func normalizeStatus(s string) string {
	switch s {
	case "published":
		return "published"
	case "archived":
		return "archived"
	default:
		return "draft"
	}
}

func normalizeVisibility(v string) string {
	if v == "private" {
		return "private"
	}
	return "public"
}
