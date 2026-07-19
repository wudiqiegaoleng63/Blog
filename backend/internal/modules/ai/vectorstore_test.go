package ai

import (
	"encoding/json"
	"testing"
)

func TestCollectionDimensionMatchesMilvusDescribeShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want bool
	}{
		{
			name: "Milvus 2.5 REST fields and params",
			data: `{"fields":[{"name":"embedding","params":[{"key":"dim","value":"64"}]}]}`,
			want: true,
		},
		{
			name: "nested schema element type params",
			data: `{"schema":{"fields":[{"fieldName":"embedding","elementTypeParams":{"dim":64}}]}}`,
			want: true,
		},
		{
			name: "object params",
			data: `{"fields":[{"name":"embedding","params":{"dim":"64"}}]}`,
			want: true,
		},
		{
			name: "dimension mismatch",
			data: `{"fields":[{"name":"embedding","params":[{"key":"dim","value":"1536"}]}]}`,
			want: false,
		},
		{
			name: "fractional dimension rejected",
			data: `{"fields":[{"name":"embedding","params":{"dim":64.5}}]}`,
			want: false,
		},
		{
			name: "embedding field missing",
			data: `{"fields":[{"name":"post_id","params":[{"key":"max_length","value":"26"}]}]}`,
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := collectionDimensionMatches(json.RawMessage(test.data), 64); got != test.want {
				t.Fatalf("collectionDimensionMatches() = %v, want %v", got, test.want)
			}
		})
	}
}
