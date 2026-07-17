package decruft

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

var reDate = regexp.MustCompile(`(?i)\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\s+\d{1,2},?\s+\d{4}\b`)
var reAuthor = regexp.MustCompile(`(?i)\b(?:by|written by|author:)\s+[A-Za-z\s]+\b`)

func scoreElement(s *goquery.Selection) int {
	text := s.Text()
	words := len(strings.Fields(text))
	score := words

	paragraphs := s.Find("p").Length()
	score += paragraphs * 10

	totalChars := len(strings.TrimSpace(text))
	var linkChars int
	s.Find("a").Each(func(_ int, a *goquery.Selection) {
		linkChars += len(strings.TrimSpace(a.Text()))
	})
	if totalChars > 0 {
		linkDensity := float64(linkChars) / float64(totalChars)
		score -= int(linkDensity * 50)
	}

	score += strings.Count(text, ",")

	images := s.Find("img").Length()
	if words > 0 {
		imageDensity := float64(images) / float64(words)
		score -= int(imageDensity * 3)
	}

	if reDate.MatchString(text) {
		score += 10
	}
	if reAuthor.MatchString(text) {
		score += 10
	}

	class := strings.ToLower(s.AttrOr("class", ""))
	id := strings.ToLower(s.AttrOr("id", ""))
	combined := class + " " + id

	for _, ind := range contentIndicators {
		if strings.Contains(combined, ind) {
			score += 15
			break
		}
	}

	for _, pat := range nonContentPatterns {
		if strings.Contains(combined, pat) {
			score -= 8
		}
	}

	nestedTables := s.Find("table").Length()
	score -= nestedTables * 5

	return score
}

func isLikelyContent(s *goquery.Selection) bool {
	role := s.AttrOr("role", "")
	if role == "article" || role == "main" || role == "contentinfo" {
		return true
	}

	class := strings.ToLower(s.AttrOr("class", ""))
	id := strings.ToLower(s.AttrOr("id", ""))
	combined := class + " " + id
	for _, ind := range contentIndicators {
		if strings.Contains(combined, ind) {
			return true
		}
	}

	words := len(strings.Fields(s.Text()))
	paragraphs := s.Find("p").Length()

	if words > 50 && paragraphs > 1 {
		return true
	}
	if words > 100 {
		return true
	}
	if words > 30 && paragraphs > 0 {
		return true
	}

	return false
}

func scoreNonContentBlock(s *goquery.Selection) int {
	text := strings.ToLower(strings.TrimSpace(s.Text()))
	words := len(strings.Fields(text))
	if words < 3 {
		return 0
	}

	score := 0

	for _, ind := range navigationIndicators {
		if strings.Contains(text, ind) {
			score -= 10
		}
	}

	links := s.Find("a").Length()
	if words > 0 {
		linkDensity := float64(links) / float64(words)
		if linkDensity > 0.5 {
			score -= 15
		}
	}

	lists := s.Find("ul").Length() + s.Find("ol").Length()
	if lists > 0 && links > lists*3 {
		score -= 10
	}

	class := strings.ToLower(s.AttrOr("class", ""))
	id := strings.ToLower(s.AttrOr("id", ""))
	combined := class + " " + id
	for _, pat := range nonContentPatterns {
		if strings.Contains(combined, pat) {
			score -= 8
		}
	}

	return score
}

// scoreAndRemove removes block elements that score negative and are not likely content.
func scoreAndRemove(root *goquery.Selection) {
	var toRemove []*goquery.Selection

	root.Find(strings.Join(blockElements, ",")).Each(func(_ int, s *goquery.Selection) {
		if isLikelyContent(s) {
			return
		}
		if scoreNonContentBlock(s) < 0 {
			toRemove = append(toRemove, s)
		}
	})

	for _, s := range toRemove {
		s.Remove()
	}
}

// findBestElement returns the highest-scoring element from a slice, or nil if
// none exceeds minScore.
func findBestElement(sels []*goquery.Selection, minScore int) *goquery.Selection {
	var best *goquery.Selection
	bestScore := minScore

	for _, s := range sels {
		if sc := scoreElement(s); sc > bestScore {
			bestScore = sc
			best = s
		}
	}

	return best
}

// findMainContent locates the best content element using entry-point selectors
// and content scoring, mirroring defuddle's findMainContent + findContentByScoring.
func findMainContent(root *goquery.Selection) *goquery.Selection {
	type candidate struct {
		sel   *goquery.Selection
		score int
		idx   int
	}

	var candidates []candidate

	for i, sel := range entryPointSelectors {
		root.Find(sel).Each(func(_ int, s *goquery.Selection) {
			score := (len(entryPointSelectors)-i)*40 + scoreElement(s)
			candidates = append(candidates, candidate{s, score, i})
		})
	}

	if len(candidates) == 0 {
		return findByScoring(root)
	}

	// Sort descending by score
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].score > candidates[j-1].score; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}

	top := candidates[0]
	best := top

	// Prefer a more-specific child that matched a higher-priority selector
	for _, c := range candidates[1:] {
		if c.idx < best.idx && nodeContains(best.sel.Get(0), c.sel.Get(0)) {
			best = c
		}
	}

	return best.sel
}

func findByScoring(root *goquery.Selection) *goquery.Selection {
	var best *goquery.Selection
	bestScore := 0

	root.Find(strings.Join(blockElements, ",")).Each(func(_ int, s *goquery.Selection) {
		if sc := scoreElement(s); sc > bestScore {
			bestScore = sc
			best = s
		}
	})

	return best
}

func nodeContains(ancestor, descendant *html.Node) bool {
	if ancestor == nil || descendant == nil {
		return false
	}
	n := descendant.Parent
	for n != nil {
		if n == ancestor {
			return true
		}
		n = n.Parent
	}
	return false
}
