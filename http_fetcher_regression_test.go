package decruft

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchRetriesResponseBodyReadFailures(t *testing.T) {
	tests := []struct {
		name    string
		readErr error
	}{
		{name: "connection reset", readErr: errors.New("connection reset by peer")},
		{name: "body deadline", readErr: context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if attempts.Add(1) == 1 {
					resp := htmlResponse(req, http.StatusOK, "")
					resp.Body = &failingResponseBody{
						Reader: bytes.NewBufferString("<html><head><title>Partial"),
						err:    tt.readErr,
					}
					return resp, nil
				}
				return htmlResponse(req, http.StatusOK, `<html><head><title>Recovered article</title></head></html>`), nil
			}), configureRetryTest)

			result, err := fetcher.Fetch(context.Background(), "https://example.com/article")
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if attempts.Load() != 2 {
				t.Fatalf("attempts: got %d, want 2", attempts.Load())
			}
			if result.Title != "Recovered article" {
				t.Fatalf("title: got %q", result.Title)
			}
		})
	}
}

func TestFetchRetriesClientTimeoutWhileReadingBody(t *testing.T) {
	var attempts atomic.Int32
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			resp := htmlResponse(req, http.StatusOK, "")
			resp.Body = &contextResponseBody{ctx: req.Context()}
			return resp, nil
		}
		return htmlResponse(req, http.StatusOK, `<html><head><title>Recovered article</title></head></html>`), nil
	}), func(config *fetchConfig) {
		configureRetryTest(config)
		config.timeout = 20 * time.Millisecond
	})

	result, err := fetcher.Fetch(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts: got %d, want 2", attempts.Load())
	}
	if result.Title != "Recovered article" {
		t.Fatalf("title: got %q", result.Title)
	}
}

func TestFetchDoesNotRetryGzipDecodeFailure(t *testing.T) {
	var attempts atomic.Int32
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)
		resp := htmlResponse(req, http.StatusOK, "not gzip data")
		resp.Header.Set("Content-Encoding", "gzip")
		return resp, nil
	}), configureRetryTest)

	_, err := fetcher.Fetch(context.Background(), "https://example.com/article")
	if err == nil {
		t.Fatal("expected gzip decode error")
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts: got %d, want 1", attempts.Load())
	}
}

func TestFetchDrainsRetryableResponseBeforeRetry(t *testing.T) {
	firstBody := &trackingResponseBody{Reader: bytes.NewBufferString("temporarily unavailable")}
	var attempts atomic.Int32
	fetcher := newConfiguredTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			resp := htmlResponse(req, http.StatusServiceUnavailable, "")
			resp.Body = firstBody
			return resp, nil
		}
		return htmlResponse(req, http.StatusOK, `<html><head><title>Recovered article</title></head></html>`), nil
	}), configureRetryTest)

	if _, err := fetcher.Fetch(context.Background(), "https://example.com/article"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !firstBody.reachedEOF {
		t.Fatal("retryable response body was not drained")
	}
	if !firstBody.closed {
		t.Fatal("retryable response body was not closed")
	}
}

func TestFetchDetectsUndeclaredUTF8BeyondCharsetSniffWindow(t *testing.T) {
	title := strings.Repeat("a", 1100) + " Caf\u00e9"
	body := `<html><head><title>` + title + `</title></head></html>`
	fetcher := newTestFetcher(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return htmlResponse(req, http.StatusOK, body), nil
	}))

	result, err := fetcher.Fetch(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.Title != title {
		t.Fatalf("title: got %q, want %q", result.Title, title)
	}
}

type failingResponseBody struct {
	io.Reader
	err error
}

func (b *failingResponseBody) Read(buffer []byte) (int, error) {
	read, err := b.Reader.Read(buffer)
	if err == io.EOF {
		return 0, b.err
	}
	return read, err
}

func (b *failingResponseBody) Close() error {
	return nil
}

type trackingResponseBody struct {
	io.Reader
	reachedEOF bool
	closed     bool
}

type contextResponseBody struct {
	ctx context.Context
}

func (b *contextResponseBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *contextResponseBody) Close() error {
	return nil
}

func (b *trackingResponseBody) Read(buffer []byte) (int, error) {
	read, err := b.Reader.Read(buffer)
	if err == io.EOF {
		b.reachedEOF = true
	}
	return read, err
}

func (b *trackingResponseBody) Close() error {
	b.closed = true
	return nil
}
