//go:build integration

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/database"
	"github.com/lsy/blog/internal/platform/ids"
	"github.com/lsy/blog/internal/platform/jobs"
	"github.com/lsy/blog/internal/platform/migrations"
	"github.com/lsy/blog/internal/platform/openaicompat"
)

func TestMilvusIndexerAndRAGLifecycle(t *testing.T) {
	dsn := getenvRequired(t, "TEST_MYSQL_DSN")
	milvusAddr := getenvRequired(t, "TEST_MILVUS_ADDR")
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := database.New(ctx, config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 10, MaxIdleConns: 5,
		ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute,
	}, "dev")
	if err != nil {
		t.Fatalf("database.New(): %v", err)
	}
	defer database.Close(db)

	const dimensions = 64
	collection := "stage51_" + strings.ToLower(ids.MustNewULID())
	embeddingServer, chatServer := newMockAIServers(t, dimensions)
	defer embeddingServer.Close()
	defer chatServer.Close()

	embedder, err := openaicompat.New(embeddingServer.URL+"/v1", "integration-embedding-key", time.Second, 0)
	if err != nil {
		t.Fatalf("embedding client: %v", err)
	}
	chat, err := openaicompat.New(chatServer.URL+"/v1", "integration-chat-key", time.Second, 0)
	if err != nil {
		t.Fatalf("chat client: %v", err)
	}
	aiConfig := config.AIConfig{
		IndexingEnabled: true,
		RAGEnabled:      true,
		Embedding: config.EmbeddingConfig{
			Model: "integration-embedding", Dimensions: dimensions, BatchSize: 4,
		},
		Chat:     config.ChatModelConfig{Model: "integration-chat", MaxTokens: 100},
		Indexing: config.IndexingConfig{ChunkChars: 1200, ChunkOverlapChars: 0},
		RAG: config.RAGConfig{
			TopK: 10, FinalChunks: 4, MaxChunksPerPost: 3,
			ScoreThreshold: -1, MaxQuestionChars: 2000,
		},
	}
	vectors := NewMilvusStore(config.MilvusConfig{
		Addr: milvusAddr, CollectionName: collection, ConnectTimeout: 15 * time.Second,
	}, dimensions)
	defer vectors.Close(context.Background())
	if err := vectors.Ensure(ctx); err != nil {
		t.Fatalf("Milvus Ensure(): %v", err)
	}

	publicID := ids.MustNewULID()
	privateID := ids.MustNewULID()
	user := &domain.User{
		PublicID: publicID, Email: strings.ToLower("stage51-" + publicID + "@example.com"),
		EmailNormalized: strings.ToLower("stage51-" + publicID + "@example.com"),
		Username:        "stage51_" + strings.ToLower(publicID[:10]), PasswordHash: "integration-only",
		Role: "user", Status: "active", TokenVersion: 1,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	publicPost := newIntegrationPost(user.ID, publicID, "Public integration article", "The public article contains the verified answer.")
	privatePost := newIntegrationPost(user.ID, privateID, "Private integration article", "This private article must never become a source.")
	privatePost.Visibility = "private"
	if err := db.Create(publicPost).Error; err != nil {
		t.Fatalf("create public post: %v", err)
	}
	if err := db.Create(privatePost).Error; err != nil {
		t.Fatalf("create private post: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("author_id = ?", user.ID).Delete(&domain.Post{})
		db.Where("id = ?", user.ID).Delete(&domain.User{})
	})

	producer := jobs.NewProducer(db, 3)
	consumer := jobs.NewConsumer(db, config.JobsConfig{
		PollInterval: time.Millisecond, LockSeconds: 30, BatchSize: 1,
	})
	indexer := NewIndexer(NewRepository(db, 3), embedder, vectors, aiConfig)
	index := func(post *domain.Post, action string) {
		t.Helper()
		payload := IndexPayload{PostID: post.PublicID, ContentVersion: post.ContentVersion, Action: action}
		job, err := producer.Enqueue(ctx, IndexJobType, payload,
			jobs.WithDedupKey(fmt.Sprintf("stage51:%s:v%d:%s", post.PublicID, post.ContentVersion, action)),
			jobs.WithRunAfter(time.Now().UTC().Add(-time.Second)))
		if err != nil {
			t.Fatalf("enqueue %s: %v", action, err)
		}
		claimed, err := consumer.Claim(ctx)
		if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
			t.Fatalf("Claim() = %#v, %v", claimed, err)
		}
		if err := indexer.Handle(ctx, claimed[0].PayloadJSON); err != nil {
			t.Fatalf("Handle(%s): %v", action, err)
		}
		if err := consumer.Complete(ctx, claimed[0].ID); err != nil {
			t.Fatalf("Complete(%s): %v", action, err)
		}
	}

	index(publicPost, "upsert")
	waitForMilvusHit(t, vectors, unitVector(dimensions), func(hits []VectorHit) bool {
		for _, hit := range hits {
			if hit.PostID == publicPost.PublicID && hit.ContentVersion == 1 {
				return true
			}
		}
		return false
	})

	// Insert a private vector directly to verify MySQL authorization filtering,
	// rather than relying only on the indexer's eligibility check.
	if err := vectors.ReplacePost(ctx, privatePost.PublicID, []VectorChunk{{
		ID: stableChunkID(privatePost.PublicID, 1, 0), PostID: privatePost.PublicID,
		PostSlug: privatePost.Slug, ContentVersion: 1, Text: privatePost.ContentMarkdown,
		Embedding: unitVector(dimensions),
	}}); err != nil {
		t.Fatalf("insert private vector: %v", err)
	}
	rag := NewRAGService(db, embedder, chat, vectors, aiConfig)
	answer, err := rag.Ask(ctx, "What is the verified answer?")
	if err != nil {
		t.Fatalf("RAG Ask(): %v", err)
	}
	if answer.Answer == "" || len(answer.Sources) != 1 || answer.Sources[0].PostID != publicPost.PublicID {
		t.Fatalf("RAG response = %+v, want only public source", answer)
	}

	// An old job must become a no-op after the post version changes.
	if err := db.Model(&domain.Post{}).Where("id = ?", publicPost.ID).Updates(map[string]any{
		"content_markdown": "The updated public article contains the current answer.",
		"content_html":     "<p>The updated public article contains the current answer.</p>",
		"content_version":  2, "updated_at": time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("update public post: %v", err)
	}
	oldPayload, _ := json.Marshal(IndexPayload{PostID: publicPost.PublicID, ContentVersion: 1, Action: "upsert"})
	if err := indexer.Handle(ctx, oldPayload); err != nil {
		t.Fatalf("stale Handle(): %v", err)
	}
	hits, err := vectors.Search(ctx, unitVector(dimensions), 10)
	if err != nil {
		t.Fatalf("Search() after stale job: %v", err)
	}
	for _, hit := range hits {
		if hit.PostID == publicPost.PublicID && hit.ContentVersion != 1 {
			t.Fatalf("stale job changed vector version: %+v", hit)
		}
	}

	publicPost.ContentVersion = 2
	index(publicPost, "upsert")
	waitForMilvusHit(t, vectors, unitVector(dimensions), func(hits []VectorHit) bool {
		for _, hit := range hits {
			if hit.PostID == publicPost.PublicID && hit.ContentVersion == 2 && strings.Contains(hit.Text, "current answer") {
				return true
			}
		}
		return false
	})

	if err := vectors.DeletePost(ctx, privatePost.PublicID); err != nil {
		t.Fatalf("delete private vector: %v", err)
	}
	if err := db.Model(&domain.Post{}).Where("id = ?", publicPost.ID).Update("deleted_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("soft delete public post: %v", err)
	}
	publicPost.ContentVersion = 2
	index(publicPost, "delete")
	waitForMilvusHit(t, vectors, unitVector(dimensions), func(hits []VectorHit) bool {
		for _, hit := range hits {
			if hit.PostID == publicPost.PublicID {
				return false
			}
		}
		return true
	})
	var document domain.AIDocument
	if err := db.Where("post_id = ?", publicPost.ID).First(&document).Error; err != nil {
		t.Fatalf("load ai document: %v", err)
	}
	if document.Status != "removed" {
		t.Fatalf("ai document status = %q, want removed", document.Status)
	}
}

func newIntegrationPost(authorID uint64, publicID, title, content string) *domain.Post {
	now := time.Now().UTC()
	return &domain.Post{
		PublicID: publicID, AuthorID: authorID, Title: title,
		Slug: "stage51-" + strings.ToLower(publicID[:12]), ContentMarkdown: content,
		ContentHTML: "<p>" + content + "</p>", Status: "published", Visibility: "public",
		ContentVersion: 1, PublishedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
}

func newMockAIServers(t *testing.T, dimensions int) (*httptest.Server, *httptest.Server) {
	t.Helper()
	embedding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer integration-embedding-key" {
			http.Error(w, "invalid embedding request", http.StatusUnauthorized)
			return
		}
		var request struct {
			Model      string   `json:"model"`
			Input      []string `json:"input"`
			Dimensions int      `json:"dimensions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "integration-embedding" || request.Dimensions != dimensions || len(request.Input) == 0 {
			http.Error(w, "invalid embedding body", http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, len(request.Input))
		for index := range request.Input {
			data[index] = map[string]any{"index": index, "embedding": unitVector(dimensions)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer integration-chat-key" {
			http.Error(w, "invalid chat request", http.StatusUnauthorized)
			return
		}
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "integration-chat" || len(request.Messages) != 2 || !strings.Contains(request.Messages[0].Content, "untrusted data") {
			http.Error(w, "invalid chat body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{
			"role": "assistant", "content": "The verified answer is grounded in the public article.",
		}}}})
	}))
	return embedding, chat
}

func waitForMilvusHit(t *testing.T, store VectorStore, vector []float32, predicate func([]VectorHit) bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		hits, err := store.Search(context.Background(), vector, 10)
		if err == nil && predicate(hits) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	hits, err := store.Search(context.Background(), vector, 10)
	t.Fatalf("Milvus condition not met: hits=%+v err=%v", hits, err)
}

func unitVector(dimensions int) []float32 {
	vector := make([]float32, dimensions)
	vector[0] = 1
	return vector
}

func getenvRequired(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for integration tests", key)
	}
	return value
}
