package decruft

import (
	"strings"
	"testing"
)

func TestResultBestContent(t *testing.T) {
	substantialContent := strings.Repeat("content ", minContentWords)
	substantialDescription := strings.Repeat("description ", minContentWords)
	thinContent := strings.Repeat("content ", minContentWords-1)

	tests := []struct {
		name   string
		result *Result
		want   string
	}{
		{
			name: "substantial content",
			result: &Result{
				Content:     substantialContent,
				Description: substantialDescription,
			},
			want: substantialContent,
		},
		{
			name: "description fallback",
			result: &Result{
				Content:     thinContent,
				Description: substantialDescription,
			},
			want: substantialDescription,
		},
		{
			name:   "both thin",
			result: &Result{Content: thinContent, Description: "short description"},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.BestContent(); got != tt.want {
				t.Fatalf("BestContent: got %q, want %q", got, tt.want)
			}
		})
	}
}
