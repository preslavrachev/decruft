package decruft

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// --- Already working ---

func TestExtractMeta_OGTags(t *testing.T) {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(`<html><head>
		<meta property="og:title" content="Article Title"/>
		<meta property="og:description" content="Article description text."/>
		<meta property="og:image" content="https://example.com/img.jpg"/>
	</head><body></body></html>`))

	result := &Result{}
	extractMeta(doc, result)

	if result.Title != "Article Title" {
		t.Errorf("title: got %q", result.Title)
	}
	if result.Description != "Article description text." {
		t.Errorf("description: got %q", result.Description)
	}
	if result.ImageURL != "https://example.com/img.jpg" {
		t.Errorf("image: got %q", result.ImageURL)
	}
}

func TestExtractMeta_TitleFallback(t *testing.T) {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(`<html><head>
		<title>Page Title From Tag</title>
	</head><body></body></html>`))

	result := &Result{}
	extractMeta(doc, result)

	if result.Title != "Page Title From Tag" {
		t.Errorf("title fallback: got %q", result.Title)
	}
}

func TestExtractContent_StripsNavAndHeader(t *testing.T) {
	html := `<html><body>
		<nav>Home About Contact Blog</nav>
		<header>Site Name</header>
		<article>
			<p>Real article content with enough words to be selected as main content.</p>
			<p>Second paragraph with more substance about the topic.</p>
		</article>
		<footer>Copyright 2024</footer>
	</body></html>`

	content := extractContent(bytes.NewReader([]byte(html)), true)

	for _, unwanted := range []string{"Home About Contact", "Site Name", "Copyright 2024"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("expected %q to be stripped, but appeared in output", unwanted)
		}
	}
	if !strings.Contains(content, "Real article content") {
		t.Errorf("expected article content in output, got %q", content)
	}
}

func TestExtractContent_StripsDisplayNone(t *testing.T) {
	html := `<html><body><article>
		<p>Real article content paragraph with sufficient words.</p>
		<div style="display:none"><p>Hidden element text should be stripped.</p></div>
	</article></body></html>`

	content := extractContent(bytes.NewReader([]byte(html)), true)

	if strings.Contains(content, "Hidden element text") {
		t.Error("expected display:none content to be stripped")
	}
}

// --- Gaps (red until fixed) ---

// JSON-LD: extractMeta should pick up articleBody from application/ld+json when
// DOM meta is absent. Many publishers (Substack, Medium, news sites) embed the
// full article text there.
func TestExtractMeta_JSONLDArticleBody(t *testing.T) {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(`<html><head>
		<script type="application/ld+json">
			{"@type":"Article","articleBody":"Full article text with lots of words about the topic."}
		</script>
	</head><body><p>Short teaser only.</p></body></html>`))

	result := &Result{}
	extractMeta(doc, result)

	if !strings.Contains(result.Description, "Full article text") {
		t.Errorf("expected JSON-LD articleBody in description, got %q", result.Description)
	}
}

// opacity:0 is not in our exactSelectors. Hidden elements with opacity:0 (popups,
// paywalled teasers, cookie banners) pollute extracted content.
// opacity:0 elements inside the content area should be stripped. Currently not
// in exactSelectors, so hidden overlays/popups embedded inside <article> leak
// into the extracted text.
func TestExtractContent_RemovesOpacityHiddenElements(t *testing.T) {
	html := `<html><body><article>
		<p>Real article paragraph one with enough words to be the main content.</p>
		<p>Real article paragraph two adding more substance to the piece.</p>
		<div style="opacity:0"><p>Hidden popup text that should not appear in output.</p></div>
	</article></body></html>`

	content := extractContent(bytes.NewReader([]byte(html)), true)

	if strings.Contains(content, "Hidden popup text") {
		t.Error("expected opacity:0 element to be removed, but its text appeared in output")
	}
}

// extractText only looks at <p>, then headings/li/blockquote. <pre>/<code> are
// skipped entirely, so technical articles lose all code samples.
func TestExtractText_IncludesCodeBlocks(t *testing.T) {
	s := sel(t, `<article>
		<p>Here is an example:</p>
		<pre><code>func main() { fmt.Println("hello world") }</code></pre>
	</article>`)

	got := extractText(s.Find("article"))

	if !strings.Contains(got, "func main") {
		t.Errorf("expected code block content in output, got %q", got)
	}
}

// Link density by character ratio: a block where ALL text is linked (nav-style)
// should score lower than prose with the same word count but no links.
// Our word-count ratio (links/words) is weak here — a nav with 3 long-phrase
// links has low link-per-word count but 100% character link density.
func TestScoreElement_LinkDensityByCharacterRatio(t *testing.T) {
	// All text is link text: character link density = 100%.
	nav := sel(t, `<div>
		<a href="/a">Introduction to machine learning and neural networks</a>
		<a href="/b">Advanced deep learning techniques and architectures</a>
		<a href="/c">Natural language processing and transformer models</a>
		<a href="/d">Computer vision object detection and segmentation</a>
	</div>`)
	navScore := scoreElement(nav.Find("div"))

	// Short prose with no links: character link density = 0%.
	prose := sel(t, `<p>Machine learning combines statistics and computer science to build systems that improve from experience without being explicitly programmed.</p>`)
	proseScore := scoreElement(prose.Find("p"))

	if navScore >= proseScore {
		t.Errorf("all-linked nav (score %d) should score lower than prose (score %d)", navScore, proseScore)
	}
}

// Comma presence signals prose — navigation and UI chrome rarely have commas.
// scoreElement does not currently count commas, so two otherwise identical
// paragraphs score identically.
func TestScoreElement_CommaBoost(t *testing.T) {
	withCommas := sel(t, `<p>The algorithm processes inputs, applies weights, computes gradients, and updates parameters iteratively.</p>`)
	withoutCommas := sel(t, `<p>The algorithm processes inputs applies weights computes gradients and updates parameters iteratively.</p>`)

	scoreWith := scoreElement(withCommas.Find("p"))
	scoreWithout := scoreElement(withoutCommas.Find("p"))

	if scoreWith <= scoreWithout {
		t.Errorf("prose with commas (score %d) should outscore prose without (score %d)", scoreWith, scoreWithout)
	}
}

// Content-pattern removal: a newsletter CTA inside a <p> bypasses our
// block-level scoring (blockElements only covers div/section/article/etc).
// The CTA text ends up in the extracted output.
func TestExtractContent_RemovesNewsletterCTAParagraph(t *testing.T) {
	html := `<html><body><article>
		<p>This is the first paragraph of a real article about an interesting topic.</p>
		<p>This is the second paragraph expanding on the ideas introduced above.</p>
		<p>Subscribe to our newsletter to get more great content delivered to your inbox every week!</p>
	</article></body></html>`

	content := extractContent(bytes.NewReader([]byte(html)), true)

	if strings.Contains(content, "Subscribe to our newsletter") {
		t.Error("expected newsletter CTA paragraph to be removed, but it appeared in output")
	}
}

func sel(t *testing.T, html string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc.Selection
}
