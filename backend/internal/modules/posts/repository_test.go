package posts

import "testing"

func TestGenerateSlug(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{input: "Hello, World!", want: "hello-world"},
		{input: "  Go 1.26 ", want: "go-1-26"},
		{input: "中文标题", want: ""},
	} {
		if got := GenerateSlug(tc.input); got != tc.want {
			t.Fatalf("GenerateSlug(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizePublicIDs(t *testing.T) {
	got, ok := normalizePublicIDs([]string{" category-3 ", "category-3", "category-2"})
	if !ok || len(got) != 2 || got[0] != "category-3" || got[1] != "category-2" {
		t.Fatalf("normalizePublicIDs() = %#v, %v", got, ok)
	}
	if _, ok := normalizePublicIDs([]string{"category-3", " "}); ok {
		t.Fatal("normalizePublicIDs() accepted an empty public ID")
	}
}

func TestTruncateASCII(t *testing.T) {
	if got := truncateASCII("hello-world", 8); got != "hello-wo" {
		t.Fatalf("truncateASCII() = %q, want %q", got, "hello-wo")
	}
	if got := truncateASCII("hello----", 8); got != "hello" {
		t.Fatalf("truncateASCII() = %q, want %q", got, "hello")
	}
}
