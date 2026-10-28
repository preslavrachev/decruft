package decruft

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestResultMarkdownPreservesDocumentStructure(t *testing.T) {
	result := &Result{Content: `<article>
		<h1>Article title</h1>
		<h2>Section</h2>
		<h3>Detail</h3>
		<p>A <strong>bold</strong>, <em>emphasized</em>, and <code>inline()</code> paragraph with
			<a href="https://example.com/docs">a link</a>.</p>
		<img src="https://example.com/image.jpg" alt="Diagram">
		<ul><li>First bullet</li><li>Second bullet</li></ul>
		<ol><li>First step</li><li>Second step</li></ol>
		<blockquote><p>Quoted text.</p></blockquote>
		<pre><code class="language-go">func main() {}</code></pre>
		<table><thead><tr><th>Name</th><th>Value</th></tr></thead>
			<tbody><tr><td>Alpha</td><td>One</td></tr></tbody></table>
	</article>`}

	markdown, err := result.Markdown()
	if err != nil {
		t.Fatalf("convert Markdown: %v", err)
	}

	for _, want := range []string{
		"# Article title",
		"## Section",
		"### Detail",
		"**bold**",
		"*emphasized*",
		"`inline()`",
		"[a link](https://example.com/docs)",
		"![Diagram](https://example.com/image.jpg)",
		"- First bullet",
		"1. First step",
		"> Quoted text.",
		"```go",
		"| Name",
		"| Alpha",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("Markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestExtractResponseReturnsCleanedHTMLAndNormalizesURLs(t *testing.T) {
	body := []byte(`<html><head><base href="/reference/"></head><body>
		<nav>Navigation clutter</nav>
		<article>
			<h1>Article title</h1>
			<p>Readable article content with enough visible words for a useful extraction.</p>
			<p><a href="guide">Read the guide</a> and <a href="javascript:alert(1)">unsafe link</a>.</p>
			<img src="../images/diagram.png" alt="Diagram">
			<p>Subscribe to our newsletter to get more great content delivered to your inbox every week!</p>
		</article>
	</body></html>`)
	result := &Result{}
	responseURL, err := url.Parse("https://example.com/redirected/page")
	if err != nil {
		t.Fatalf("parse response URL: %v", err)
	}

	if err := extractDocument(body, result, responseURL, ""); err != nil {
		t.Fatalf("extract response: %v", err)
	}

	for _, want := range []string{
		"<article>",
		`href="https://example.com/reference/guide"`,
		`src="https://example.com/images/diagram.png"`,
	} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("Content missing %q:\n%s", want, result.Content)
		}
	}
	for _, unwanted := range []string{"Navigation clutter", "Subscribe to our newsletter", "javascript:"} {
		if strings.Contains(result.Content, unwanted) {
			t.Errorf("Content contains removed value %q:\n%s", unwanted, result.Content)
		}
	}
	if result.WordCount != visibleWordCount(result.Content) {
		t.Fatalf("WordCount: got %d, visible content has %d", result.WordCount, visibleWordCount(result.Content))
	}

	markdown, err := result.Markdown()
	if err != nil {
		t.Fatalf("convert Markdown: %v", err)
	}
	if !strings.Contains(markdown, "[Read the guide](https://example.com/reference/guide)") {
		t.Errorf("Markdown did not use normalized link:\n%s", markdown)
	}
	if strings.Contains(markdown, "javascript:") {
		t.Errorf("Markdown retained unsafe link:\n%s", markdown)
	}
}

func TestFetchUsesRedirectedResponseURLForRelativeContentURLs(t *testing.T) {
	fetcher := newTestFetcher(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/start" {
			return redirectResponse(request, "/articles/current/page"), nil
		}
		return htmlResponse(request, http.StatusOK, `<html><body><article>
			<p>Readable redirected article content.</p>
			<p><a href="next">Next page</a></p>
		</article></body></html>`), nil
	}))

	result, err := fetcher.Fetch(context.Background(), "https://example.com/start")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(result.Content, `href="https://example.com/articles/current/next"`) {
		t.Fatalf("Content did not use redirected response URL:\n%s", result.Content)
	}
}

func TestRelaxedCandidateDrivesHTMLAndMarkdown(t *testing.T) {
	longContent := "RELAXEDWINNER " + strings.Repeat("substantial article word ", 80)
	body := []byte(`<html><body><article>
		<p>Short retained introduction.</p>
		<div class="related"><p>` + longContent + `</p></div>
	</article></body></html>`)
	result := &Result{}

	if err := extractDocument(body, result, nil, ""); err != nil {
		t.Fatalf("extract response: %v", err)
	}
	if !strings.Contains(result.Content, "RELAXEDWINNER") {
		t.Fatalf("relaxed candidate did not win:\n%s", result.Content)
	}
	if result.WordCount < 200 {
		t.Fatalf("WordCount: got %d, want at least 200", result.WordCount)
	}

	markdown, err := result.Markdown()
	if err != nil {
		t.Fatalf("convert Markdown: %v", err)
	}
	if !strings.Contains(markdown, "RELAXEDWINNER") {
		t.Fatalf("Markdown did not use winning candidate.\nHTML:\n%s\nMarkdown:\n%s", result.Content, markdown)
	}
}

func TestBestContentCountsVisibleHTMLWords(t *testing.T) {
	content := "<article><p>" + strings.Repeat("<strong>visible</strong> ", minContentWords) + "</p></article>"
	result := &Result{
		Content:     content,
		Description: strings.Repeat("fallback ", minContentWords),
	}

	if got := result.BestContent(); got != content {
		t.Fatalf("BestContent: got %q, want cleaned HTML", got)
	}
}
