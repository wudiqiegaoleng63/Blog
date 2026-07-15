package openaicompat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbedOrdersByIndexAndValidatesDimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "embed" || body.Dimensions != 2 || len(body.Input) != 2 {
			t.Fatalf("unexpected body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()

	client, err := New(server.URL+"/v1", "secret", time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Embed(context.Background(), "embed", 2, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if vectors[0][0] != 1 || vectors[1][0] != 3 {
		t.Fatalf("vectors not ordered by index: %#v", vectors)
	}
}

func TestClientRetriesRetryableStatus(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"answer"}}]}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "secret", 2*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "answer" || calls.Load() != 2 {
		t.Fatalf("answer=%q calls=%d", answer, calls.Load())
	}
}

func TestClientDoesNotRetryOrdinaryBadRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "invalid", http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := New(server.URL, "secret", time.Second, 3)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Chat(context.Background(), "chat", 100, []Message{{Role: "user", Content: "hello"}})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}
