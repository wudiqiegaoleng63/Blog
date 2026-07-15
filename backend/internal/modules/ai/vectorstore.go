package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsy/blog/internal/config"
)

const maxMilvusResponseBytes = 8 << 20

type VectorChunk struct {
	ID             string
	PostID         string
	PostSlug       string
	ContentVersion uint64
	Index          int
	Text           string
	Embedding      []float32
}

type VectorHit struct {
	PostID         string
	PostSlug       string
	ContentVersion uint64
	Index          int
	Text           string
	Score          float32
}

type VectorStore interface {
	Ensure(context.Context) error
	ReplacePost(context.Context, string, []VectorChunk) error
	DeletePost(context.Context, string) error
	Search(context.Context, []float32, int) ([]VectorHit, error)
	Close(context.Context) error
}

// MilvusStore uses Milvus' stable v2 REST API. This keeps the modular monolith
// independent from Milvus server internals and their large transitive graph.
type MilvusStore struct {
	cfg        config.MilvusConfig
	dimensions int
	baseURL    string
	httpClient *http.Client
	mu         sync.Mutex
	ready      bool
}

func NewMilvusStore(cfg config.MilvusConfig, dimensions int) *MilvusStore {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Addr), "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}
	timeout := cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &MilvusStore{cfg: cfg, dimensions: dimensions, baseURL: baseURL, httpClient: &http.Client{Timeout: timeout}}
}

type milvusResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (s *MilvusStore) Ensure(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ready {
		return nil
	}
	if _, err := url.ParseRequestURI(s.baseURL); err != nil || s.dimensions <= 0 {
		return errors.New("ai: invalid Milvus address or dimensions")
	}
	var described milvusResponse
	err := s.call(ctx, "/v2/vectordb/collections/describe", map[string]any{"collectionName": s.cfg.CollectionName}, &described)
	if err != nil {
		var status *MilvusError
		if !errors.As(err, &status) || status.Code != 100 {
			return fmt.Errorf("ai: describe milvus collection: %w", err)
		}
		if err := s.createCollection(ctx); err != nil {
			return err
		}
	} else if !collectionDimensionMatches(described.Data, s.dimensions) {
		return fmt.Errorf("ai: Milvus collection embedding dimension does not match %d", s.dimensions)
	}
	var response milvusResponse
	if err := s.call(ctx, "/v2/vectordb/collections/load", map[string]any{"collectionName": s.cfg.CollectionName}, &response); err != nil {
		return fmt.Errorf("ai: load milvus collection: %w", err)
	}
	s.ready = true
	return nil
}

func (s *MilvusStore) createCollection(ctx context.Context) error {
	request := map[string]any{
		"collectionName": s.cfg.CollectionName,
		"schema": map[string]any{
			"autoId": false, "enableDynamicField": false,
			"fields": []map[string]any{
				{"fieldName": "chunk_id", "dataType": "VarChar", "isPrimary": true, "elementTypeParams": map[string]any{"max_length": 128}},
				{"fieldName": "post_id", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": 26}},
				{"fieldName": "post_slug", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": 200}},
				{"fieldName": "content_version", "dataType": "Int64"},
				{"fieldName": "chunk_index", "dataType": "Int64"},
				{"fieldName": "text", "dataType": "VarChar", "elementTypeParams": map[string]any{"max_length": 4096}},
				{"fieldName": "embedding", "dataType": "FloatVector", "elementTypeParams": map[string]any{"dim": s.dimensions}},
			},
		},
	}
	var response milvusResponse
	if err := s.call(ctx, "/v2/vectordb/collections/create", request, &response); err != nil {
		return fmt.Errorf("ai: create milvus collection: %w", err)
	}
	indexRequest := map[string]any{
		"collectionName": s.cfg.CollectionName,
		"indexParams":    []map[string]any{{"fieldName": "embedding", "indexName": "embedding_idx", "indexType": "AUTOINDEX", "metricType": "COSINE"}},
	}
	if err := s.call(ctx, "/v2/vectordb/indexes/create", indexRequest, &response); err != nil {
		return fmt.Errorf("ai: create milvus index: %w", err)
	}
	return nil
}

func (s *MilvusStore) ReplacePost(ctx context.Context, postID string, chunks []VectorChunk) error {
	if err := s.Ensure(ctx); err != nil {
		return err
	}
	if err := s.deletePostReady(ctx, postID); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}
	data := make([]map[string]any, len(chunks))
	for index, chunk := range chunks {
		if len(chunk.Embedding) != s.dimensions {
			return fmt.Errorf("ai: chunk %s has embedding dimension %d", chunk.ID, len(chunk.Embedding))
		}
		data[index] = map[string]any{
			"chunk_id": chunk.ID, "post_id": chunk.PostID, "post_slug": chunk.PostSlug,
			"content_version": chunk.ContentVersion, "chunk_index": chunk.Index,
			"text": chunk.Text, "embedding": chunk.Embedding,
		}
	}
	var response milvusResponse
	if err := s.call(ctx, "/v2/vectordb/entities/upsert", map[string]any{"collectionName": s.cfg.CollectionName, "data": data}, &response); err != nil {
		return fmt.Errorf("ai: upsert milvus chunks: %w", err)
	}
	return nil
}

func (s *MilvusStore) DeletePost(ctx context.Context, postID string) error {
	if err := s.Ensure(ctx); err != nil {
		return err
	}
	return s.deletePostReady(ctx, postID)
}

func (s *MilvusStore) deletePostReady(ctx context.Context, postID string) error {
	filter := `post_id == "` + escapeMilvusString(postID) + `"`
	var response milvusResponse
	if err := s.call(ctx, "/v2/vectordb/entities/delete", map[string]any{"collectionName": s.cfg.CollectionName, "filter": filter}, &response); err != nil {
		return fmt.Errorf("ai: delete milvus post: %w", err)
	}
	return nil
}

func (s *MilvusStore) Search(ctx context.Context, vector []float32, limit int) ([]VectorHit, error) {
	if len(vector) != s.dimensions || limit <= 0 {
		return nil, errors.New("ai: invalid search vector or limit")
	}
	if err := s.Ensure(ctx); err != nil {
		return nil, err
	}
	request := map[string]any{
		"collectionName": s.cfg.CollectionName, "annsField": "embedding", "data": [][]float32{vector}, "limit": limit,
		"outputFields": []string{"post_id", "post_slug", "content_version", "chunk_index", "text"},
		"searchParams": map[string]any{"metricType": "COSINE", "params": map[string]any{}},
	}
	var response milvusResponse
	if err := s.call(ctx, "/v2/vectordb/entities/search", request, &response); err != nil {
		return nil, fmt.Errorf("ai: search milvus: %w", err)
	}
	var rows []struct {
		PostID         string  `json:"post_id"`
		PostSlug       string  `json:"post_slug"`
		ContentVersion uint64  `json:"content_version"`
		ChunkIndex     int     `json:"chunk_index"`
		Text           string  `json:"text"`
		Distance       float32 `json:"distance"`
	}
	if len(response.Data) > 0 && string(response.Data) != "null" {
		if err := json.Unmarshal(response.Data, &rows); err != nil {
			return nil, fmt.Errorf("ai: decode milvus search: %w", err)
		}
	}
	hits := make([]VectorHit, len(rows))
	for i, row := range rows {
		hits[i] = VectorHit{PostID: row.PostID, PostSlug: row.PostSlug, ContentVersion: row.ContentVersion, Index: row.ChunkIndex, Text: row.Text, Score: row.Distance}
	}
	return hits, nil
}

func (s *MilvusStore) Close(context.Context) error { return nil }

type MilvusError struct {
	Code    int
	Message string
}

func (e *MilvusError) Error() string { return fmt.Sprintf("Milvus code %d: %s", e.Code, e.Message) }

func (s *MilvusStore) call(ctx context.Context, path string, request any, response *milvusResponse) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.Username != "" || s.cfg.Password != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Username+":"+s.cfg.Password)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMilvusResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxMilvusResponseBytes {
		return errors.New("Milvus response exceeds size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Milvus HTTP %d: %s", resp.StatusCode, truncateError(strings.TrimSpace(string(body)), 1000))
	}
	if err := json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("decode Milvus response: %w", err)
	}
	if response.Code != 0 {
		return &MilvusError{Code: response.Code, Message: response.Message}
	}
	return nil
}

func collectionDimensionMatches(data json.RawMessage, dimensions int) bool {
	var description struct {
		Schema struct {
			Fields []struct {
				Name              string         `json:"name"`
				FieldName         string         `json:"fieldName"`
				ElementTypeParams map[string]any `json:"elementTypeParams"`
				TypeParams        map[string]any `json:"typeParams"`
			} `json:"fields"`
		} `json:"schema"`
	}
	if json.Unmarshal(data, &description) != nil {
		return false
	}
	for _, field := range description.Schema.Fields {
		if field.Name != "embedding" && field.FieldName != "embedding" {
			continue
		}
		value := field.ElementTypeParams["dim"]
		if value == nil {
			value = field.TypeParams["dim"]
		}
		switch dim := value.(type) {
		case string:
			return dim == strconv.Itoa(dimensions)
		case float64:
			return int(dim) == dimensions
		}
	}
	return false
}

func escapeMilvusString(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}
