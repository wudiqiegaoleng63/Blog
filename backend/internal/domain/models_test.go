package domain

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestGORMRelationships(t *testing.T) {
	postSchema, err := schema.Parse(&Post{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse Post schema: %v", err)
	}
	for field, joinTable := range map[string]string{"Categories": "post_categories", "Tags": "post_tags"} {
		relation := postSchema.Relationships.Relations[field]
		if relation == nil || relation.JoinTable == nil || relation.JoinTable.Table != joinTable {
			t.Fatalf("Post.%s join table = %#v, want %s", field, relation, joinTable)
		}
	}
	if _, err := schema.Parse(&Comment{}, &sync.Map{}, schema.NamingStrategy{}); err != nil {
		t.Fatalf("parse Comment schema: %v", err)
	}
}

func TestDomainJSONDoesNotLeakSecrets(t *testing.T) {
	encoded, err := json.Marshal(Post{Author: &User{ID: 42, Email: "private@example.test", PasswordHash: "secret-hash", TokenVersion: 7}})
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	text := string(encoded)
	for _, secret := range []string{"secret-hash", "private@example.test", "PasswordHash", "TokenVersion"} {
		if strings.Contains(text, secret) {
			t.Fatalf("JSON leaked %q: %s", secret, text)
		}
	}
}
