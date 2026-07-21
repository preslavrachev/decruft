package decruft

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestFindMainContent_NarrowsNoisyMainToArticle(t *testing.T) {
	doc := parseScorerTestDocument(t, fmt.Sprintf(`
		<html><body><main id="main">
			<article id="article"><p>%s</p></article>
			<div>%s</div>
		</main></body></html>`, words(80), words(200)))

	assertMainContentID(t, doc, "article")
}

func TestFindMainContent_DoesNotBroadenToHigherPriorityAncestor(t *testing.T) {
	doc := parseScorerTestDocument(t, fmt.Sprintf(`
		<html><body><div id="post">
			<article id="article"><p>%s</p></article>
			%s
		</div></body></html>`, words(200), strings.Repeat("<table></table>", 100)))

	assertMainContentID(t, doc, "article")
}

func TestFindMainContent_DoesNotJumpToUnrelatedCandidate(t *testing.T) {
	doc := parseScorerTestDocument(t, fmt.Sprintf(`
		<html><body>
			<main id="main"><p>%s</p></main>
			<div id="post"><p>%s</p></div>
		</body></html>`, words(1_000), words(60)))

	assertMainContentID(t, doc, "main")
}

func TestFindMainContent_ChainsThroughNestedCandidates(t *testing.T) {
	doc := parseScorerTestDocument(t, fmt.Sprintf(`
		<html><body id="body">
			<main id="main">
				<article id="article">
					<div class="entry-content" id="entry"><p>%s</p></div>
					<div>%s</div>
				</article>
				<div>%s</div>
			</main>
			<div>%s</div>
		</body></html>`, words(60), words(250), words(200), words(200)))

	assertMainContentID(t, doc, "entry")
}

func TestFindMainContent_KeepsParentForMultipleArticleCards(t *testing.T) {
	doc := parseScorerTestDocument(t, fmt.Sprintf(`
		<html><body><main id="main">
			<article id="first"><p>%s</p></article>
			<article id="second"><p>%s</p></article>
		</main></body></html>`, words(100), words(100)))

	assertMainContentID(t, doc, "main")
}

func TestFindMainContent_IgnoresShortHigherPriorityChild(t *testing.T) {
	doc := parseScorerTestDocument(t, fmt.Sprintf(`
		<html><body><main id="main">
			<div class="post-content" id="card"><p>%s</p></div>
			<div>%s</div>
		</main></body></html>`, words(10), words(600)))

	assertMainContentID(t, doc, "main")
}

func parseScorerTestDocument(t *testing.T, source string) *goquery.Document {
	t.Helper()

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		t.Fatalf("parsing test document: %v", err)
	}
	return doc
}

func assertMainContentID(t *testing.T, doc *goquery.Document, want string) {
	t.Helper()

	main := findMainContent(doc.Selection)
	if main == nil {
		t.Fatal("findMainContent returned nil")
	}
	if got := main.AttrOr("id", ""); got != want {
		t.Errorf("main content id: got %q, want %q", got, want)
	}
}

func words(count int) string {
	return strings.Repeat("word ", count)
}
