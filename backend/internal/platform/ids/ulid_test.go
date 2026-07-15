package ids

import (
	"strings"
	"testing"
)

func TestNewULID_FormatAndUniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id, err := NewULID()
		if err != nil {
			t.Fatalf("NewULID error: %v", err)
		}
		if len(id) != ulidLen {
			t.Fatalf("len=%d want %d (id=%q)", len(id), ulidLen, id)
		}
		if strings.ToUpper(id) != id {
			t.Fatalf("ULID must be uppercase, got %q", id)
		}
		for _, r := range id {
			if !strings.ContainsRune(ulidAlphabet, r) {
				t.Fatalf("ULID contains invalid rune %q in %q", r, id)
			}
		}
		if seen[id] {
			t.Fatalf("duplicate ULID generated: %q", id)
		}
		seen[id] = true
	}
}

func TestNewULID_TimePrefixIsNonDecreasing(t *testing.T) {
	// 随机段不保证单调；ULID 只保证时间前缀按毫秒非递减。
	prev := ""
	for i := 0; i < 1000; i++ {
		id, err := NewULID()
		if err != nil {
			t.Fatalf("NewULID error: %v", err)
		}
		prefix := id[:10]
		if prefix < prev {
			t.Fatalf("ULID time prefix decreased at %d: %q < %q", i, prefix, prev)
		}
		prev = prefix
	}
}
