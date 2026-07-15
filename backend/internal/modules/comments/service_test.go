package comments

import (
	"encoding/json"
	"testing"
)

func TestModerationPayloadRoundTrip(t *testing.T) {
	encoded, err := json.Marshal(ModerationPayload{CommentID: "01TESTCOMMENT"})
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
}

func TestModerationJobTypeIsStable(t *testing.T) {
	if ModerationJobType != "comment_moderation" {
		t.Fatalf("ModerationJobType = %q", ModerationJobType)
	}
}
