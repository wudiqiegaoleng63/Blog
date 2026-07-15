package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/observability"
)

func TestRequestIDPropagatesAndGenerates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID("X-Request-ID"))
	r.GET("/", func(c *gin.Context) {
		id, _ := c.Get("request_id")
		c.String(http.StatusOK, id.(string))
	})

	for _, tc := range []struct {
		name       string
		requestID  string
		wantExact  string
		wantNonNil bool
	}{
		{name: "propagates", requestID: "client-request-id", wantExact: "client-request-id"},
		{name: "generates", wantNonNil: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.requestID != "" {
				request.Header.Set("X-Request-ID", tc.requestID)
			}
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)

			got := response.Header().Get("X-Request-ID")
			if tc.wantExact != "" && got != tc.wantExact {
				t.Fatalf("X-Request-ID = %q, want %q", got, tc.wantExact)
			}
			if tc.wantNonNil && got == "" {
				t.Fatal("X-Request-ID is empty, want generated value")
			}
			if response.Body.String() != got {
				t.Fatalf("context request_id = %q, response header = %q", response.Body.String(), got)
			}
		})
	}
}

func TestCORSMaxAgeIsDecimalSeconds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS(config.CORSConfig{
		AllowedOrigins: []string{"https://example.test"},
		AllowedMethods: []string{http.MethodGet},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         12 * time.Hour,
	}))

	request := httptest.NewRequest(http.MethodOptions, "/", nil)
	request.Header.Set("Origin", "https://example.test")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Max-Age"); got != "43200" {
		t.Fatalf("Access-Control-Max-Age = %q, want %q", got, "43200")
	}
}

func TestMaxBodyRejectsOversizedRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MaxBody(4))
	r.POST("/", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	r.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestNewNoRouteUsesUnifiedErrorResponse(t *testing.T) {
	cfg := &config.Config{Observability: config.ObservabilityConfig{RequestIDHeader: "X-Request-ID"}}
	logger := &observability.Logger{Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	engine, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	response := httptest.NewRecorder()
	engine.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("body = %s, want unified not_found code", response.Body.String())
	}
}

func TestRecoveryIncludesRequestIDAndLogsStack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	logger := &observability.Logger{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	r := gin.New()
	r.Use(RequestID("X-Request-ID"), Recovery(logger))
	r.GET("/panic", func(*gin.Context) { panic("boom") })

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	request.Header.Set("X-Request-ID", "panic-request-id")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	var body struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.RequestID != "panic-request-id" {
		t.Fatalf("request_id = %q, want %q", body.RequestID, "panic-request-id")
	}
	if got := logs.String(); !strings.Contains(got, `"stack"`) || !strings.Contains(got, "panic-request-id") {
		t.Fatalf("panic log = %q, want request_id and stack", got)
	}
}

func TestErrorHandlerIncludesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := &observability.Logger{Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	r := gin.New()
	r.Use(RequestID("X-Request-ID"), ErrorHandler(logger))
	r.GET("/error", func(c *gin.Context) {
		_ = c.Error(errors.New("database detail"))
	})

	request := httptest.NewRequest(http.MethodGet, "/error", nil)
	request.Header.Set("X-Request-ID", "error-request-id")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)

	var body struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.RequestID != "error-request-id" {
		t.Fatalf("request_id = %q, want %q", body.RequestID, "error-request-id")
	}
	if strings.Contains(response.Body.String(), "database detail") {
		t.Fatalf("response leaked internal error: %s", response.Body.String())
	}
}
