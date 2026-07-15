// Package openaicompat implements the small OpenAI-compatible HTTP surface used
// by the application. It deliberately depends on wire contracts rather than a
// provider SDK so self-hosted compatible endpoints remain supported.
package openaicompat

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
	"time"
)

const maxResponseBytes = 4 << 20

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	maxRetries int
}

func New(baseURL, apiKey string, timeout time.Duration, maxRetries int) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" {
		return nil, errors.New("openaicompat: base URL and API key are required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("openaicompat: base URL must be http or https")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &Client{
		baseURL: baseURL, apiKey: apiKey,
		httpClient: &http.Client{Timeout: timeout}, maxRetries: maxRetries,
	}, nil
}

type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
	Encoding   string   `json:"encoding_format"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *Client) Embed(ctx context.Context, model string, dimensions int, inputs []string) ([][]float32, error) {
	if strings.TrimSpace(model) == "" || dimensions <= 0 || len(inputs) == 0 {
		return nil, errors.New("openaicompat: model, dimensions and inputs are required")
	}
	var response embeddingResponse
	if err := c.post(ctx, "/embeddings", embeddingRequest{
		Model: model, Input: inputs, Dimensions: dimensions, Encoding: "float",
	}, &response); err != nil {
		return nil, fmt.Errorf("openaicompat: embeddings: %w", err)
	}
	if len(response.Data) != len(inputs) {
		return nil, fmt.Errorf("openaicompat: embeddings returned %d vectors for %d inputs", len(response.Data), len(inputs))
	}
	vectors := make([][]float32, len(inputs))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(inputs) || vectors[item.Index] != nil {
			return nil, errors.New("openaicompat: embeddings returned invalid indices")
		}
		if len(item.Embedding) != dimensions {
			return nil, fmt.Errorf("openaicompat: embedding dimension %d does not match configured %d", len(item.Embedding), dimensions)
		}
		vectors[item.Index] = item.Embedding
	}
	for _, vector := range vectors {
		if vector == nil {
			return nil, errors.New("openaicompat: embeddings response omitted an index")
		}
	}
	return vectors, nil
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

func (c *Client) Chat(ctx context.Context, model string, maxTokens int, messages []Message) (string, error) {
	if strings.TrimSpace(model) == "" || maxTokens <= 0 || len(messages) == 0 {
		return "", errors.New("openaicompat: model, max tokens and messages are required")
	}
	var response chatResponse
	if err := c.post(ctx, "/chat/completions", chatRequest{
		Model: model, Messages: messages, MaxTokens: maxTokens, Stream: false,
	}, &response); err != nil {
		return "", fmt.Errorf("openaicompat: chat: %w", err)
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", errors.New("openaicompat: chat returned no content")
	}
	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}

type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("provider returned HTTP %d: %s", e.StatusCode, e.Body)
}

func (c *Client) post(ctx context.Context, path string, requestBody, responseBody any) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<min(attempt-1, 5)) * 250 * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if !retryableTransport(err) {
				return err
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if len(body) > maxResponseBytes {
			return errors.New("provider response exceeds size limit")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if err := json.Unmarshal(body, responseBody); err != nil {
				return fmt.Errorf("decode provider response: %w", err)
			}
			return nil
		}
		statusErr := &StatusError{StatusCode: resp.StatusCode, Body: truncate(strings.TrimSpace(string(body)), 1000)}
		lastErr = statusErr
		if !retryableStatus(resp.StatusCode) {
			return statusErr
		}
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil && seconds > 0 && seconds <= 30 {
				timer := time.NewTimer(time.Duration(seconds) * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
	}
	return lastErr
}

func retryableTransport(err error) bool {
	return !errors.Is(err, context.Canceled)
}

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusConflict || status == http.StatusTooManyRequests || status >= 500
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
