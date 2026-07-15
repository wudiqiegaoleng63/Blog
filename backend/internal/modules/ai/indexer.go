package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/jobs"
)

const IndexJobType = "post_index"

type IndexPayload struct {
	PostID         string `json:"post_id"`
	ContentVersion uint64 `json:"content_version"`
	Action         string `json:"action"`
}

type Embedder interface {
	Embed(context.Context, string, int, []string) ([][]float32, error)
}

type Repository struct {
	db       *gorm.DB
	producer *jobs.Producer
}

func NewRepository(db *gorm.DB, maxAttempts int) *Repository {
	return &Repository{db: db, producer: jobs.NewProducer(db, maxAttempts)}
}

func EnqueueIndexTx(ctx context.Context, producer *jobs.Producer, tx *gorm.DB, post *domain.Post, action string) error {
	if producer == nil || tx == nil || post == nil {
		return errors.New("ai: index enqueue dependencies are nil")
	}
	payload := IndexPayload{PostID: post.PublicID, ContentVersion: post.ContentVersion, Action: action}
	dedup := fmt.Sprintf("post:%s:v%d:%s", post.PublicID, post.ContentVersion, action)
	_, err := producer.EnqueueTx(ctx, tx, IndexJobType, payload, jobs.WithDedupKey(dedup), jobs.WithPriority(10))
	return err
}

func (r *Repository) Backfill(ctx context.Context) (int, error) {
	var posts []domain.Post
	if err := r.db.WithContext(ctx).Where("status = 'published' AND visibility = 'public' AND deleted_at IS NULL").Find(&posts).Error; err != nil {
		return 0, err
	}
	count := 0
	for i := range posts {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return EnqueueIndexTx(ctx, r.producer, tx, &posts[i], "upsert")
		})
		if err != nil && !duplicateJob(err) {
			return count, err
		}
		if err == nil {
			count++
		}
	}
	return count, nil
}

func (r *Repository) findPost(ctx context.Context, publicID string) (*domain.Post, error) {
	var post domain.Post
	err := r.db.WithContext(ctx).Unscoped().Where("public_id = ?", publicID).First(&post).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &post, err
}

func (r *Repository) mark(ctx context.Context, postID uint64, version uint64, model, status string, chunkCount int, indexedAt *time.Time, lastError *string) error {
	now := time.Now().UTC()
	document := domain.AIDocument{PostID: postID}
	updates := map[string]any{
		"content_version": version, "embedding_model": model, "chunk_count": chunkCount,
		"status": status, "indexed_at": indexedAt, "last_error": lastError, "updated_at": now,
	}
	return r.db.WithContext(ctx).Where("post_id = ?", postID).
		Assign(updates).Attrs(domain.AIDocument{CreatedAt: now}).FirstOrCreate(&document).Error
}

type Indexer struct {
	repo     *Repository
	embedder Embedder
	vectors  VectorStore
	cfg      config.AIConfig
}

func NewIndexer(repo *Repository, embedder Embedder, vectors VectorStore, cfg config.AIConfig) *Indexer {
	return &Indexer{repo: repo, embedder: embedder, vectors: vectors, cfg: cfg}
}

func (i *Indexer) Handle(ctx context.Context, payloadJSON []byte) error {
	var payload IndexPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return fmt.Errorf("ai: decode index payload: %w", err)
	}
	if payload.PostID == "" || payload.ContentVersion == 0 || (payload.Action != "upsert" && payload.Action != "delete") {
		return errors.New("ai: invalid index payload")
	}
	post, err := i.repo.findPost(ctx, payload.PostID)
	if err != nil {
		return err
	}
	if post == nil {
		return i.vectors.DeletePost(ctx, payload.PostID)
	}
	eligible := post.DeletedAt == nil && post.Status == "published" && post.Visibility == "public"
	if payload.Action == "delete" || !eligible {
		if err := i.vectors.DeletePost(ctx, payload.PostID); err != nil {
			return err
		}
		return i.repo.mark(ctx, post.ID, post.ContentVersion, i.cfg.Embedding.Model, "removed", 0, nil, nil)
	}
	if post.ContentVersion != payload.ContentVersion {
		return nil
	}

	text := post.Title
	if post.Summary != nil && strings.TrimSpace(*post.Summary) != "" {
		text += "\n\n" + strings.TrimSpace(*post.Summary)
	}
	text += "\n\n" + post.ContentMarkdown
	chunks := ChunkText(text, i.cfg.Indexing.ChunkChars, i.cfg.Indexing.ChunkOverlapChars)
	if len(chunks) == 0 {
		return errors.New("ai: post produced no indexable chunks")
	}
	vectors := make([][]float32, len(chunks))
	batchSize := i.cfg.Embedding.BatchSize
	for start := 0; start < len(chunks); start += batchSize {
		end := min(start+batchSize, len(chunks))
		batch, err := i.embedder.Embed(ctx, i.cfg.Embedding.Model, i.cfg.Embedding.Dimensions, chunks[start:end])
		if err != nil {
			message := truncateError(err.Error(), 1000)
			_ = i.repo.mark(context.Background(), post.ID, post.ContentVersion, i.cfg.Embedding.Model, "failed", 0, nil, &message)
			return err
		}
		copy(vectors[start:end], batch)
	}
	current, err := i.repo.findPost(ctx, payload.PostID)
	if err != nil {
		return err
	}
	if current == nil || current.DeletedAt != nil || current.Status != "published" || current.Visibility != "public" || current.ContentVersion != payload.ContentVersion {
		return nil
	}
	entries := make([]VectorChunk, len(chunks))
	for index := range chunks {
		entries[index] = VectorChunk{
			ID: stableChunkID(post.PublicID, payload.ContentVersion, index), PostID: post.PublicID,
			PostSlug: post.Slug, ContentVersion: payload.ContentVersion, Index: index,
			Text: chunks[index], Embedding: vectors[index],
		}
	}
	if err := i.vectors.ReplacePost(ctx, post.PublicID, entries); err != nil {
		message := truncateError(err.Error(), 1000)
		_ = i.repo.mark(context.Background(), post.ID, post.ContentVersion, i.cfg.Embedding.Model, "failed", 0, nil, &message)
		return err
	}
	indexedAt := time.Now().UTC()
	return i.repo.mark(ctx, post.ID, payload.ContentVersion, i.cfg.Embedding.Model, "indexed", len(chunks), &indexedAt, nil)
}

func stableChunkID(postID string, version uint64, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", postID, version, index)))
	return hex.EncodeToString(sum[:])
}

func duplicateJob(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate entry")
}

func truncateError(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
