package decruft

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
)

const minContentWords = 50

type Result struct {
	URL         string
	Title       string
	Description string
	ImageURL    string
	Domain      string
	Content     string
}

// BestContent returns the richest text available: full Content if substantial,
// Description as fallback (may hold JSON-LD articleBody), or "" when both are
// thin so callers can fall back to their own metadata (e.g. RSS description).
func (r *Result) BestContent() string {
	if len(strings.Fields(r.Content)) >= minContentWords {
		return r.Content
	}
	if len(strings.Fields(r.Description)) >= minContentWords {
		return r.Description
	}
	return ""
}

type Fetcher interface {
	Fetch(ctx context.Context, url string) (*Result, error)
}

type collyFetcher struct {
	collector *colly.Collector
}

func New() *collyFetcher {
	c := colly.NewCollector(
		colly.MaxDepth(1),
		colly.Async(true),
	)

	c.UserAgent = "Mozilla/5.0 (compatible; Decruft/1.0)"
	c.AllowURLRevisit = true
	c.SetRequestTimeout(10 * time.Second)
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 3,
		Delay:       500 * time.Millisecond,
		RandomDelay: 250 * time.Millisecond,
	})
	c.WithTransport(&http.Transport{DialContext: safeDialContext})

	return &collyFetcher{collector: c}
}

func (f *collyFetcher) Fetch(ctx context.Context, rawURL string) (*Result, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	result := &Result{URL: rawURL, Domain: parsed.Hostname()}
	var fetchErr error

	c := f.collector.Clone()

	c.OnResponse(func(r *colly.Response) {
		body := r.Body

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
		if err != nil {
			fetchErr = err
			return
		}

		extractMeta(doc, result)

		// Full extraction with clutter removal
		content := extractContent(bytes.NewReader(body), true)

		// If the result is thin, retry without partial-selector removal
		if wordCount(content) < 200 {
			if retry := extractContent(bytes.NewReader(body), false); wordCount(retry) > wordCount(content) {
				content = retry
			}
		}

		result.Content = content
	})

	c.OnError(func(r *colly.Response, err error) {
		if r != nil && r.StatusCode != 0 {
			fetchErr = fmt.Errorf("HTTP %d: %w", r.StatusCode, err)
		} else {
			fetchErr = err
		}
	})

	if err := c.Visit(rawURL); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	done := make(chan struct{})
	go func() {
		c.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if fetchErr != nil {
		return nil, fetchErr
	}

	if result.Title == "" && result.Content == "" {
		return nil, fmt.Errorf("no usable content found at URL")
	}

	// Resolve relative image URL
	if result.ImageURL != "" && !strings.HasPrefix(result.ImageURL, "http") {
		if ref, err := url.Parse(result.ImageURL); err == nil {
			result.ImageURL = parsed.ResolveReference(ref).String()
		}
	}

	return result, nil
}

// extractMeta reads OG, Twitter Card, <title>, and <meta name="description"> tags.
func extractMeta(doc *goquery.Document, result *Result) {
	var inArticleBlock bool

	doc.Find("meta").Each(func(_ int, s *goquery.Selection) {
		content, _ := s.Attr("content")
		if content == "" {
			return
		}

		if property, ok := s.Attr("property"); ok {
			switch property {
			case "og:type":
				inArticleBlock = content == "article"
			case "og:title":
				if inArticleBlock || result.Title == "" {
					result.Title = content
				}
			case "og:description":
				if inArticleBlock || result.Description == "" {
					result.Description = content
				}
			case "og:image":
				if inArticleBlock || result.ImageURL == "" {
					result.ImageURL = content
				}
			}
		}

		if name, ok := s.Attr("name"); ok {
			switch name {
			case "twitter:title":
				if result.Title == "" {
					result.Title = content
				}
			case "twitter:description":
				if result.Description == "" {
					result.Description = content
				}
			case "twitter:image":
				if result.ImageURL == "" {
					result.ImageURL = content
				}
			case "description":
				if result.Description == "" {
					result.Description = content
				}
			}
		}
	})

	if result.Title == "" {
		result.Title = strings.TrimSpace(doc.Find("title").Text())
	}

	// Fallback description: first few sentences from article paragraphs
	if result.Description == "" {
		var paras []string
		doc.Find("article p").Each(func(_ int, s *goquery.Selection) {
			if t := strings.TrimSpace(s.Text()); t != "" {
				paras = append(paras, t)
			}
		})
		if len(paras) > 0 {
			result.Description = strings.Join(paras[:min(3, len(paras))], " ")
		}
	}

	// JSON-LD fallback: many publishers embed full articleBody in ld+json.
	if result.Description == "" {
		doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
			var data struct {
				ArticleBody string `json:"articleBody"`
			}
			if err := json.Unmarshal([]byte(s.Text()), &data); err == nil && data.ArticleBody != "" {
				result.Description = data.ArticleBody
				return false
			}
			return true
		})
	}
}

// extractContent parses a fresh copy of the HTML body and runs the full
// defuddle pipeline: remove clutter → score blocks → find main content → extract text.
func extractContent(body *bytes.Reader, removePartial bool) string {
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return ""
	}

	root := doc.Selection

	// 1. Remove exact-selector clutter
	removeExactSelectors(root)

	// 2. Remove elements matching partial-selector patterns on class/id/data attrs
	if removePartial {
		removePartialSelectors(root)
	}

	// 3. Remove short paragraphs whose text matches navigation/CTA patterns
	removePatternParagraphs(root)

	// 4. Score and remove remaining non-content blocks
	scoreAndRemove(root)

	// 5. Find the best content element
	main := findMainContent(root)
	if main == nil || main.Length() == 0 {
		main = doc.Find("body")
	}
	if main.Length() == 0 {
		main = root
	}

	// 6. Extract readable text
	return extractText(main)
}

// removeExactSelectors removes elements matching the exact CSS selector list.
// Individual selectors that cascadia cannot parse are silently skipped.
func removeExactSelectors(root *goquery.Selection) {
	for _, sel := range exactSelectors {
		func() {
			defer func() { recover() }()
			root.Find(sel).Remove()
		}()
	}
}

var partialPattern *regexp.Regexp

func init() {
	// Escape any regex metacharacters in the partial selector strings, then join
	escaped := make([]string, len(partialSelectors))
	for i, p := range partialSelectors {
		escaped[i] = regexp.QuoteMeta(p)
	}
	partialPattern = regexp.MustCompile(`(?i)(` + strings.Join(escaped, "|") + `)`)
}

// removePartialSelectors removes elements whose class/id/data attributes match
// any pattern in partialSelectors.
func removePartialSelectors(root *goquery.Selection) {
	attrSel := strings.Join(testAttributeSelectors, ",")

	var toRemove []*goquery.Selection
	root.Find(attrSel).Each(func(_ int, s *goquery.Selection) {
		for _, attr := range testAttributes {
			val, ok := s.Attr(attr)
			if !ok || val == "" {
				continue
			}
			if partialPattern.MatchString(val) {
				toRemove = append(toRemove, s)
				return
			}
		}
	})

	for _, s := range toRemove {
		s.Remove()
	}
}

// removePatternParagraphs removes short <p> elements whose text matches
// navigation/CTA patterns (newsletter CTAs, social prompts, boilerplate).
// Long paragraphs are left alone to avoid false positives in real prose.
func removePatternParagraphs(root *goquery.Selection) {
	var toRemove []*goquery.Selection
	root.Find("p").Each(func(_ int, s *goquery.Selection) {
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		if len(strings.Fields(text)) > 20 {
			return
		}
		for _, indicator := range navigationIndicators {
			if strings.Contains(text, indicator) {
				toRemove = append(toRemove, s)
				return
			}
		}
	})
	for _, s := range toRemove {
		s.Remove()
	}
}

// extractText pulls readable text from the content element. Prefers collecting
// individual paragraphs; falls back to all text content if no paragraphs found.
func extractText(s *goquery.Selection) string {
	var parts []string

	s.Find("p, pre").Each(func(_ int, p *goquery.Selection) {
		if t := strings.TrimSpace(p.Text()); t != "" {
			parts = append(parts, t)
		}
	})

	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}

	// Fallback: headings + list items + blockquotes
	s.Find("h1, h2, h3, h4, h5, h6, li, blockquote").Each(func(_ int, el *goquery.Selection) {
		if t := strings.TrimSpace(el.Text()); t != "" {
			parts = append(parts, t)
		}
	})

	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}

	return strings.TrimSpace(s.Text())
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}

// blockedPrefixes covers all IANA special-use ranges that must not be
// reachable via user-initiated outbound fetches (loopback, private,
// link-local, CGNAT, cloud metadata, documentation, benchmarking, multicast).
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),          // "this" network
	netip.MustParsePrefix("10.0.0.0/8"),         // private
	netip.MustParsePrefix("100.64.0.0/10"),      // CGNAT shared address space
	netip.MustParsePrefix("127.0.0.0/8"),        // loopback
	netip.MustParsePrefix("169.254.0.0/16"),     // link-local + cloud metadata
	netip.MustParsePrefix("172.16.0.0/12"),      // private
	netip.MustParsePrefix("192.0.0.0/24"),       // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),       // documentation TEST-NET-1
	netip.MustParsePrefix("192.168.0.0/16"),     // private
	netip.MustParsePrefix("198.18.0.0/15"),      // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"),    // documentation TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),     // documentation TEST-NET-3
	netip.MustParsePrefix("224.0.0.0/4"),        // multicast
	netip.MustParsePrefix("240.0.0.0/4"),        // reserved
	netip.MustParsePrefix("255.255.255.255/32"), // broadcast
	netip.MustParsePrefix("::/128"),             // unspecified
	netip.MustParsePrefix("::1/128"),            // loopback
	netip.MustParsePrefix("::ffff:0:0/96"),      // IPv4-mapped
	netip.MustParsePrefix("64:ff9b::/96"),       // IPv4/IPv6 translation
	netip.MustParsePrefix("100::/64"),           // discard
	netip.MustParsePrefix("2001:db8::/32"),      // documentation
	netip.MustParsePrefix("fc00::/7"),           // unique local
	netip.MustParsePrefix("fe80::/10"),          // link-local
	netip.MustParsePrefix("ff00::/8"),           // multicast
}

// IsBlockedAddr reports whether ip is a reserved address that must not be
// reached by outbound fetches. Exported so callers can run the same check
// on DNS-resolved addresses before handing a URL to Fetch.
func IsBlockedAddr(ip netip.Addr) bool {
	ip = ip.Unmap() // normalize IPv4-in-IPv6 to plain IPv4
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

var baseDialer = &net.Dialer{}

// safeDialContext is http.Transport.DialContext for every cloned collector.
// Go passes addr as "host:port" where host may be a hostname or an IP.
// We resolve it ourselves, validate every returned IP, then dial the first
// one directly so there is no second DNS lookup (no rebinding window).
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("bad address: %w", err)
	}

	// Fast path: addr already contains a literal IP.
	if ip, err := netip.ParseAddr(host); err == nil {
		if IsBlockedAddr(ip) {
			return nil, fmt.Errorf("connection to reserved address blocked: %s", host)
		}
		return baseDialer.DialContext(ctx, network, addr)
	}

	// Slow path: resolve the hostname, validate all results, dial the first.
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("could not resolve %s: %w", host, err)
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	for _, ipAddr := range resolved {
		ip, ok := netip.AddrFromSlice(ipAddr.IP)
		if !ok {
			continue
		}
		if IsBlockedAddr(ip) {
			return nil, fmt.Errorf("connection to reserved address blocked: %s", ipAddr.IP)
		}
	}
	return baseDialer.DialContext(ctx, network, net.JoinHostPort(resolved[0].IP.String(), port))
}
