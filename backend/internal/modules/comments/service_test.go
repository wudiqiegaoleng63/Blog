package comments

import (
	"encoding/json"
	"testing"
	"time"
)

func TestModerationPayloadRoundTrip(t *testing.T) {
	revision := time.Date(2026, time.July, 16, 12, 30, 0, 123000000, time.UTC)
	encoded, err := json.Marshal(ModerationPayload{CommentID: "01TESTCOMMENT", Revision: revision})
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	var decoded ModerationPayload
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if decoded.CommentID != "01TESTCOMMENT" {
		t.Fatalf("CommentID = %q", decoded.CommentID)
	}
	if !decoded.Revision.Equal(revision) {
		t.Fatalf("Revision = %s, want %s", decoded.Revision, revision)
	}
}

func TestModerationJobTypeIsStable(t *testing.T) {
	if ModerationJobType != "comment_moderation" {
		t.Fatalf("ModerationJobType = %q", ModerationJobType)
	}
}
