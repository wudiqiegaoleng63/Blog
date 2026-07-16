package posts

import (
	"context"
	"errors"
	"strings"

	mysqlerr "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/lsy/blog/internal/domain"
	aimod "github.com/lsy/blog/internal/modules/ai"
	"github.com/lsy/blog/internal/platform/jobs"
)

// ErrPostSlugTaken means another post committed the same slug while the caller
// was creating or renaming a post.
var ErrPostSlugTaken = errors.New("posts: slug already taken")

// IsPostSlugConflict recognizes only the posts.slug unique constraint. Other
// duplicate keys (for example public_id) remain internal failures.
func IsPostSlugConflict(err error) bool {
	var mysqlErr *mysqlerr.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && strings.Contains(mysqlErr.Message, "uk_posts_slug")
}

// Repository provides database access for posts, categories, and tags.
type Repository struct {
	db           *gorm.DB
	jobProducer  *jobs.Producer
	indexEnabled bool
}

// NewRepository creates a posts repository.
func NewRepository(db *gorm.DB, options ...RepositoryOption) *Repository {
	r := &Repository{db: db}
	for _, option := range options {
		option(r)
	}
	return r
}

type RepositoryOption func(*Repository)

func WithIndexJobs(maxAttempts int) RepositoryOption {
	return func(r *Repository) {
		r.jobProducer = jobs.NewProducer(r.db, maxAttempts)
		r.indexEnabled = true
	}
}

// --- Posts ---

// CreatePost inserts a new post.
func (r *Repository) CreatePost(ctx context.Context, post *domain.Post, categoryIDs, tagIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(post).Error; err != nil {
			if IsPostSlugConflict(err) {
				return ErrPostSlugTaken
			}
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
		if r.indexEnabled {
			return aimod.EnqueueIndexTx(ctx, r.jobProducer, tx, post, "upsert")
		}
		return nil
	})
}

// UpdatePost updates an existing post and replaces its categories and tags.
func (r *Repository) UpdatePost(ctx context.Context, post *domain.Post, categoryIDs, tagIDs *[]uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(post).Error; err != nil {
			if IsPostSlugConflict(err) {
				return ErrPostSlugTaken
			}
			return err
		}
		if categoryIDs != nil {
			if err := tx.Exec("DELETE FROM post_categories WHERE post_id = ?", post.ID).Error; err != nil {
				return err
			}
			for _, categoryID := range *categoryIDs {
				if err := tx.Exec("INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)", post.ID, categoryID).Error; err != nil {
					return err
				}
			}
		}
		if tagIDs != nil {
			if err := tx.Exec("DELETE FROM post_tags WHERE post_id = ?", post.ID).Error; err != nil {
				return err
			}
			for _, tagID := range *tagIDs {
				if err := tx.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", post.ID, tagID).Error; err != nil {
					return err
				}
			}
		}
		if r.indexEnabled {
			return aimod.EnqueueIndexTx(ctx, r.jobProducer, tx, post, "upsert")
		}
		return nil
	})
}

// FindPostBySlug returns a post by its slug, optionally with relations.
func (r *Repository) FindPostBySlug(ctx context.Context, slug string) (*domain.Post, error) {
	var post domain.Post
	err := r.db.WithContext(ctx).
		Preload("Author").
		Preload("Categories").
		Preload("Tags").
		Where("slug = ? AND deleted_at IS NULL", slug).
		First(&post).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &post, err
}

// FindPostForComments loads only the fields needed to authorize comment
// access, avoiding relation preloads on the public comments hot path.
func (r *Repository) FindPostForComments(ctx context.Context, slug string) (*domain.Post, error) {
	var post domain.Post
	err := r.db.WithContext(ctx).
		Select("id", "status", "visibility").
		Where("slug = ? AND deleted_at IS NULL", slug).
		First(&post).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &post, err
}

// PostSlugExists checks all rows, including soft-deleted posts, because the
// database unique constraint reserves a slug for the lifetime of the row.
func (r *Repository) PostSlugExists(ctx context.Context, slug string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Unscoped().Model(&domain.Post{}).Where("slug = ?", slug).Count(&count).Error
	return count > 0, err
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
		Order("published_at DESC, id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&posts).Error
	return posts, total, err
}

// SoftDeletePost atomically soft-deletes a post and enqueues vector removal.
func (r *Repository) SoftDeletePost(ctx context.Context, post *domain.Post) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.Post{}).Where("id = ?", post.ID).Update("deleted_at", gorm.Expr("NOW(6)")).Error; err != nil {
			return err
		}
		if r.indexEnabled {
			return aimod.EnqueueIndexTx(ctx, r.jobProducer, tx, post, "delete")
		}
		return nil
	})
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

func (r *Repository) FindCategoryByName(ctx context.Context, name string) (*domain.Category, error) {
	var cat domain.Category
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&cat).Error
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

// FindCategoriesByPublicIDs returns categories matching the public API IDs.
func (r *Repository) FindCategoriesByPublicIDs(ctx context.Context, publicIDs []string) ([]domain.Category, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	var cats []domain.Category
	err := r.db.WithContext(ctx).Where("public_id IN ?", publicIDs).Find(&cats).Error
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

func (r *Repository) FindTagByName(ctx context.Context, name string) (*domain.Tag, error) {
	var tag domain.Tag
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&tag).Error
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

// FindTagsByPublicIDs returns tags matching the public API IDs.
func (r *Repository) FindTagsByPublicIDs(ctx context.Context, publicIDs []string) ([]domain.Tag, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	var tags []domain.Tag
	err := r.db.WithContext(ctx).Where("public_id IN ?", publicIDs).Find(&tags).Error
	return tags, err
}

// GenerateSlug returns a conservative ASCII slug. Callers must provide a
// non-empty fallback for titles that contain no ASCII letters or digits.
func GenerateSlug(title string) string {
	var builder strings.Builder
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if separator && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(r)
			separator = false
		default:
			separator = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
