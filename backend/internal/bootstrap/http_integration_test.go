//go:build integration

package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/modules/comments"
	"github.com/lsy/blog/internal/platform/jobs"
	"github.com/lsy/blog/internal/platform/migrations"
)

func TestHTTPBlogAndModerationFlow(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("TEST_MYSQL_DSN is required for integration tests")
	}
	redisAddr := os.Getenv("TEST_REDIS_ADDR")
	redisPassword := os.Getenv("TEST_REDIS_PASSWORD")
	if redisAddr == "" {
		t.Fatal("TEST_REDIS_ADDR is required for integration tests")
	}
	if err := migrations.RunUp(dsn); err != nil {
		t.Fatalf("RunUp(): %v", err)
	}
	t.Setenv("APP_ENV", "dev")
	t.Setenv("APP_SERVICE_MODE", "api")
	t.Setenv("MYSQL_DSN", dsn)
	t.Setenv("JWT_SECRET", "integration-test-jwt-secret-at-least-32-bytes")
	t.Setenv("HTTP_TRUSTED_PROXIES", "127.0.0.0/8")
	t.Setenv("REDIS_ADDR", redisAddr)
	t.Setenv("REDIS_PASSWORD", redisPassword)
	t.Setenv("REDIS_KEY_PREFIX", "blog:integration:")
	t.Setenv("REDIS_DIAL_TIMEOUT", "1s")
	t.Setenv("REDIS_READ_TIMEOUT", "1s")
	t.Setenv("REDIS_WRITE_TIMEOUT", "1s")
	t.Setenv("RATE_REGISTER_PER_MINUTE", "100")
	t.Setenv("RATE_LOGIN_PER_MINUTE", "100")
	t.Setenv("RATE_REFRESH_PER_MINUTE", "100")
	t.Setenv("RATE_COMMENT_PER_MINUTE", "100")
	t.Setenv("ARGON2_MEMORY_KIB", "8192")
	t.Setenv("ARGON2_ITERATIONS", "1")
	t.Setenv("ARGON2_PARALLELISM", "1")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load(): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	container, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("bootstrap.New(): %v", err)
	}
	defer container.Close(context.Background())
	if err := container.Redis.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush Redis: %v", err)
	}
	for _, statement := range []string{
		"SET FOREIGN_KEY_CHECKS=0",
		"DELETE FROM background_jobs",
		"DELETE FROM comments",
		"DELETE FROM posts",
		"DELETE FROM user_profiles",
		"DELETE FROM users",
		"SET FOREIGN_KEY_CHECKS=1",
	} {
		if err := container.DB.Exec(statement).Error; err != nil {
			t.Fatalf("clean database with %q: %v", statement, err)
		}
	}

	register := performJSON(t, container.Router(), http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": "integration@example.com", "username": "integration", "password": "safe-password-123",
	}, "", nil)
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	accessToken := nestedString(t, register.Body.Bytes(), "data", "access_token")
	originalRefresh := responseCookie(t, register, cfg.Auth.RefreshCookieName)

	me := performJSON(t, container.Router(), http.MethodGet, "/api/v1/auth/me", nil, accessToken, nil)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", me.Code, me.Body.String())
	}
	userPublicID := nestedString(t, me.Body.Bytes(), "data", "user", "public_id")

	refresh := performJSON(t, container.Router(), http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{originalRefresh})
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", refresh.Code, refresh.Body.String())
	}
	rotatedAccessToken := nestedString(t, refresh.Body.Bytes(), "data", "access_token")
	rotatedRefresh := responseCookie(t, refresh, cfg.Auth.RefreshCookieName)
	if rotatedRefresh.Value == originalRefresh.Value {
		t.Fatal("refresh token was not rotated")
	}

	replay := performJSON(t, container.Router(), http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{originalRefresh})
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("refresh replay status = %d body=%s", replay.Code, replay.Body.String())
	}
	revokedFamily := performJSON(t, container.Router(), http.MethodPost, "/api/v1/auth/refresh", nil, "", []*http.Cookie{rotatedRefresh})
	if revokedFamily.Code != http.StatusUnauthorized {
		t.Fatalf("revoked family status = %d body=%s", revokedFamily.Code, revokedFamily.Body.String())
	}

	if err := container.DB.Model(&domain.User{}).Where("public_id = ?", userPublicID).Update("role", "admin").Error; err != nil {
		t.Fatalf("promote user: %v", err)
	}
	adminRequest := performJSON(t, container.Router(), http.MethodPost, "/api/v1/categories", map[string]any{"name": "Integration Category"}, rotatedAccessToken, nil)
	if adminRequest.Code != http.StatusCreated {
		t.Fatalf("current role status = %d body=%s", adminRequest.Code, adminRequest.Body.String())
	}
	if err := container.DB.Model(&domain.User{}).Where("public_id = ?", userPublicID).Update("status", "suspended").Error; err != nil {
		t.Fatalf("suspend user: %v", err)
	}
	suspended := performJSON(t, container.Router(), http.MethodGet, "/api/v1/auth/me", nil, rotatedAccessToken, nil)
	if suspended.Code != http.StatusUnauthorized {
		t.Fatalf("suspended account status = %d body=%s", suspended.Code, suspended.Body.String())
	}
	if err := container.DB.Model(&domain.User{}).Where("public_id = ?", userPublicID).Updates(map[string]any{"status": "active", "role": "user"}).Error; err != nil {
		t.Fatalf("restore user: %v", err)
	}

	createPost := performJSON(t, container.Router(), http.MethodPost, "/api/v1/posts", map[string]any{
		"title": "Integration Post", "content_markdown": "# Verified\n\nThis is public.", "status": "published", "visibility": "public",
	}, accessToken, nil)
	if createPost.Code != http.StatusCreated {
		t.Fatalf("create post status = %d body=%s", createPost.Code, createPost.Body.String())
	}
	slug := nestedString(t, createPost.Body.Bytes(), "data", "post", "slug")

	createComment := performJSON(t, container.Router(), http.MethodPost, "/api/v1/posts/"+slug+"/comments", map[string]any{
		"body_markdown": "A useful comment",
	}, accessToken, nil)
	if createComment.Code != http.StatusCreated {
		t.Fatalf("create comment status = %d body=%s", createComment.Code, createComment.Body.String())
	}
	commentID := nestedString(t, createComment.Body.Bytes(), "data", "comment", "public_id")

	var job domain.Job
	if err := container.DB.Where("job_type = ?", comments.ModerationJobType).First(&job).Error; err != nil {
		t.Fatalf("find moderation job: %v", err)
	}
	consumer := jobs.NewConsumer(container.DB, cfg.Jobs)
	claimed, err := consumer.Claim(ctx)
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("Claim() = %#v, %v", claimed, err)
	}
	commentsRepo := comments.NewRepository(container.DB, cfg.Jobs.MaxAttempts)
	if err := commentsRepo.Moderate(ctx, claimed[0].PayloadJSON); err != nil {
		t.Fatalf("Moderate(): %v", err)
	}
	if err := consumer.Complete(ctx, claimed[0].ID); err != nil {
		t.Fatalf("Complete(): %v", err)
	}

	list := performJSON(t, container.Router(), http.MethodGet, "/api/v1/posts/"+slug+"/comments", nil, "", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list comments status = %d body=%s", list.Code, list.Body.String())
	}
	if got := nestedString(t, list.Body.Bytes(), "data", "comments", "0", "public_id"); got != commentID {
		t.Fatalf("approved comment public_id = %q, want %q", got, commentID)
	}
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatalf("response did not set non-empty cookie %q", name)
	return nil
}

func performJSON(t *testing.T, handler http.Handler, method, path string, body any, accessToken string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal(): %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func nestedString(t *testing.T, payload []byte, path ...string) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", payload, err)
	}
	for _, part := range path {
		switch current := value.(type) {
		case map[string]any:
			value = current[part]
		case []any:
			if part != "0" || len(current) == 0 {
				t.Fatalf("invalid array path %q in %v", part, path)
			}
			value = current[0]
		default:
			t.Fatalf("path %v does not resolve in %s", path, payload)
		}
	}
	result, ok := value.(string)
	if !ok || result == "" {
		t.Fatalf("path %v is not a non-empty string in %s", path, payload)
	}
	return result
}
