package decruft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchPropagatesContextCancellationToRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-req.Context().Done()
		close(requestCanceled)
		return nil, req.Context().Err()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := fetcher.Fetch(ctx, "https://example.com/article")
		result <- err
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()

	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("request transport did not observe cancellation")
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("error: got %v, want context canceled", err)
	}
}

func TestFetchDetectsUndeclaredLegacyCharset(t *testing.T) {
	body := []byte(`<html><head><title>Caf` + "\xe9" + ` Society</title></head><body><article><p>` +
		strings.Repeat("Le caf\xe9 est d\xe9licieux et tr\xe8s appr\xe9ci\xe9. ", 12) +
		`</p></article></body></html>`)
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return htmlResponseBytes(req, http.StatusOK, body), nil
	}))

	result, err := fetcher.Fetch(context.Background(), "https://example.com/legacy-charset")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.Title != "Caf\u00e9 Society" {
		t.Fatalf("title: got %q", result.Title)
	}
	if !strings.Contains(result.Content, "d\u00e9licieux") {
		t.Fatalf("content: got %q", result.Content)
	}
}

func TestFetchRejectsKnownBinaryWithoutReadingBody(t *testing.T) {
	for _, contentType := range []string{
		"image/jpeg",
		"application/pdf",
		"audio/mpeg",
		"video/mp4",
		"font/woff2",
	} {
		t.Run(contentType, func(t *testing.T) {
			body := &trackingReadCloser{reader: strings.NewReader(strings.Repeat("binary", 100))}
			fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				resp := htmlResponse(req, http.StatusOK, "")
				resp.Header.Set("Content-Type", contentType)
				resp.Body = body
				return resp, nil
			}))

			_, err := fetcher.Fetch(context.Background(), "https://example.com/file")
			if !errors.Is(err, ErrUnsupportedMediaType) {
				t.Fatalf("error: got %v, want ErrUnsupportedMediaType", err)
			}
			if body.bytesRead.Load() != 0 {
				t.Fatalf("body bytes read: got %d, want 0", body.bytesRead.Load())
			}
		})
	}
}

func TestFetchRejectsOversizedContentLengthWithoutReadingBody(t *testing.T) {
	const limit = 128
	body := &trackingReadCloser{reader: strings.NewReader(strings.Repeat("x", limit+1))}
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := htmlResponse(req, http.StatusOK, "")
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", limit+1))
		resp.Body = body
		return resp, nil
	}), func(config *fetchConfig) {
		config.maxBodySize = limit
	})

	_, err := fetcher.Fetch(context.Background(), "https://example.com/large")
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error: got %v, want ErrResponseTooLarge", err)
	}
	if body.bytesRead.Load() != 0 {
		t.Fatalf("body bytes read: got %d, want 0", body.bytesRead.Load())
	}
}

func TestFetchChecksRawBodySizeBeforeCharsetConversion(t *testing.T) {
	const limit = 128
	html := `<html><head><title>Wide article</title></head><body><article><p>` +
		strings.Repeat("readable content ", 20) + `</p></article></body></html>`
	body := make([]byte, len(html)*2)
	for i := range len(html) {
		body[i*2] = html[i]
	}

	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := htmlResponseBytes(req, http.StatusOK, body)
		resp.Header.Set("Content-Type", "text/html; charset=utf-16le")
		return resp, nil
	}), func(config *fetchConfig) {
		config.maxBodySize = limit
	})

	_, err := fetcher.Fetch(context.Background(), "https://example.com/wide")
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error: got %v, want ErrResponseTooLarge", err)
	}
}

func TestFetchAcceptsMissingOrGenericContentType(t *testing.T) {
	for _, contentType := range []string{"", "application/octet-stream-ish"} {
		t.Run(contentType, func(t *testing.T) {
			fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				resp := htmlResponse(req, http.StatusOK, `<html><head><title>Forgiving article</title></head></html>`)
				resp.Header.Set("Content-Type", contentType)
				return resp, nil
			}))

			result, err := fetcher.Fetch(context.Background(), "https://example.com/article")
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if result.Title != "Forgiving article" {
				t.Fatalf("title: got %q", result.Title)
			}
		})
	}
}

func TestFetchSendsDocumentRequestHeaders(t *testing.T) {
	var accept string
	var language string
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		accept = req.Header.Get("Accept")
		language = req.Header.Get("Accept-Language")
		return htmlResponse(req, http.StatusOK, `<html><head><title>Localized article</title></head></html>`), nil
	}), WithLanguage("bg"))

	if _, err := fetcher.Fetch(context.Background(), "https://example.com/article"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(accept, "text/html") || !strings.Contains(accept, "application/xhtml+xml") {
		t.Fatalf("Accept: got %q", accept)
	}
	if language != "bg" {
		t.Fatalf("Accept-Language: got %q, want %q", language, "bg")
	}
}

func TestFetchCarriesRedirectCookieWithoutCrossFetchLeak(t *testing.T) {
	var cookieLeaked atomic.Bool
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/start":
			resp := redirectResponse(req, "/article")
			resp.Header.Add("Set-Cookie", "session=redirect; Path=/")
			return resp, nil
		case "/article":
			if cookie, err := req.Cookie("session"); err != nil || cookie.Value != "redirect" {
				return nil, fmt.Errorf("redirect cookie missing")
			}
			return htmlResponse(req, http.StatusOK, `<html><head><title>Cookie article</title></head></html>`), nil
		case "/independent":
			if _, err := req.Cookie("session"); err == nil {
				cookieLeaked.Store(true)
			}
			return htmlResponse(req, http.StatusOK, `<html><head><title>Independent article</title></head></html>`), nil
		default:
			return nil, fmt.Errorf("unexpected path %s", req.URL.Path)
		}
	})
	fetcher := newTestFetcher(transport)

	if _, err := fetcher.Fetch(context.Background(), "https://example.com/start"); err != nil {
		t.Fatalf("redirect fetch: %v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), "https://example.com/independent"); err != nil {
		t.Fatalf("independent fetch: %v", err)
	}
	if cookieLeaked.Load() {
		t.Fatal("cookie from previous Fetch leaked into an independent call")
	}
}

func TestFetchRetriesTransientResponseOnce(t *testing.T) {
	for _, status := range []int{
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			var requests atomic.Int32
			fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if requests.Add(1) == 1 {
					return htmlResponse(req, status, ""), nil
				}
				return htmlResponse(req, http.StatusOK, `<html><head><title>Recovered article</title></head></html>`), nil
			}), configureRetryTest)

			result, err := fetcher.Fetch(context.Background(), "https://example.com/article")
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if requests.Load() != 2 {
				t.Fatalf("requests: got %d, want 2", requests.Load())
			}
			if result.Title != "Recovered article" {
				t.Fatalf("title: got %q", result.Title)
			}
		})
	}
}

func TestFetchRetriesTransientTransportErrorOnce(t *testing.T) {
	var requests atomic.Int32
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return nil, errors.New("temporary connection failure")
		}
		return htmlResponse(req, http.StatusOK, `<html><head><title>Recovered transport</title></head></html>`), nil
	}), configureRetryTest)

	result, err := fetcher.Fetch(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests: got %d, want 2", requests.Load())
	}
	if result.Title != "Recovered transport" {
		t.Fatalf("title: got %q", result.Title)
	}
}

func TestFetchDoesNotRetryPermanentResponse(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
	} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			var requests atomic.Int32
			fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests.Add(1)
				return htmlResponse(req, status, ""), nil
			}), configureRetryTest)

			_, err := fetcher.Fetch(context.Background(), "https://example.com/article")
			if !errors.Is(err, ErrHTTPStatus) {
				t.Fatalf("error: got %v, want ErrHTTPStatus", err)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests: got %d, want 1", requests.Load())
			}
		})
	}
}

func TestFetchDoesNotRetryCanceledRequest(t *testing.T) {
	var requests atomic.Int32
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}), configureRetryTest)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fetcher.Fetch(ctx, "https://example.com/article")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: got %v, want context canceled", err)
	}
	if requests.Load() > 1 {
		t.Fatalf("requests: got %d, want at most 1", requests.Load())
	}
}

func TestFetchBoundsRetryAfter(t *testing.T) {
	var requests atomic.Int32
	var waited time.Duration
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			resp := htmlResponse(req, http.StatusServiceUnavailable, "")
			resp.Header.Set("Retry-After", "30")
			return resp, nil
		}
		return htmlResponse(req, http.StatusOK, `<html><head><title>Retried article</title></head></html>`), nil
	}), func(config *fetchConfig) {
		configureRetryTest(config)
		config.maxRetryDelay = 2 * time.Second
		config.wait = func(_ context.Context, delay time.Duration) error {
			waited = delay
			return nil
		}
	})

	if _, err := fetcher.Fetch(context.Background(), "https://example.com/article"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if waited != 2*time.Second {
		t.Fatalf("retry wait: got %s, want 2s", waited)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "seconds", value: "3", want: 3 * time.Second},
		{name: "HTTP date", value: now.Add(4 * time.Second).Format(http.TimeFormat), want: 4 * time.Second},
		{name: "bounded", value: "30", want: 5 * time.Second},
		{name: "integer overflow", value: "9223372036854775807", want: 5 * time.Second},
		{name: "past date", value: now.Add(-time.Second).Format(http.TimeFormat), want: 0},
		{name: "invalid", value: "later", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.value, now, 5*time.Second); got != tt.want {
				t.Fatalf("parseRetryAfter: got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestWaitForContext(t *testing.T) {
	if err := waitForContext(context.Background(), 0); err != nil {
		t.Fatalf("zero delay: %v", err)
	}
	if err := waitForContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("completed delay: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delay: got %v, want context canceled", err)
	}
}

type trackingReadCloser struct {
	reader    io.Reader
	bytesRead atomic.Int64
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead.Add(int64(n))
	return n, err
}

func (*trackingReadCloser) Close() error {
	return nil
}

func configureRetryTest(config *fetchConfig) {
	config.maxRetries = 1
	config.wait = func(context.Context, time.Duration) error { return nil }
}
