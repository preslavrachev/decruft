package decruft

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestBestHTMLHasUniformHTMLContract(t *testing.T) {
	substantialContent := "<article><p>" + strings.Repeat("content ", minContentWords) + "</p></article>"
	substantialDescription := "<script>alert('unsafe')</script> " + strings.Repeat("description ", minContentWords)

	tests := []struct {
		name   string
		result *Result
		want   string
	}{
		{
			name: "cleaned content",
			result: &Result{
				Content:   substantialContent,
				WordCount: minContentWords,
			},
			want: substantialContent,
		},
		{
			name: "escaped description fallback",
			result: &Result{
				Content:     "<p>thin</p>",
				WordCount:   1,
				Description: substantialDescription,
			},
			want: "<p>&lt;script&gt;alert(&#39;unsafe&#39;)&lt;/script&gt; " +
				strings.Repeat("description ", minContentWords) + "</p>",
		},
		{
			name:   "both thin",
			result: &Result{Content: "<p>thin</p>", WordCount: 1, Description: "short"},
			want:   "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.result.BestHTML(); got != test.want {
				t.Fatalf("BestHTML: got %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeURLAttributeRemovesEmptyAndHostlessHTTPURLs(t *testing.T) {
	content := sel(t, `<article>
		<a id="empty-link" href="">Empty</a>
		<a id="hostless-link" href="https:///missing-host">Hostless</a>
		<a id="valid-link" href="https://example.com/page">Valid</a>
		<a id="email-link" href="mailto:reader@example.com">Email</a>
		<img id="empty-image" src="" alt="Empty">
		<img id="hostless-image" src="http:/missing-host" alt="Hostless">
		<img id="valid-image" src="https://example.com/image.jpg" alt="Valid">
	</article>`).Find("article")

	normalizeContentURLs(content, nil)

	for _, selector := range []string{"#empty-link", "#hostless-link", "#empty-image", "#hostless-image"} {
		element := content.Find(selector)
		attribute := "href"
		if goquery.NodeName(element) == "img" {
			attribute = "src"
		}
		if _, exists := element.Attr(attribute); exists {
			t.Errorf("%s retained invalid %s", selector, attribute)
		}
	}
	for selector, want := range map[string]string{
		"#valid-link":  "https://example.com/page",
		"#email-link":  "mailto:reader@example.com",
		"#valid-image": "https://example.com/image.jpg",
	} {
		element := content.Find(selector)
		attribute := "href"
		if goquery.NodeName(element) == "img" {
			attribute = "src"
		}
		if got, _ := element.Attr(attribute); got != want {
			t.Errorf("%s %s: got %q, want %q", selector, attribute, got, want)
		}
	}
}
