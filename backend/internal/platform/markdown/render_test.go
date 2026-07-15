package markdown

import (
	"strings"
	"testing"
)

func TestRenderConvertsMarkdownAndSanitizesHTML(t *testing.T) {
	html, err := NewRenderer().Render("# Hello\n\n**world**\n\n<script>alert(1)</script>")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if !strings.Contains(html, "<h1") || !strings.Contains(html, "<strong>world</strong>") {
		t.Fatalf("rendered HTML = %q", html)
	}
	if strings.Contains(strings.ToLower(html), "<script") {
		t.Fatalf("rendered HTML kept script: %q", html)
	}
}
