package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/preslavrachev/decruft"
)

func TestParseMarkdownEscapesSyntheticTitleBeforeConversion(t *testing.T) {
	fetcher := &fakeFetcher{result: &decruft.Result{
		Title:   "Unsafe [link](javascript:alert(1)) <img src=x>\n## Injected",
		Content: "<article><p>Readable body.</p></article>",
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
	for _, want := range []string{
		`\[link](javascript:alert(1))`,
		"&lt;img src=x&gt;",
		"## Injected",
		"Readable body.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing escaped title value %q:\n%s", want, output)
		}
	}
	for _, unsafe := range []string{
		"<img src=x>",
		"\n## Injected",
	} {
		if strings.Contains(output, unsafe) {
			t.Errorf("output contains active title markup %q:\n%s", unsafe, output)
		}
	}
}
