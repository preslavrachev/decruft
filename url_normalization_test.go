package decruft

import (
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestExtractResponseUsesDocumentBaseForMetadataImage(t *testing.T) {
	body := []byte(`<html><head>
		<base href="/assets/">
		<meta property="og:image" content=" cover.jpg ">
		<title>Example article</title>
	</head><body><article><p>Readable article content.</p></article></body></html>`)
	responseURL := mustParseURL(t, "https://example.com/articles/post")
	result := &Result{}

	if err := extractDocument(body, result, responseURL, ""); err != nil {
		t.Fatalf("extract response: %v", err)
	}
	if result.ImageURL != "https://example.com/assets/cover.jpg" {
		t.Fatalf("ImageURL = %q, want document-base URL", result.ImageURL)
	}
}

func TestFinalizeResultRejectsUnsafeOrInvalidMetadataImages(t *testing.T) {
	baseURL := mustParseURL(t, "https://example.com/articles/post")
	tests := []struct {
		name     string
		imageURL string
		want     string
	}{
		{name: "javascript", imageURL: "javascript:alert(1)"},
		{name: "lookalike scheme", imageURL: "httpwhatever:payload"},
		{name: "hostless HTTP", imageURL: "https:///missing-host"},
		{name: "malformed", imageURL: "https://example.com/%"},
		{name: "trimmed absolute", imageURL: " https://cdn.example.com/cover.jpg ", want: "https://cdn.example.com/cover.jpg"},
		{name: "relative", imageURL: "../cover.jpg", want: "https://example.com/cover.jpg"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := finalizeResult(&Result{Title: "Article", ImageURL: test.imageURL}, baseURL, baseURL)
			if err != nil {
				t.Fatalf("finalize result: %v", err)
			}
			if result.ImageURL != test.want {
				t.Fatalf("ImageURL = %q, want %q", result.ImageURL, test.want)
			}
		})
	}
}

func TestNormalizeContentURLsWritesCanonicalValues(t *testing.T) {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(`<article>
		<a id="link" href=" HTTPS://example.com/a path ">Link</a>
		<img id="image" src=" /cover image.jpg " alt="Cover">
	</article>`))
	if err != nil {
		t.Fatalf("parse HTML: %v", err)
	}
	content := document.Find("article")

	normalizeContentURLs(content, mustParseURL(t, "https://example.com/base/"))

	if got, _ := content.Find("#link").Attr("href"); got != "https://example.com/a%20path" {
		t.Fatalf("href = %q, want canonical URL", got)
	}
	if got, _ := content.Find("#image").Attr("src"); got != "https://example.com/cover%20image.jpg" {
		t.Fatalf("src = %q, want canonical URL", got)
	}
}

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse URL %q: %v", value, err)
	}
	return parsed
}
