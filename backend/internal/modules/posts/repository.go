package posts

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"

	"gorm.io/gorm"

	"github.com/lsy/blog/internal/domain"
)

// Repository provides database access for posts, categories, and tags.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a posts repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// --- Posts ---

// CreatePost inserts a new post.
func (r *Repository) CreatePost(ctx context.Context, post *domain.Post, categoryIDs, tagIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(post).Error; err != nil {
			return err
		}
		for _, cid := range categoryIDs {
			if err := tx.Exec("INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)", post.ID, cid).Error; err != nil {
				return err
			}
		}
		for _, tid := range tagIDs {
			if err := tx.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", post.ID, tid).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdatePost updates an existing post and replaces its categories and tags.
func (r *Repository) UpdatePost(ctx context.Context, post *domain.Post, categoryIDs, tagIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(post).Error; err != nil {
			return err
		}
		tx.Exec("DELETE FROM post_categories WHERE post_id = ?", post.ID)
		tx.Exec("DELETE FROM post_tags WHERE post_id = ?", post.ID)
		for _, cid := range categoryIDs {
			if err := tx.Exec("INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)", post.ID, cid).Error; err != nil {
				return err
			}
		}
		for _, tid := range tagIDs {
			if err := tx.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", post.ID, tid).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// FindPostBySlug returns a post by its slug, optionally with relations.
func (r *Repository) FindPostBySlug(ctx context.Context, slug string) (*domain.Post, error) {
	var post domain.Post
	err := r.db.WithContext(ctx).
		Preload("Categories").
		Preload("Tags").
		Where("slug = ? AND deleted_at IS NULL", slug).
		First(&post).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &post, err
}

// FindPostByPublicID returns a post by its public ID.
func (r *Repository) FindPostByPublicID(ctx context.Context, publicID string) (*domain.Post, error) {
	var post domain.Post
	err := r.db.WithContext(ctx).
		Where("public_id = ? AND deleted_at IS NULL", publicID).
		First(&post).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &post, err
}

// ListPosts returns a paginated list of published posts.
func (r *Repository) ListPosts(ctx context.Context, page, pageSize int, status, visibility string) ([]domain.Post, int64, error) {
	var posts []domain.Post
	var total int64

	query := r.db.WithContext(ctx).Model(&domain.Post{}).Where("deleted_at IS NULL")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if visibility != "" {
		query = query.Where("visibility = ?", visibility)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.
		Preload("Author").
		Preload("Categories").
		Preload("Tags").
		Order("published_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&posts).Error
	return posts, total, err
}

// SoftDeletePost sets deleted_at on a post by its ID.
func (r *Repository) SoftDeletePost(ctx context.Context, postID uint64) error {
	return r.db.WithContext(ctx).
		Model(&domain.Post{}).
		Where("id = ?", postID).
		Update("deleted_at", gorm.Expr("NOW(6)")).Error
}

// --- Categories ---

// ListCategories returns all categories.
func (r *Repository) ListCategories(ctx context.Context) ([]domain.Category, error) {
	var categories []domain.Category
	err := r.db.WithContext(ctx).Order("name ASC").Find(&categories).Error
	return categories, err
}

// FindCategoryBySlug returns a category by its slug.
func (r *Repository) FindCategoryBySlug(ctx context.Context, slug string) (*domain.Category, error) {
	var cat domain.Category
	err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&cat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &cat, err
}

// CreateCategory inserts a new category.
func (r *Repository) CreateCategory(ctx context.Context, cat *domain.Category) error {
	return r.db.WithContext(ctx).Create(cat).Error
}

// UpdateCategory updates an existing category.
func (r *Repository) UpdateCategory(ctx context.Context, cat *domain.Category) error {
	return r.db.WithContext(ctx).Save(cat).Error
}

// DeleteCategory removes a category by ID.
func (r *Repository) DeleteCategory(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&domain.Category{}, id).Error
}

// FindCategoriesByIDs returns categories matching the given IDs.
func (r *Repository) FindCategoriesByIDs(ctx context.Context, ids []uint64) ([]domain.Category, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var cats []domain.Category
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&cats).Error
	return cats, err
}

// --- Tags ---

// ListTags returns all tags.
func (r *Repository) ListTags(ctx context.Context) ([]domain.Tag, error) {
	var tags []domain.Tag
	err := r.db.WithContext(ctx).Order("name ASC").Find(&tags).Error
	return tags, err
}

// FindTagBySlug returns a tag by its slug.
func (r *Repository) FindTagBySlug(ctx context.Context, slug string) (*domain.Tag, error) {
	var tag domain.Tag
	err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&tag).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &tag, err
}

// CreateTag inserts a new tag.
func (r *Repository) CreateTag(ctx context.Context, tag *domain.Tag) error {
	return r.db.WithContext(ctx).Create(tag).Error
}

// UpdateTag updates an existing tag.
func (r *Repository) UpdateTag(ctx context.Context, tag *domain.Tag) error {
	return r.db.WithContext(ctx).Save(tag).Error
}

// DeleteTag removes a tag by ID.
func (r *Repository) DeleteTag(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&domain.Tag{}, id).Error
}

// FindTagsByIDs returns tags matching the given IDs.
func (r *Repository) FindTagsByIDs(ctx context.Context, ids []uint64) ([]domain.Tag, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var tags []domain.Tag
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&tags).Error
	return tags, err
}

// --- Slug generation ---

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// GenerateSlug creates a URL-safe slug from a title.
func GenerateSlug(title string) string {
	slug := strings.ToLower(title)
	slug = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == ' ' {
			return r
		}
		return ' '
	}, slug)
	slug = slugRE.ReplaceAllString(strings.TrimSpace(slug), "-")
	slug = strings.Trim(slug, "-")
	return slug
}
