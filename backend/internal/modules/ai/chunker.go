// Package ai implements Stage 3 article indexing and Stage 4 retrieval-augmented
// question answering. Its interfaces are transport and provider independent.
package ai

import (
	"strings"
	"unicode/utf8"
)

// ChunkText deterministically splits text at paragraph boundaries when possible.
func ChunkText(text string, maxChars, overlapChars int) []string {
	if maxChars <= 0 {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	paragraphs := strings.Split(text, "\n\n")
	var units []string
	for _, paragraph := range paragraphs {
		paragraph = strings.Join(strings.Fields(paragraph), " ")
		if paragraph == "" {
			continue
		}
		runes := []rune(paragraph)
		for len(runes) > maxChars {
			units = append(units, string(runes[:maxChars]))
			runes = runes[maxChars:]
		}
		if len(runes) > 0 {
			units = append(units, string(runes))
		}
	}
	if len(units) == 0 {
		return nil
	}

	var chunks []string
	var current string
	flush := func() {
		current = strings.TrimSpace(current)
		if current == "" {
			return
		}
		chunks = append(chunks, current)
		current = ""
	}
	for _, unit := range units {
		candidate := unit
		if current != "" {
			candidate = current + "\n\n" + unit
		}
		if utf8.RuneCountInString(candidate) <= maxChars {
			current = candidate
			continue
		}
		flush()
		current = unit
	}
	flush()

	if overlapChars > 0 {
		for i := 1; i < len(chunks); i++ {
			previous := []rune(chunks[i-1])
			start := len(previous) - min(overlapChars, len(previous))
			prefix := strings.TrimSpace(string(previous[start:]))
			if prefix == "" {
				continue
			}
			combined := []rune(prefix + "\n\n" + chunks[i])
			if len(combined) > maxChars {
				combined = combined[:maxChars]
			}
			chunks[i] = strings.TrimSpace(string(combined))
		}
	}
	return chunks
}
