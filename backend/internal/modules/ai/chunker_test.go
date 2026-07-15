package ai

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

func TestChunkTextDeterministicAndBounded(t *testing.T) {
	text := "第一段 内容 很长。\n\nSecond paragraph has words.\n\nThird paragraph."
	first := ChunkText(text, 24, 5)
	second := ChunkText(text, 24, 5)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("chunking is not deterministic: %#v %#v", first, second)
	}
	if len(first) < 2 {
		t.Fatalf("expected multiple chunks, got %#v", first)
	}
	for _, chunk := range first {
		if chunk == "" || utf8.RuneCountInString(chunk) > 24 {
			t.Fatalf("invalid chunk %q", chunk)
		}
	}
}

func TestChunkTextEmpty(t *testing.T) {
	if chunks := ChunkText(" \n\n ", 100, 10); len(chunks) != 0 {
		t.Fatalf("expected no chunks, got %#v", chunks)
	}
}
