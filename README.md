# decruft

Decruft extracts cleaned HTML and metadata from cluttered web pages. It can
project the cleaned HTML to structured Markdown on demand.

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

`--markdown` produces real Markdown that preserves supported headings, links,
images, emphasis, lists, blockquotes, code blocks, and GFM tables. The command
adds the extracted title only when the converted content does not already begin
with the same H1. Without `--markdown`, the command prints cleaned HTML.

`--selector 'table.prices'` skips main-content scoring and outputs only the
elements matching the CSS selector (HTML, or Markdown with `--markdown`). It
fails when nothing matches.

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

	markdown, err := result.Markdown()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(markdown)
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
	Content     string // Cleaned HTML.
	WordCount   int    // Visible words in Content.
}
```

`Result.Content` is the cleaned selected DOM subtree and is the canonical
content representation. `Result.Markdown()` converts that HTML only when
requested. `WordCount` counts whitespace-delimited words in the visible DOM
text, not HTML tokens.

The cleaned HTML is not sanitized or trusted HTML. Sanitize it before inserting
it into a browser document.

Use `BestHTML` when short or empty extractions should fall back to a substantial
description. It always returns HTML: either substantial cleaned `Content`, or
an HTML-escaped `Description` wrapped in a paragraph:

```go
content := result.BestHTML()
if content == "" {
	// Fall back to application-specific metadata, such as an RSS description.
}
```

`BestContent` is deprecated because its legacy fallback can return plain text
while its primary branch returns HTML.

Use `NewWithOptions` to request a preferred document language:

```go
fetcher := decruft.NewWithOptions(decruft.WithLanguage("en"))
```

`WithSelector(css)` returns exactly the matching elements in `Result.Content`
instead of the scored main content. No match returns `ErrSelectorNoMatch`;
an invalid selector returns an error. Read `Result.Content` directly, since
`BestHTML` and `BestContent` keep their 50-word floor.

`WithHTTPClient(client)` sends requests through the client's transport and
timeout. The body limit, cookie jar, and retries still apply, but the SSRF guard
does not: the caller owns dial safety.

## How it works

The extraction pipeline:

1. Reads Open Graph, Twitter Card, standard metadata, and JSON-LD.
2. Removes known clutter and hidden elements.
3. Removes short navigation and call-to-action paragraphs.
4. Scores the remaining DOM blocks.
5. Selects the strongest main-content candidate.
6. Retries with less aggressive removal when the first candidate is too thin.
7. Resolves content links and images against the document base or final
   response URL.
8. Serializes the winning cleaned DOM subtree as HTML.
9. Converts that HTML to Markdown only when requested.

## Network safety

The default fetcher is intended for use with untrusted URLs. Its HTTP transport
blocks loopback, private, link-local, cloud metadata, documentation, multicast,
and other reserved IP ranges. DNS results are validated before dialing to
reduce SSRF and DNS-rebinding risk.

Responses are limited to 10 MB. Known binary media types and oversized
responses are rejected before their bodies are read when response headers make
that possible. The fetcher retries one transient transport failure or HTTP 429,
502, 503, or 504 response, with `Retry-After` capped at one second. Cookies are
kept within one fetch and its redirects, but are not shared between calls.

Callers can identify policy failures with `errors.Is` and
`ErrHTTPStatus`, `ErrResponseTooLarge`, or `ErrUnsupportedMediaType`.

`IsBlockedAddr` is exported for applications that need to apply the same
address policy before calling `Fetch`.

## Current scope

- The library and default CLI output is cleaned, unsanitized HTML.
- `Result.Markdown()`, `--markdown`, and `--md` produce structured Markdown.
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
