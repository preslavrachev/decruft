package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/preslavrachev/decruft"
)

func TestParseDefaultsToCleanedHTML(t *testing.T) {
	fetcher := &fakeFetcher{result: &decruft.Result{
		Content:   `<article><h1>Example</h1><p>Readable content.</p></article>`,
		WordCount: 3,
	}}
	var stdout bytes.Buffer

	err := newCommand(fetcher, &stdout, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse", "https://example.com/article"},
	)
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if stdout.String() != "<article><h1>Example</h1><p>Readable content.</p></article>\n" {
		t.Fatalf("output: got %q", stdout.String())
	}
}

func TestParseMarkdownPreservesStructureWithoutDuplicateH1(t *testing.T) {
	fetcher := &fakeFetcher{result: &decruft.Result{
		Title:   "Example article",
		Content: `<article><h1>Example article</h1><h2>Details</h2><p>A <strong>bold</strong> <a href="https://example.com">link</a>.</p></article>`,
	}}
	var stdout bytes.Buffer

	err := newCommand(fetcher, &stdout, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse", "https://example.com/article", "--markdown"},
	)
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	output := stdout.String()
	if strings.Count(output, "# Example article") != 1 {
		t.Fatalf("title heading was duplicated:\n%s", output)
	}
	for _, want := range []string{"## Details", "**bold**", "[link](https://example.com)"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestParseMarkdownEscapesDescriptionFallback(t *testing.T) {
	fetcher := &fakeFetcher{result: &decruft.Result{
		Description: `<script>alert("unsafe")</script> *literal* [text](javascript:alert(1))`,
	}}
	var stdout bytes.Buffer

	err := newCommand(fetcher, &stdout, io.Discard).Run(
		context.Background(),
		[]string{"decruft", "parse", "https://example.com/article", "--md"},
	)
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	output := stdout.String()
	if strings.Contains(output, "<script>") || strings.Contains(output, " *literal* ") {
		t.Fatalf("description was interpreted as markup:\n%s", output)
	}
	for _, want := range []string{"&lt;script&gt;", `\*literal*`, `\[text](javascript:alert(1))`} {
		if !strings.Contains(output, want) {
			t.Errorf("escaped fallback missing %q:\n%s", want, output)
		}
	}
}
