package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/preslavrachev/decruft"
)

func TestParseMarkdown(t *testing.T) {
	fetcher := &fakeFetcher{
		result: &decruft.Result{
			Title:   "Example article",
			Content: "First paragraph.\n\nSecond paragraph.",
		},
	}
	var stdout bytes.Buffer

	err := newCommand(fetcher, &stdout, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse", "https://example.com/article", "--markdown"},
	)
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	want := "# Example article\n\nFirst paragraph.\n\nSecond paragraph.\n"
	if stdout.String() != want {
		t.Fatalf("output:\nwant %q\ngot  %q", want, stdout.String())
	}
	if fetcher.rawURL != "https://example.com/article" {
		t.Fatalf("URL: got %q", fetcher.rawURL)
	}
}

func TestParseMarkdownAlias(t *testing.T) {
	fetcher := &fakeFetcher{
		result: &decruft.Result{Content: "Article content."},
	}
	var stdout bytes.Buffer

	err := newCommand(fetcher, &stdout, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse", "https://example.com/article", "--md"},
	)
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	if stdout.String() != "Article content.\n" {
		t.Fatalf("output: got %q", stdout.String())
	}
}

func TestParsePlainTitleOnly(t *testing.T) {
	fetcher := &fakeFetcher{
		result: &decruft.Result{Title: "Example article"},
	}
	var stdout bytes.Buffer

	err := newCommand(fetcher, &stdout, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse", "https://example.com/article"},
	)
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	if stdout.String() != "Example article\n" {
		t.Fatalf("output: got %q", stdout.String())
	}
}

func TestParseReportsNoContent(t *testing.T) {
	fetcher := &fakeFetcher{result: &decruft.Result{}}

	err := newCommand(fetcher, io.Discard, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse", "https://example.com/article"},
	)
	if err == nil || !strings.Contains(err.Error(), "no readable content found") {
		t.Fatalf("error: got %v", err)
	}
}

func TestParseRequiresOneURL(t *testing.T) {
	err := newCommand(&fakeFetcher{}, io.Discard, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse"},
	)
	if err == nil || !strings.Contains(err.Error(), "exactly one URL") {
		t.Fatalf("error: got %v", err)
	}
}

func TestParseReportsFetchError(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.New("connection refused")}

	err := newCommand(fetcher, io.Discard, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse", "https://example.com/article"},
	)
	if err == nil || !strings.Contains(err.Error(), "extracting https://example.com/article") {
		t.Fatalf("error: got %v", err)
	}
}

type fakeFetcher struct {
	result *decruft.Result
	err    error
	rawURL string
}

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string) (*decruft.Result, error) {
	f.rawURL = rawURL
	return f.result, f.err
}
