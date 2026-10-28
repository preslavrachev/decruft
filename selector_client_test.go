package decruft

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const selectorPage = `<html><head><title>Prices</title></head><body>
<nav><a href="/home">Home</a></nav>
<table class="prices"><tr><td><a href="/item/1">One</a></td> <td>10</td> </td></tr></table>
<div class="row">first</div><p>between</p><div class="row"><img src="/a.png">second</div>
</body></html>`

// TestWithSelectorSingleMatch checks that only the requested element is
// returned, with relative links made absolute and no word-count floor.
func TestWithSelectorSingleMatch(t *testing.T) {
	//GIVEN: a page with a short price table and surrounding navigation.
	base, _ := url.Parse("https://example.com/shop/")
	result := &Result{}

	//WHEN: extracting with the table selector.
	err := extractDocument([]byte(selectorPage), result, base, "table.prices")

	//THEN: the table is kept with an absolute link, and the navigation is not.
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(result.Content, `href="https://example.com/item/1"`) {
		t.Fatalf("content: %q", result.Content)
	}
	if strings.Contains(result.Content, "Home") {
		t.Fatalf("navigation leaked: %q", result.Content)
	}
	if result.WordCount != 2 {
		t.Fatalf("word count: got %d, want 2", result.WordCount)
	}
}

// TestWithSelectorMultipleMatchesKeepOrder checks that several matches are
// concatenated in document order.
func TestWithSelectorMultipleMatchesKeepOrder(t *testing.T) {
	//GIVEN: a page with two separate "row" elements.
	base, _ := url.Parse("https://example.com/")
	result := &Result{}

	//WHEN: extracting with the row selector.
	err := extractDocument([]byte(selectorPage), result, base, ".row")

	//THEN: both rows appear in order and the text between them is excluded.
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	first, second := strings.Index(result.Content, "first"), strings.Index(result.Content, "second")
	if first < 0 || second < first || strings.Contains(result.Content, "between") {
		t.Fatalf("content: %q", result.Content)
	}
	if !strings.Contains(result.Content, `src="https://example.com/a.png"`) {
		t.Fatalf("image not absolutized: %q", result.Content)
	}
}

// TestWithSelectorNoMatch checks that a selector matching nothing is a
// distinguishable error rather than empty content.
func TestWithSelectorNoMatch(t *testing.T) {
	//GIVEN: a page without the requested element.
	//WHEN: extracting with a selector that matches nothing.
	err := extractDocument([]byte(selectorPage), &Result{}, nil, ".missing")

	//THEN: the error is ErrSelectorNoMatch and names the selector.
	if !errors.Is(err, ErrSelectorNoMatch) || !strings.Contains(err.Error(), ".missing") {
		t.Fatalf("error: got %v", err)
	}
}

// TestWithSelectorInvalidSelector checks that a malformed selector returns an
// error instead of panicking.
func TestWithSelectorInvalidSelector(t *testing.T) {
	//GIVEN: a malformed CSS selector.
	//WHEN: extracting with it.
	err := extractDocument([]byte(selectorPage), &Result{}, nil, "div[")

	//THEN: an error that is not ErrSelectorNoMatch is returned.
	if err == nil || errors.Is(err, ErrSelectorNoMatch) {
		t.Fatalf("error: got %v", err)
	}
}

// TestWithSelectorFetchIsNotRetried checks that a missing selector fails the
// fetch once instead of being retried.
func TestWithSelectorFetchIsNotRetried(t *testing.T) {
	//GIVEN: a server serving a page without the selected element.
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, selectorPage)
	}))
	defer server.Close()
	fetcher := NewWithOptions(WithHTTPClient(server.Client()), WithSelector(".missing"))
	fetcher.config.requestDelay, fetcher.config.randomDelay = 0, 0

	//WHEN: fetching.
	_, err := fetcher.Fetch(context.Background(), server.URL)

	//THEN: ErrSelectorNoMatch is returned after a single request.
	if !errors.Is(err, ErrSelectorNoMatch) || requests != 1 {
		t.Fatalf("error: %v, requests: %d", err, requests)
	}
}

// TestWithHTTPClientBypassesDefaultGuard checks that loopback servers are
// blocked by default but reachable through a caller-supplied client.
func TestWithHTTPClientBypassesDefaultGuard(t *testing.T) {
	//GIVEN: a local test server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, selectorPage)
	}))
	defer server.Close()
	guarded := New()
	guarded.config.requestDelay, guarded.config.randomDelay, guarded.config.maxRetries = 0, 0, 0
	custom := NewWithOptions(WithHTTPClient(server.Client()), WithSelector("table.prices"))
	custom.config.requestDelay, custom.config.randomDelay = 0, 0

	//WHEN: fetching it with the default and with the custom fetcher.
	_, guardedErr := guarded.Fetch(context.Background(), server.URL)
	result, customErr := custom.Fetch(context.Background(), server.URL)

	//THEN: the default is blocked and the custom one returns the table.
	if guardedErr == nil {
		t.Fatal("default fetcher reached loopback")
	}
	if customErr != nil || !strings.Contains(result.Content, "One") {
		t.Fatalf("custom fetch: %v, %v", result, customErr)
	}
}

// TestWithHTTPClientKeepsBodyLimitAndTimeout checks that the size limit still
// applies and the client's timeout is adopted.
func TestWithHTTPClientKeepsBodyLimitAndTimeout(t *testing.T) {
	//GIVEN: a server with a body larger than a small limit.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>"+strings.Repeat("x ", 5000)+"</body></html>")
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	fetcher := NewWithOptions(WithHTTPClient(client))
	fetcher.config.requestDelay, fetcher.config.randomDelay = 0, 0
	fetcher.config.maxBodySize = 1024

	//WHEN: fetching.
	_, err := fetcher.Fetch(context.Background(), server.URL)

	//THEN: the body limit error is returned and the timeout is the client's.
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error: got %v", err)
	}
	if fetcher.config.timeout != 3*time.Second {
		t.Fatalf("timeout: got %s", fetcher.config.timeout)
	}
}

// TestWithSelectorNestedMatchesAreNotDuplicated checks that an element nested
// inside another match is rendered once, as part of its ancestor.
func TestWithSelectorNestedMatchesAreNotDuplicated(t *testing.T) {
	//GIVEN: a page where one matching div contains another, plus a sibling div.
	page := `<html><body><div id="outer"><div id="inner">deep</div></div><div id="next">after</div></body></html>`
	result := &Result{}

	//WHEN: extracting with the "div" selector.
	err := extractDocument([]byte(page), result, nil, "div")

	//THEN: each block appears exactly once, in document order.
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if n := strings.Count(result.Content, "deep"); n != 1 {
		t.Fatalf("inner content appears %d times: %q", n, result.Content)
	}
	if strings.Index(result.Content, "deep") > strings.Index(result.Content, "after") {
		t.Fatalf("order: %q", result.Content)
	}
}
