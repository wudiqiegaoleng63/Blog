package posts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/domain"
	authmod "github.com/lsy/blog/internal/modules/auth"
	"github.com/lsy/blog/internal/platform/ids"
	"github.com/lsy/blog/internal/platform/markdown"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Service implements posts and taxonomy business logic.
type Service struct {
	repo     *Repository
	renderer *markdown.Renderer
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo, renderer: markdown.NewRenderer()}
}

type CreatePostInput struct {
	Title           string   `json:"title"`
	ContentMarkdown string   `json:"content_markdown"`
	Summary         string   `json:"summary"`
	CoverURL        string   `json:"cover_url"`
	Status          string   `json:"status"`
	Visibility      string   `json:"visibility"`
	CategoryIDs     []string `json:"category_ids"`
	TagIDs          []string `json:"tag_ids"`
}

func (s *Service) Create(ctx *gin.Context, input CreatePostInput) (*domain.Post, error) {
	author := authmod.GetCurrentUser(ctx)
	if author == nil {
		return nil, apperr.Unauthorized("")
	}

	title := strings.TrimSpace(input.Title)
	if utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 200 {
		return nil, apperr.Validation("title must be 1-200 characters", nil)
	}
	if strings.TrimSpace(input.ContentMarkdown) == "" {
		return nil, apperr.Validation("content_markdown is required", nil)
	}
	slug, err := s.uniqueSlug(ctx.Request.Context(), slugBase(title))
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	contentHTML, err := s.renderer.Render(input.ContentMarkdown)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if strings.TrimSpace(contentHTML) == "" {
		return nil, apperr.Validation("content_markdown must contain visible content", nil)
	}
	categoryIDs, tagIDs, err := s.validateTaxonomy(ctx.Request.Context(), input.CategoryIDs, input.TagIDs)
	if err != nil {
		return nil, err
	}
	status, ok := validStatusOrDefault(input.Status, "draft")
	if !ok {
		return nil, apperr.Validation("status must be draft, published, or archived", nil)
	}
	visibility, ok := validVisibilityOrDefault(input.Visibility, "public")
	if !ok {
		return nil, apperr.Validation("visibility must be public or private", nil)
	}

	now := time.Now().UTC()
	post := &domain.Post{
		PublicID: ids.MustNewULID(), AuthorID: author.ID, Title: title, Slug: slug,
		ContentMarkdown: input.ContentMarkdown, ContentHTML: contentHTML,
		Status: status, Visibility: visibility,
		ContentVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if input.Summary != "" {
		summary := strings.TrimSpace(input.Summary)
		if utf8.RuneCountInString(summary) > 500 {
			return nil, apperr.Validation("summary must not exceed 500 characters", nil)
		}
		post.Summary = &summary
	}
	if input.CoverURL != "" {
		coverURL := strings.TrimSpace(input.CoverURL)
		post.CoverURL = &coverURL
	}
	if post.Status == "published" {
		post.PublishedAt = &now
	}
	if err := s.repo.CreatePost(ctx.Request.Context(), post, categoryIDs, tagIDs); err != nil {
		if errors.Is(err, ErrPostSlugTaken) {
			return nil, apperr.Conflict("post slug is already in use; please retry")
		}
		return nil, apperr.Internal(err, "")
	}
	created, err := s.repo.FindPostBySlug(ctx.Request.Context(), post.Slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if created == nil {
		return nil, apperr.Internal(errors.New("posts: created post could not be reloaded"), "")
	}
	return created, nil
}

type UpdatePostInput struct {
	Title           *string   `json:"title"`
	ContentMarkdown *string   `json:"content_markdown"`
	Summary         *string   `json:"summary"`
	CoverURL        *string   `json:"cover_url"`
	Status          *string   `json:"status"`
	Visibility      *string   `json:"visibility"`
	CategoryIDs     *[]string `json:"category_ids"`
	TagIDs          *[]string `json:"tag_ids"`
}

func (s *Service) Update(ctx *gin.Context, slug string, input UpdatePostInput) (*domain.Post, error) {
	post, err := s.repo.FindPostBySlug(ctx.Request.Context(), slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if post == nil {
		return nil, apperr.NotFound("post not found")
	}
	if err := s.authorizePost(ctx, post); err != nil {
		return nil, err
	}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 200 {
			return nil, apperr.Validation("title must be 1-200 characters", nil)
		}
		if title != post.Title {
			base := slugBase(title)
			if base != post.Slug {
				newSlug, err := s.uniqueSlug(ctx.Request.Context(), base)
				if err != nil {
					return nil, apperr.Internal(err, "")
				}
				post.Slug = newSlug
			}
		}
		post.Title = title
	}
	if input.ContentMarkdown != nil {
		if strings.TrimSpace(*input.ContentMarkdown) == "" {
			return nil, apperr.Validation("content_markdown is required", nil)
		}
		html, err := s.renderer.Render(*input.ContentMarkdown)
		if err != nil {
			return nil, apperr.Internal(err, "")
		}
		if strings.TrimSpace(html) == "" {
			return nil, apperr.Validation("content_markdown must contain visible content", nil)
		}
		post.ContentMarkdown, post.ContentHTML = *input.ContentMarkdown, html
	}
	if input.Summary != nil {
		summary := strings.TrimSpace(*input.Summary)
		if utf8.RuneCountInString(summary) > 500 {
			return nil, apperr.Validation("summary must not exceed 500 characters", nil)
		}
		post.Summary = &summary
	}
	if input.CoverURL != nil {
		coverURL := strings.TrimSpace(*input.CoverURL)
		post.CoverURL = &coverURL
	}
	if input.Status != nil {
		status, ok := validStatus(*input.Status)
		if !ok {
			return nil, apperr.Validation("status must be draft, published, or archived", nil)
		}
		post.Status = status
		if status == "published" && post.PublishedAt == nil {
			now := time.Now().UTC()
			post.PublishedAt = &now
		}
	}
	if input.Visibility != nil {
		visibility, ok := validVisibility(*input.Visibility)
		if !ok {
			return nil, apperr.Validation("visibility must be public or private", nil)
		}
		post.Visibility = visibility
	}
	var categoryIDs, tagIDs *[]uint64
	if input.CategoryIDs != nil || input.TagIDs != nil {
		var categoryPublicIDs, tagPublicIDs []string
		if input.CategoryIDs != nil {
			categoryPublicIDs = *input.CategoryIDs
		}
		if input.TagIDs != nil {
			tagPublicIDs = *input.TagIDs
		}
		validatedCategories, validatedTags, err := s.validateTaxonomy(ctx.Request.Context(), categoryPublicIDs, tagPublicIDs)
		if err != nil {
			return nil, err
		}
		if input.CategoryIDs != nil {
			categoryIDs = &validatedCategories
		}
		if input.TagIDs != nil {
			tagIDs = &validatedTags
		}
	}
	post.ContentVersion++
	post.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdatePost(ctx.Request.Context(), post, categoryIDs, tagIDs); err != nil {
		if errors.Is(err, ErrPostSlugTaken) {
			return nil, apperr.Conflict("post slug is already in use; please retry")
		}
		return nil, apperr.Internal(err, "")
	}
	updated, err := s.repo.FindPostBySlug(ctx.Request.Context(), post.Slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if updated == nil {
		return nil, apperr.NotFound("post not found")
	}
	return updated, nil
}

func (s *Service) GetBySlug(ctx *gin.Context, slug string) (*domain.Post, error) {
	post, err := s.repo.FindPostBySlug(ctx.Request.Context(), slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if post == nil {
		return nil, apperr.NotFound("post not found")
	}
	if post.Status != "published" || post.Visibility != "public" {
		if err := s.authorizePost(ctx, post); err != nil {
			if appErr, ok := apperr.As(err); ok && (appErr.Code == apperr.CodeUnauthorized || appErr.Code == apperr.CodeForbidden) {
				return nil, apperr.NotFound("post not found")
			}
			return nil, err
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
	maxPage := int(^uint(0)>>1)/pageSize + 1
	if page > maxPage {
		return nil, apperr.Validation("page is too large", nil)
	}
	posts, total, err := s.repo.ListPosts(ctx.Request.Context(), page, pageSize, "published", "public")
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	return &domain.PagedPosts{Posts: posts, Pagination: domain.Pagination{
		Page: page, PageSize: pageSize, Total: int(total), TotalPages: int(math.Ceil(float64(total) / float64(pageSize))),
	}}, nil
}

func (s *Service) Delete(ctx *gin.Context, slug string) error {
	post, err := s.repo.FindPostBySlug(ctx.Request.Context(), slug)
	if err != nil {
		return apperr.Internal(err, "")
	}
	if post == nil {
		return apperr.NotFound("post not found")
	}
	if err := s.authorizePost(ctx, post); err != nil {
		return err
	}
	if err := s.repo.SoftDeletePost(ctx.Request.Context(), post); err != nil {
		return apperr.Internal(err, "")
	}
	return nil
}

func (s *Service) ListCategories(ctx context.Context) ([]domain.Category, error) {
	categories, err := s.repo.ListCategories(ctx)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	return categories, nil
}

type CategoryInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func (s *Service) CreateCategory(ctx context.Context, input CategoryInput) (*domain.Category, error) {
	name, err := validateTaxonomyName(input.Name, 64)
	if err != nil {
		return nil, err
	}
	if existing, err := s.repo.FindCategoryByName(ctx, name); err != nil {
		return nil, apperr.Internal(err, "")
	} else if existing != nil {
		return nil, apperr.Conflict("category name already exists")
	}
	slug := taxonomySlug(name)
	existing, err := s.repo.FindCategoryBySlug(ctx, slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if existing != nil {
		return nil, apperr.Conflict("category slug already exists")
	}
	now := time.Now().UTC()
	category := &domain.Category{PublicID: ids.MustNewULID(), Name: name, Slug: slug, CreatedAt: now, UpdatedAt: now}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(description) > 500 {
			return nil, apperr.Validation("description must not exceed 500 characters", nil)
		}
		category.Description = &description
	}
	if err := s.repo.CreateCategory(ctx, category); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return category, nil
}

func (s *Service) UpdateCategory(ctx context.Context, slug string, input CategoryInput) (*domain.Category, error) {
	category, err := s.repo.FindCategoryBySlug(ctx, slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if category == nil {
		return nil, apperr.NotFound("category not found")
	}
	if input.Name != "" {
		name, err := validateTaxonomyName(input.Name, 64)
		if err != nil {
			return nil, err
		}
		newSlug := category.Slug
		if name != category.Name {
			if existing, err := s.repo.FindCategoryByName(ctx, name); err != nil {
				return nil, apperr.Internal(err, "")
			} else if existing != nil && existing.ID != category.ID {
				return nil, apperr.Conflict("category name already exists")
			}
			newSlug = taxonomySlug(name)
		}
		if newSlug != category.Slug {
			existing, err := s.repo.FindCategoryBySlug(ctx, newSlug)
			if err != nil {
				return nil, apperr.Internal(err, "")
			}
			if existing != nil {
				return nil, apperr.Conflict("category slug already exists")
			}
		}
		category.Name, category.Slug = name, newSlug
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(description) > 500 {
			return nil, apperr.Validation("description must not exceed 500 characters", nil)
		}
		category.Description = &description
	}
	category.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateCategory(ctx, category); err != nil {
		return nil, apperr.Internal(err, "")
	}
	return category, nil
}

func (s *Service) DeleteCategory(ctx context.Context, slug string) error {
	category, err := s.repo.FindCategoryBySlug(ctx, slug)
	if err != nil {
		return apperr.Internal(err, "")
	}
	if category == nil {
		return apperr.NotFound("category not found")
	}
	if err := s.repo.DeleteCategory(ctx, category.ID); err != nil {
		return apperr.Internal(err, "")
	}
	return nil
}

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
	name, err := validateTaxonomyName(input.Name, 32)
	if err != nil {
		return nil, err
	}
	if existing, err := s.repo.FindTagByName(ctx, name); err != nil {
		return nil, apperr.Internal(err, "")
	} else if existing != nil {
		return nil, apperr.Conflict("tag name already exists")
	}
	slug := taxonomySlug(name)
	existing, err := s.repo.FindTagBySlug(ctx, slug)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if existing != nil {
		return nil, apperr.Conflict("tag slug already exists")
	}
	now := time.Now().UTC()
	tag := &domain.Tag{PublicID: ids.MustNewULID(), Name: name, Slug: slug, CreatedAt: now, UpdatedAt: now}
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
	name, err := validateTaxonomyName(input.Name, 32)
	if err != nil {
		return nil, err
	}
	newSlug := tag.Slug
	if name != tag.Name {
		if existing, err := s.repo.FindTagByName(ctx, name); err != nil {
			return nil, apperr.Internal(err, "")
		} else if existing != nil && existing.ID != tag.ID {
			return nil, apperr.Conflict("tag name already exists")
		}
		newSlug = taxonomySlug(name)
	}
	if newSlug != tag.Slug {
		existing, err := s.repo.FindTagBySlug(ctx, newSlug)
		if err != nil {
			return nil, apperr.Internal(err, "")
		}
		if existing != nil {
			return nil, apperr.Conflict("tag slug already exists")
		}
	}
	tag.Name, tag.Slug, tag.UpdatedAt = name, newSlug, time.Now().UTC()
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
	if err := s.repo.DeleteTag(ctx, tag.ID); err != nil {
		return apperr.Internal(err, "")
	}
	return nil
}

func (s *Service) validateTaxonomy(ctx context.Context, categoryPublicIDs, tagPublicIDs []string) ([]uint64, []uint64, error) {
	categoryPublicIDs, ok := normalizePublicIDs(categoryPublicIDs)
	if !ok {
		return nil, nil, apperr.Validation("category_ids must contain non-empty public IDs", nil)
	}
	tagPublicIDs, ok = normalizePublicIDs(tagPublicIDs)
	if !ok {
		return nil, nil, apperr.Validation("tag_ids must contain non-empty public IDs", nil)
	}
	categories, err := s.repo.FindCategoriesByPublicIDs(ctx, categoryPublicIDs)
	if err != nil {
		return nil, nil, apperr.Internal(err, "")
	}
	if len(categories) != len(categoryPublicIDs) {
		return nil, nil, apperr.Validation("one or more category_ids do not exist", nil)
	}
	tags, err := s.repo.FindTagsByPublicIDs(ctx, tagPublicIDs)
	if err != nil {
		return nil, nil, apperr.Internal(err, "")
	}
	if len(tags) != len(tagPublicIDs) {
		return nil, nil, apperr.Validation("one or more tag_ids do not exist", nil)
	}
	categoryIDs := make([]uint64, 0, len(categories))
	for _, category := range categories {
		categoryIDs = append(categoryIDs, category.ID)
	}
	tagIDs := make([]uint64, 0, len(tags))
	for _, tag := range tags {
		tagIDs = append(tagIDs, tag.ID)
	}
	return categoryIDs, tagIDs, nil
}

func (s *Service) uniqueSlug(ctx context.Context, base string) (string, error) {
	for suffix := 0; suffix < 1000; suffix++ {
		suffixText := ""
		if suffix > 0 {
			suffixText = fmt.Sprintf("-%d", suffix)
		}
		candidate := truncateASCII(base, 200-len(suffixText)) + suffixText
		exists, err := s.repo.PostSlugExists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", errors.New("posts: unable to allocate unique slug")
}

func slugBase(title string) string {
	if slug := GenerateSlug(title); slug != "" {
		return slug
	}
	return "post-" + strings.ToLower(ids.MustNewULID())
}

func taxonomySlug(name string) string {
	if slug := GenerateSlug(name); slug != "" {
		return slug
	}
	return "item-" + strings.ToLower(ids.MustNewULID())
}

func validateTaxonomyName(raw string, max int) (string, error) {
	name := strings.TrimSpace(raw)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > max {
		return "", apperr.Validation("name has invalid length", nil)
	}
	return name, nil
}

func normalizePublicIDs(values []string) ([]string, bool) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, false
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, true
}

func truncateASCII(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return strings.TrimRight(value[:max], "-")
}

func (s *Service) authorizePost(ctx *gin.Context, post *domain.Post) error {
	user := authmod.GetCurrentUser(ctx)
	if user == nil {
		return apperr.Unauthorized("")
	}
	if user.Role == "admin" || user.ID == post.AuthorID {
		return nil
	}
	return apperr.Forbidden("")
}

func validStatusOrDefault(raw, fallback string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return fallback, true
	}
	return validStatus(raw)
}

func validStatus(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "draft", "published", "archived":
		return strings.ToLower(strings.TrimSpace(raw)), true
	default:
		return "", false
	}
}

func validVisibilityOrDefault(raw, fallback string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return fallback, true
	}
	return validVisibility(raw)
}

func validVisibility(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "public", "private":
		return strings.ToLower(strings.TrimSpace(raw)), true
	default:
		return "", false
	}
}
