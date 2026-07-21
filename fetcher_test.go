package decruft

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewConfiguresDefaults(t *testing.T) {
	fetcher := New()

	if fetcher.config.maxBodySize != defaultMaxBodySize {
		t.Fatalf("body limit: got %d, want %d", fetcher.config.maxBodySize, defaultMaxBodySize)
	}
	if fetcher.config.timeout != defaultTimeout {
		t.Fatalf("timeout: got %s, want %s", fetcher.config.timeout, defaultTimeout)
	}
	if fetcher.config.maxRetries != 1 {
		t.Fatalf("retries: got %d, want 1", fetcher.config.maxRetries)
	}
	if cap(fetcher.slots) != 3 {
		t.Fatalf("parallelism: got %d, want 3", cap(fetcher.slots))
	}
	localized := NewWithOptions(WithLanguage("bg"))
	if localized.config.language != "bg" {
		t.Fatalf("language: got %q, want %q", localized.config.language, "bg")
	}
}

func TestFetchSuccess(t *testing.T) {
	var requestMethod string
	var userAgent string
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestMethod = req.Method
		userAgent = req.Header.Get("User-Agent")
		return htmlResponse(req, http.StatusOK, `<html><head>
			<title>Fallback title</title>
			<meta property="og:title" content="Characterized article">
			<meta property="og:description" content="A useful description.">
			<meta property="og:image" content="/images/cover.jpg">
		</head><body><article>
			<p>The first readable paragraph contains the article content.</p>
			<p>The second readable paragraph adds useful detail.</p>
		</article></body></html>`), nil
	}))

	result, err := fetcher.Fetch(context.Background(), "https://example.com/articles/post")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if requestMethod != http.MethodGet {
		t.Fatalf("method: got %q, want GET", requestMethod)
	}
	if userAgent != "Mozilla/5.0 (compatible; Decruft/1.0)" {
		t.Fatalf("user agent: got %q", userAgent)
	}
	if result.URL != "https://example.com/articles/post" {
		t.Fatalf("URL: got %q", result.URL)
	}
	if result.Domain != "example.com" {
		t.Fatalf("domain: got %q", result.Domain)
	}
	if result.Title != "Characterized article" {
		t.Fatalf("title: got %q", result.Title)
	}
	if result.Description != "A useful description." {
		t.Fatalf("description: got %q", result.Description)
	}
	if result.ImageURL != "https://example.com/images/cover.jpg" {
		t.Fatalf("image URL: got %q", result.ImageURL)
	}
	if !strings.Contains(result.Content, "first readable paragraph") {
		t.Fatalf("content: got %q", result.Content)
	}
}

func TestFetchReturnsHTTPStatusErrors(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return htmlResponse(req, status, `<html><head><title>Status response</title></head></html>`), nil
			}))

			_, err := fetcher.Fetch(context.Background(), "https://example.com/status")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
				t.Fatalf("error: got %v, want HTTP %d", err, status)
			}
		})
	}
}

func TestFetchAcceptsSuccessfulHTTPStatus(t *testing.T) {
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return htmlResponse(req, http.StatusNonAuthoritativeInfo, `<html><head><title>Successful article</title></head></html>`), nil
	}))

	result, err := fetcher.Fetch(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.Title != "Successful article" {
		t.Fatalf("title: got %q", result.Title)
	}
}

func TestFetchReportsTransportError(t *testing.T) {
	fetcher := newTestFetcher(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	}))

	_, err := fetcher.Fetch(context.Background(), "https://example.com/article")
	if err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("error: got %v", err)
	}
}

func TestFetchReturnsContextCancellation(t *testing.T) {
	started := make(chan struct{})
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := fetcher.Fetch(ctx, "https://example.com/slow")
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error: got %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Fetch did not return after cancellation")
	}
}

func TestFetchFollowsRedirect(t *testing.T) {
	var requests atomic.Int32
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		if req.URL.Path == "/start" {
			return redirectResponse(req, "/article"), nil
		}
		return htmlResponse(req, http.StatusOK, `<html><head><title>Redirected article</title></head>
			<body><article><p>Readable redirected content.</p></article></body></html>`), nil
	}))

	result, err := fetcher.Fetch(context.Background(), "https://example.com/start")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests: got %d, want 2", requests.Load())
	}
	if result.Title != "Redirected article" {
		t.Fatalf("title: got %q", result.Title)
	}
}

func TestFetchStopsRedirectLoop(t *testing.T) {
	var requests atomic.Int32
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := requests.Add(1)
		return redirectResponse(req, fmt.Sprintf("/redirect/%d", n)), nil
	}))

	_, err := fetcher.Fetch(context.Background(), "https://example.com/redirect/0")
	if err == nil {
		t.Fatal("expected redirect loop error")
	}
	if requests.Load() != 10 {
		t.Fatalf("requests: got %d, want 10", requests.Load())
	}
}

func TestFetchCharacterizesBodyLimit(t *testing.T) {
	const limit = 256

	t.Run("below limit", func(t *testing.T) {
		body := sizedHTML(t, limit-1, "Below limit")
		fetcher := newTestFetcherWithBodyLimit(staticHTMLTransport(body), limit)

		result, err := fetcher.Fetch(context.Background(), "https://example.com/below")
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if result.Title != "Below limit" {
			t.Fatalf("title: got %q", result.Title)
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		body := sizedHTML(t, limit, "Exact limit")
		fetcher := newTestFetcherWithBodyLimit(staticHTMLTransport(body), limit)

		result, err := fetcher.Fetch(context.Background(), "https://example.com/exact")
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if result.Title != "Exact limit" {
			t.Fatalf("title: got %q", result.Title)
		}
	})

	t.Run("above limit returns an error", func(t *testing.T) {
		body := `<html><head><title>Truncated article</title></head><body><article><p>` +
			strings.Repeat("readable word ", 40) + `TAIL_MARKER</p></article></body></html>`
		fetcher := newTestFetcherWithBodyLimit(staticHTMLTransport(body), limit)

		_, err := fetcher.Fetch(context.Background(), "https://example.com/large")
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("error: got %v, want ErrResponseTooLarge", err)
		}
	})
}

func TestFetchDecodesDeclaredCharset(t *testing.T) {
	body := []byte("<html><head><title>Caf\xe9</title></head><body><article><p>Caf\xe9 article.</p></article></body></html>")
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := htmlResponseBytes(req, http.StatusOK, body)
		resp.Header.Set("Content-Type", "text/html; charset=iso-8859-1")
		return resp, nil
	}))

	result, err := fetcher.Fetch(context.Background(), "https://example.com/charset")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.Title != "Caf\u00e9" {
		t.Fatalf("title: got %q", result.Title)
	}
	if !strings.Contains(result.Content, "Caf\u00e9 article") {
		t.Fatalf("content: got %q", result.Content)
	}
}

func TestFetchDecompressesGzip(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := io.WriteString(zw, `<html><head><title>Compressed article</title></head>
		<body><article><p>Readable compressed content.</p></article></body></html>`); err != nil {
		t.Fatalf("compress body: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}

	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := htmlResponseBytes(req, http.StatusOK, compressed.Bytes())
		resp.Header.Set("Content-Encoding", "gzip")
		return resp, nil
	}))

	result, err := fetcher.Fetch(context.Background(), "https://example.com/compressed")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.Title != "Compressed article" {
		t.Fatalf("title: got %q", result.Title)
	}
}

func TestFetchRejectsMalformedAndUnsupportedURLs(t *testing.T) {
	t.Run("malformed escaping", func(t *testing.T) {
		fetcher := newTestFetcher(staticHTMLTransport(`<html><head><title>Unused</title></head></html>`))

		_, err := fetcher.Fetch(context.Background(), "https://example.com/%zz")
		if err == nil || !strings.Contains(err.Error(), "invalid URL") {
			t.Fatalf("error: got %v", err)
		}
	})

	t.Run("unsupported scheme", func(t *testing.T) {
		fetcher := newTestFetcher(http.DefaultTransport)

		_, err := fetcher.Fetch(context.Background(), "file:///tmp/article.html")
		if err == nil {
			t.Fatal("expected unsupported scheme error")
		}
	})
}

func TestFetchReportsNoUsableContent(t *testing.T) {
	fetcher := newTestFetcher(staticHTMLTransport(`<html><head></head><body></body></html>`))

	_, err := fetcher.Fetch(context.Background(), "https://example.com/empty")
	if err == nil || !strings.Contains(err.Error(), "no usable content") {
		t.Fatalf("error: got %v", err)
	}
}

func TestFetchLimitsConcurrentRequests(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		current := active.Add(1)
		recordMaximum(&maximum, current)
		started <- struct{}{}
		<-release
		active.Add(-1)
		return htmlResponse(req, http.StatusOK, `<html><head><title>Concurrent article</title></head></html>`), nil
	}), func(config *fetchConfig) {
		config.parallelism = 3
	})

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := fetcher.Fetch(context.Background(), fmt.Sprintf("https://example.com/article/%d", i))
			errs <- err
		}(i)
	}

	for range 3 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("three requests did not start concurrently")
		}
	}
	select {
	case <-started:
		close(release)
		t.Fatal("fourth request started before a concurrency slot was released")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent fetches did not finish")
	}
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("fetch: %v", err)
		}
	}
	if maximum.Load() != 3 {
		t.Fatalf("maximum concurrency: got %d, want 3", maximum.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func recordMaximum(maximum *atomic.Int32, current int32) {
	for {
		previous := maximum.Load()
		if current <= previous {
			return
		}
		if maximum.CompareAndSwap(previous, current) {
			return
		}
	}
}

func newTestFetcher(transport http.RoundTripper) Fetcher {
	return newTestFetcherWithBodyLimit(transport, 10*1024*1024)
}

func newTestFetcherWithBodyLimit(transport http.RoundTripper, bodyLimit int) Fetcher {
	return newConfiguredTestFetcher(transport, func(config *fetchConfig) {
		config.maxBodySize = bodyLimit
	})
}

func newConfiguredTestFetcher(transport http.RoundTripper, configure func(*fetchConfig)) Fetcher {
	config := testFetchConfig()
	if configure != nil {
		configure(&config)
	}
	return newHTTPFetcher(config, transport)
}

func testFetchConfig() fetchConfig {
	config := defaultFetchConfig()
	config.maxRetries = 0
	config.requestDelay = 0
	config.randomDelay = 0
	config.retryDelay = 0
	return config
}

func staticHTMLTransport(body string) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return htmlResponse(req, http.StatusOK, body), nil
	})
}

func htmlResponse(req *http.Request, status int, body string) *http.Response {
	return htmlResponseBytes(req, status, []byte(body))
}

func htmlResponseBytes(req *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func redirectResponse(req *http.Request, location string) *http.Response {
	resp := htmlResponse(req, http.StatusFound, "")
	resp.Header.Set("Location", location)
	return resp
}

func sizedHTML(t *testing.T, size int, title string) string {
	t.Helper()
	prefix := fmt.Sprintf("<html><head><title>%s</title></head><body><article><p>Readable content.", title)
	suffix := "</p></article></body></html>"
	padding := size - len(prefix) - len(suffix)
	if padding < 0 {
		t.Fatalf("requested body size %d is too small", size)
	}
	return prefix + strings.Repeat(" ", padding) + suffix
}
