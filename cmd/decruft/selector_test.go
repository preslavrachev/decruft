package main

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/preslavrachev/decruft"
)

// TestParseSelectorPassesOptionAndPrintsContent checks that --selector builds a
// fetcher with the option and prints the content as HTML or Markdown.
func TestParseSelectorPassesOptionAndPrintsContent(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"html", []string{"decruft", "parse", "https://example.com", "--selector", "table"}, "<b>Price</b> 10\n"},
		{"markdown", []string{"decruft", "parse", "https://example.com", "--selector", "table", "--markdown"}, "**Price** 10\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			//GIVEN: a fetcher factory that records how many options it received.
			fetcher := &fakeFetcher{result: &decruft.Result{Content: "<b>Price</b> 10"}}
			var optionCount int
			factory := func(options ...decruft.Option) decruft.Fetcher {
				optionCount = len(options)
				return fetcher
			}
			var stdout bytes.Buffer

			//WHEN: running parse with --selector.
			err := newCommandWithFactory(factory, &stdout, io.Discard).Run(context.Background(), tc.args)

			//THEN: the selector option is passed and the content is printed.
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if optionCount != 1 {
				t.Fatalf("options: got %d, want 1", optionCount)
			}
			if stdout.String() != tc.want {
				t.Fatalf("output: want %q, got %q", tc.want, stdout.String())
			}
		})
	}
}

// TestParseSelectorMarkdownOmitsTitle checks that Markdown output of a
// selected element does not gain the page title as a heading.
func TestParseSelectorMarkdownOmitsTitle(t *testing.T) {
	//GIVEN: a fetcher returning a titled page whose selected content is a paragraph.
	fetcher := &fakeFetcher{result: &decruft.Result{Title: "Example Domain", Content: "<p>Hello</p>"}}
	var stdout bytes.Buffer

	//WHEN: running parse with --selector and --markdown.
	err := newCommand(fetcher, &stdout, io.Discard).Run(context.Background(),
		[]string{"decruft", "parse", "https://example.com", "--selector", "p", "--markdown"})

	//THEN: only the paragraph is printed.
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.String() != "Hello\n" {
		t.Fatalf("output: got %q", stdout.String())
	}
}
