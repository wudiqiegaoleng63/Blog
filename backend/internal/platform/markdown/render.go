// Package markdown renders user-authored Markdown and sanitizes the generated HTML.
package markdown

import (
	"bytes"
	"fmt"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// Renderer is safe for concurrent use after construction.
type Renderer struct {
	markdown goldmark.Markdown
	policy   *bluemonday.Policy
}

func NewRenderer() *Renderer {
	return &Renderer{
		markdown: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		),
		policy: bluemonday.UGCPolicy(),
	}
}

func (r *Renderer) Render(source string) (string, error) {
	var output bytes.Buffer
	if err := r.markdown.Convert([]byte(source), &output); err != nil {
		return "", fmt.Errorf("markdown: render: %w", err)
	}
	return r.policy.Sanitize(output.String()), nil
}
