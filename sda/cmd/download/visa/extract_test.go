//go:build visas

package visa

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractDatasetID_Raw(t *testing.T) {
	value := "urn:sampledataset:gdi:testcase"

	id := ExtractDatasetID(value, "raw")

	assert.Equal(t, value, id)
}

func TestExtractDatasetID_Suffix(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "urn",
			value: "urn:sampledataset:gdi:testcase",
			want:  "testcase",
		},
		{
			name:  "url",
			value: "https://example.org/datasets/EGAD001",
			want:  "EGAD001",
		},
		{
			name:  "url-with-query",
			value: "https://example.org/datasets/EGAD001?foo=bar",
			want:  "EGAD001",
		},
		{
			name:  "plain",
			value: "EGAD001",
			want:  "EGAD001",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := ExtractDatasetID(tc.value, "suffix")
			assert.Equal(t, tc.want, id)
		})
	}
}
