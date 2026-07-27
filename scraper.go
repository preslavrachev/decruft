package decruft

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
)

const (
	minContentWords    = 50
	defaultMaxBodySize = 10 * 1024 * 1024
	defaultTimeout     = 10 * time.Second
	defaultRetryDelay  = 100 * time.Millisecond
	maxRetryDelay      = time.Second
	maxResponseDrain   = 4 * 1024
)

var (
	// ErrHTTPStatus reports a final non-2xx HTTP response.
	ErrHTTPStatus = errors.New("unexpected HTTP status")
	// ErrResponseTooLarge reports a response that exceeds the configured body limit.
	ErrResponseTooLarge = errors.New("response body too large")
	// ErrUnsupportedMediaType reports a response known not to contain readable text.
	ErrUnsupportedMediaType = errors.New("unsupported media type")
)

type Result struct {
	// URL is the originally requested URL.
	URL string
	// Title is the extracted document title.
	Title string
	// Description is extracted metadata and may be plain text.
	Description string
	// ImageURL is the absolute URL of the extracted representative image.
	ImageURL string
	// Domain is the hostname of the originally requested URL.
	Domain string
	// Content is cleaned HTML for the selected main-content subtree. It is not
	// sanitized or trusted HTML and must be sanitized before browser insertion.
	Content string
	// WordCount is the number of whitespace-delimited words in Content's
	// visible DOM text.
	WordCount int
}

// Markdown converts the cleaned HTML in Content to Markdown. It preserves
// supported document structure, including GFM tables.
func (r *Result) Markdown() (string, error) {
	if strings.TrimSpace(r.Content) == "" {
		return "", nil
	}

	markdownConverter := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
		),
	)
	markdown, err := markdownConverter.ConvertString(r.Content)
	if err != nil {
		return "", fmt.Errorf("converting cleaned HTML: %v", err)
	}
	return markdown, nil
}

// BestContent returns cleaned HTML when Content has at least 50 visible words,
// Description as a plain-text fallback when it is substantial, or "" when both
// are thin.
//
// Deprecated: use BestHTML, which always returns cleaned HTML and therefore
// has a uniform output contract.
func (r *Result) BestContent() string {
	if r.contentWordCount() >= minContentWords {
		return r.Content
	}
	if len(strings.Fields(r.Description)) >= minContentWords {
		return r.Description
	}
	return ""
}

// BestHTML returns cleaned HTML when Content has at least 50 visible words. If
// Content is thin but Description is substantial, it returns the description
// HTML-escaped inside a paragraph. It returns "" when both are thin.
//
// The returned HTML is not sanitized or trusted and must be sanitized before
// browser insertion.
func (r *Result) BestHTML() string {
	if r.contentWordCount() >= minContentWords {
		return r.Content
	}
	if len(strings.Fields(r.Description)) >= minContentWords {
		return "<p>" + html.EscapeString(r.Description) + "</p>"
	}
	return ""
}

func (r *Result) contentWordCount() int {
	if r.WordCount != 0 || strings.TrimSpace(r.Content) == "" {
		return r.WordCount
	}
	return visibleWordCount(r.Content)
}

type Fetcher interface {
	Fetch(ctx context.Context, url string) (*Result, error)
}

// Option configures the default network fetcher.
type Option func(*fetchConfig)

// WithLanguage sends language as the preferred BCP 47 language in HTTP
// requests. An empty value leaves the Accept-Language header unset.
func WithLanguage(language string) Option {
	return func(config *fetchConfig) {
		config.language = strings.TrimSpace(language)
	}
}

type fetchConfig struct {
	language      string
	maxBodySize   int
	maxRetries    int
	parallelism   int
	requestDelay  time.Duration
	randomDelay   time.Duration
	retryDelay    time.Duration
	maxRetryDelay time.Duration
	timeout       time.Duration
	now           func() time.Time
	wait          func(context.Context, time.Duration) error
}

type httpFetcher struct {
	config    fetchConfig
	transport http.RoundTripper
	slots     chan struct{}
}

func New() *httpFetcher {
	return NewWithOptions()
}

// NewWithOptions creates the default guarded network fetcher with options.
func NewWithOptions(options ...Option) *httpFetcher {
	config := defaultFetchConfig()
	for _, option := range options {
		option(&config)
	}
	return newHTTPFetcher(config, &http.Transport{DialContext: safeDialContext})
}

func defaultFetchConfig() fetchConfig {
	return fetchConfig{
		maxBodySize:   defaultMaxBodySize,
		maxRetries:    1,
		parallelism:   3,
		requestDelay:  500 * time.Millisecond,
		randomDelay:   250 * time.Millisecond,
		retryDelay:    defaultRetryDelay,
		maxRetryDelay: maxRetryDelay,
		timeout:       defaultTimeout,
		now:           time.Now,
		wait:          waitForContext,
	}
}

func newHTTPFetcher(config fetchConfig, transport http.RoundTripper) *httpFetcher {
	return &httpFetcher{
		config:    config,
		transport: transport,
		slots:     make(chan struct{}, config.parallelism),
	}
}

func (f *httpFetcher) Fetch(ctx context.Context, rawURL string) (*Result, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid URL: expected an HTTP or HTTPS URL")
	}

	select {
	case f.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer f.releaseSlot(ctx)

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %v", err)
	}

	for attemptNumber := 0; ; attemptNumber++ {
		result := &Result{URL: rawURL, Domain: parsed.Hostname()}
		attempt := f.fetchOnce(ctx, rawURL, result, jar)
		if attempt.err == nil {
			return finalizeResult(result, parsed, attempt.baseURL)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !attempt.retryable || attemptNumber >= f.config.maxRetries {
			return nil, attempt.err
		}

		delay := attempt.retryAfter
		if delay == 0 {
			delay = f.config.retryDelay
		}
		if err := f.config.wait(ctx, delay); err != nil {
			return nil, err
		}
	}
}

type fetchAttempt struct {
	baseURL    *url.URL
	err        error
	retryAfter time.Duration
	retryable  bool
}

func (f *httpFetcher) fetchOnce(ctx context.Context, rawURL string, result *Result, jar http.CookieJar) fetchAttempt {
	attempt := fetchAttempt{}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		attempt.err = fmt.Errorf("creating request: %v", err)
		return attempt
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Decruft/1.0)")
	request.Header.Set("Accept", "text/html, application/xhtml+xml;q=0.9, */*;q=0.1")
	request.Header.Set("Accept-Encoding", "gzip")
	if f.config.language != "" {
		request.Header.Set("Accept-Language", f.config.language)
	}

	client := &http.Client{
		Transport: &bodyLimitTransport{
			transport: f.transport,
			limit:     int64(f.config.maxBodySize),
		},
		Jar:     jar,
		Timeout: f.config.timeout,
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			attempt.err = ctx.Err()
			return attempt
		}
		attempt.err = fmt.Errorf("fetching URL: %v", err)
		attempt.retryable = true
		return attempt
	}
	defer response.Body.Close()
	attempt.baseURL = response.Request.URL

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		drainResponseBody(response.Body)
		attempt.err = fmt.Errorf("%w: HTTP %d %s", ErrHTTPStatus, response.StatusCode, http.StatusText(response.StatusCode))
		attempt.retryable = shouldRetryStatus(response.StatusCode)
		attempt.retryAfter = parseRetryAfter(response.Header.Get("Retry-After"), f.config.now(), f.config.maxRetryDelay)
		return attempt
	}
	if mediaType := unsupportedMediaType(response.Header.Get("Content-Type")); mediaType != "" {
		attempt.err = fmt.Errorf("%w: %s", ErrUnsupportedMediaType, mediaType)
		return attempt
	}
	if responseContentLength(response) > int64(f.config.maxBodySize) {
		attempt.err = fmt.Errorf("%w: limit is %d bytes", ErrResponseTooLarge, f.config.maxBodySize)
		return attempt
	}

	body, err := readResponseBody(response, f.config.maxBodySize)
	if errors.Is(err, ErrResponseTooLarge) {
		attempt.err = fmt.Errorf("%w: limit is %d bytes", ErrResponseTooLarge, f.config.maxBodySize)
		return attempt
	}
	if err != nil {
		attempt.err = fmt.Errorf("reading response: %v", err)
		attempt.retryable = isRetryableBodyReadError(err)
		return attempt
	}
	if err := extractResponse(body, result, response.Request.URL); err != nil {
		attempt.err = err
	}
	return attempt
}

func responseContentLength(response *http.Response) int64 {
	if response.ContentLength > 0 {
		return response.ContentLength
	}
	contentLength, _ := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64)
	return contentLength
}

func readResponseBody(response *http.Response, limit int) ([]byte, error) {
	var reader io.Reader = response.Body
	if !response.Uncompressed && strings.Contains(strings.ToLower(response.Header.Get("Content-Encoding")), "gzip") {
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		defer compressed.Close()
		reader = compressed
	}

	encoded, err := readBoundedBody(reader, limit)
	if err != nil {
		return nil, err
	}
	contentType := response.Header.Get("Content-Type")
	if !hasDeclaredCharset(contentType) && utf8.Valid(encoded) {
		return bytes.TrimPrefix(encoded, []byte("\xef\xbb\xbf")), nil
	}

	decoded, err := charset.NewReader(bytes.NewReader(encoded), contentType)
	if err != nil {
		return nil, err
	}
	return readBoundedBody(decoded, limit)
}

func readBoundedBody(reader io.Reader, limit int) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, ErrResponseTooLarge
	}
	return body, nil
}

func hasDeclaredCharset(contentType string) bool {
	_, parameters, err := mime.ParseMediaType(contentType)
	return err == nil && strings.TrimSpace(parameters["charset"]) != ""
}

type bodyLimitTransport struct {
	transport http.RoundTripper
	limit     int64
}

func (t *bodyLimitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.transport.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &bodyLimitReadCloser{
		ReadCloser: response.Body,
		remaining:  t.limit,
	}
	return response, nil
}

type bodyLimitReadCloser struct {
	io.ReadCloser
	remaining int64
}

func (r *bodyLimitReadCloser) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		var probe [1]byte
		read, err := r.ReadCloser.Read(probe[:])
		if read > 0 {
			return 0, ErrResponseTooLarge
		}
		return 0, classifyBodyReadError(err)
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}

	read, err := r.ReadCloser.Read(buffer)
	r.remaining -= int64(read)
	return read, classifyBodyReadError(err)
}

type transportBodyReadError struct {
	err error
}

func (e *transportBodyReadError) Error() string {
	return e.err.Error()
}

func (e *transportBodyReadError) Unwrap() error {
	return e.err
}

func classifyBodyReadError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, ErrResponseTooLarge) {
		return err
	}
	return &transportBodyReadError{err: err}
}

func isRetryableBodyReadError(err error) bool {
	var readErr *transportBodyReadError
	if errors.As(err, &readErr) {
		return true
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}

func drainResponseBody(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxResponseDrain))
}

func extractResponse(body []byte, result *Result, responseURL *url.URL) error {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	extractMeta(doc, result)
	baseURL := documentBaseURL(doc, responseURL)
	result.ImageURL, _ = normalizeURL(result.ImageURL, baseURL, imageURLSchemes)

	content, err := extractContentCandidate(bytes.NewReader(body), true)
	if err != nil {
		return err
	}
	if content.wordCount < 200 {
		retry, err := extractContentCandidate(bytes.NewReader(body), false)
		if err != nil {
			return err
		}
		if retry.wordCount > content.wordCount {
			content = retry
		}
	}

	normalizeContentURLs(content.selection, baseURL)
	contentHTML, err := goquery.OuterHtml(content.selection)
	if err != nil {
		return err
	}
	result.Content = strings.TrimSpace(contentHTML)
	result.WordCount = content.wordCount
	return nil
}

func finalizeResult(result *Result, originalURL, responseURL *url.URL) (*Result, error) {
	if result.Title == "" && result.WordCount == 0 {
		return nil, fmt.Errorf("no usable content found at URL")
	}

	baseURL := originalURL
	if responseURL != nil {
		baseURL = responseURL
	}
	result.ImageURL, _ = normalizeURL(result.ImageURL, baseURL, imageURLSchemes)

	return result, nil
}

func (f *httpFetcher) releaseSlot(ctx context.Context) {
	delay := f.config.requestDelay
	if f.config.randomDelay > 0 {
		delay += time.Duration(f.config.now().UnixNano() % int64(f.config.randomDelay))
	}
	if delay > 0 {
		_ = f.config.wait(ctx, delay)
	}
	<-f.slots
}

func shouldRetryStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func parseRetryAfter(value string, now time.Time, maximum time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" || maximum <= 0 {
		return 0
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > int64(maximum/time.Second) {
			return maximum
		}
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		return min(max(retryAt.Sub(now), 0), maximum)
	}
	return 0
}

func unsupportedMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "image/") ||
		strings.HasPrefix(mediaType, "audio/") ||
		strings.HasPrefix(mediaType, "video/") ||
		strings.HasPrefix(mediaType, "font/") {
		return mediaType
	}
	switch mediaType {
	case "application/octet-stream", "application/pdf", "application/zip":
		return mediaType
	default:
		return ""
	}
}

func waitForContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

type contentCandidate struct {
	selection *goquery.Selection
	wordCount int
}

// extractContent is retained as an internal convenience for focused extraction
// tests. Production extraction uses extractContentCandidate so parse and
// serialization errors can be returned to the caller.
func extractContent(body *bytes.Reader, removePartial bool) string {
	candidate, err := extractContentCandidate(body, removePartial)
	if err != nil {
		return ""
	}
	contentHTML, err := goquery.OuterHtml(candidate.selection)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(contentHTML)
}

// extractContentCandidate parses a fresh copy of the HTML body and runs the
// full extraction pipeline while retaining the selected cleaned DOM subtree.
func extractContentCandidate(body *bytes.Reader, removePartial bool) (*contentCandidate, error) {
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil, err
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

	return &contentCandidate{
		selection: main,
		wordCount: len(strings.Fields(strings.TrimSpace(main.Text()))),
	}, nil
}

func documentBaseURL(doc *goquery.Document, responseURL *url.URL) *url.URL {
	if responseURL == nil {
		return nil
	}

	if value, ok := doc.Find("base[href]").First().Attr("href"); ok {
		if normalized, valid := normalizeURL(value, responseURL, imageURLSchemes); valid {
			if base, err := url.Parse(normalized); err == nil {
				return base
			}
		}
	}
	return responseURL
}

var (
	linkURLSchemes  = map[string]bool{"http": true, "https": true, "mailto": true, "tel": true}
	imageURLSchemes = map[string]bool{"http": true, "https": true}
)

func normalizeContentURLs(content *goquery.Selection, baseURL *url.URL) {
	normalizeURLAttribute(content, "a[href]", "href", baseURL, linkURLSchemes)
	normalizeURLAttribute(content, "img[src]", "src", baseURL, imageURLSchemes)
}

func normalizeURLAttribute(content *goquery.Selection, selector, attribute string, baseURL *url.URL, allowedSchemes map[string]bool) {
	elements := content.Find(selector).AddSelection(content.Filter(selector))
	elements.Each(func(_ int, element *goquery.Selection) {
		value, _ := element.Attr(attribute)
		normalized, valid := normalizeURL(value, baseURL, allowedSchemes)
		if !valid {
			element.RemoveAttr(attribute)
			return
		}
		element.SetAttr(attribute, normalized)
	})
}

func normalizeURL(value string, baseURL *url.URL, allowedSchemes map[string]bool) (string, bool) {
	ref, err := url.Parse(strings.TrimSpace(value))
	if err != nil || ref.String() == "" {
		return "", false
	}
	if ref.Scheme == "" {
		if baseURL == nil {
			return "", false
		}
		ref = baseURL.ResolveReference(ref)
	}

	scheme := strings.ToLower(ref.Scheme)
	if !allowedSchemes[scheme] {
		return "", false
	}
	ref.Scheme = scheme
	if (scheme == "http" || scheme == "https") && ref.Hostname() == "" {
		return "", false
	}
	return ref.String(), true
}

func visibleWordCount(content string) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return 0
	}
	return len(strings.Fields(strings.TrimSpace(doc.Text())))
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

// safeDialContext is the guarded http.Transport.DialContext used by the fetcher.
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
