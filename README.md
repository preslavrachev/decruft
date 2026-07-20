# decruft

Decruft extracts readable plain-text content and metadata from cluttered web
pages.

It fetches a URL, removes navigation, advertising, forms, sidebars, sharing
widgets, and other page chrome, then scores the remaining DOM to find the main
content.

Decruft is inspired by
[Defuddle](https://github.com/kepano/defuddle), Readability, and similar
content-extraction projects. It is an independent Go implementation and is not
affiliated with the original Defuddle project.

## Installation

Install the CLI:

```bash
go install github.com/preslavrachev/decruft/cmd/decruft@latest
```

Or add the library to a Go module:

```bash
go get github.com/preslavrachev/decruft
```

## CLI

The initial CLI intentionally mirrors the small part of Defuddle's command
surface used for local URL inspection:

```bash
decruft parse https://example.com/article --markdown
```

`--md` is available as a shorter alias:

```bash
decruft parse https://example.com/article --md
```

Markdown output includes the extracted title as a heading followed by the
readable content. Without `--markdown`, the command prints plain text.

The CLI currently accepts one URL at a time. It does not yet parse local files,
read HTML from standard input, emit JSON, or execute page JavaScript.

## Library usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/preslavrachev/decruft"
)

func main() {
	fetcher := decruft.New()

	result, err := fetcher.Fetch(
		context.Background(),
		"https://example.com/article",
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Title)
	fmt.Println(result.Description)
	fmt.Println(result.ImageURL)
	fmt.Println(result.Content)
}
```

`Result` contains:

```go
type Result struct {
	URL         string
	Title       string
	Description string
	ImageURL    string
	Domain      string
	Content     string
}
```

Use `BestContent` when short or empty extractions should fall back to a
substantial description:

```go
content := result.BestContent()
if content == "" {
	// Fall back to application-specific metadata, such as an RSS description.
}
```

## How it works

The extraction pipeline:

1. Reads Open Graph, Twitter Card, standard metadata, and JSON-LD.
2. Removes known clutter and hidden elements.
3. Removes short navigation and call-to-action paragraphs.
4. Scores the remaining DOM blocks.
5. Selects the strongest main-content candidate.
6. Extracts paragraphs and code blocks as readable plain text.
7. Retries with less aggressive removal when the first result is too thin.

## Network safety

The default fetcher is intended for use with untrusted URLs. Its HTTP transport
blocks loopback, private, link-local, cloud metadata, documentation, multicast,
and other reserved IP ranges. DNS results are validated before dialing to
reduce SSRF and DNS-rebinding risk.

`IsBlockedAddr` is exported for applications that need to apply the same
address policy before calling `Fetch`.

## Current scope

- The library output is plain text. The CLI can wrap it as basic Markdown.
- Pages are parsed from their HTTP response and JavaScript is not executed.
- Extraction is heuristic and may need site-specific regression fixes.
- The default fetcher has a 10-second request timeout.

## Development

```bash
gofmt -w .
go test ./...
go vet ./...
```

New extraction behavior should include a focused test using inline HTML or a
local fixture. Tests should not depend on live websites.
