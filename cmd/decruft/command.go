package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/preslavrachev/decruft"
	"github.com/urfave/cli/v3"
	xhtml "golang.org/x/net/html"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return newCommand(decruft.New(), stdout, stderr).Run(ctx, args)
}

func newCommand(fetcher decruft.Fetcher, stdout, stderr io.Writer) *cli.Command {
	cmd := &cli.Command{
		Name:      "decruft",
		Usage:     "extract readable content from web pages",
		Writer:    stdout,
		ErrWriter: stderr,
		Commands: []*cli.Command{
			{
				Name:      "parse",
				Usage:     "fetch and extract a URL",
				ArgsUsage: "URL",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "markdown",
						Aliases: []string{"md"},
						Usage:   "format the result as Markdown",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return fmt.Errorf("parse requires exactly one URL")
					}

					rawURL := cmd.Args().First()
					result, err := fetcher.Fetch(ctx, rawURL)
					if err != nil {
						return fmt.Errorf("extracting %s: %w", rawURL, err)
					}

					body := result.Content
					if strings.TrimSpace(body) == "" {
						body = result.Description
					}
					if strings.TrimSpace(body) == "" && strings.TrimSpace(result.Title) == "" {
						return fmt.Errorf("extracting %s: no readable content found", rawURL)
					}

					output := body
					if cmd.Bool("markdown") {
						output, err = renderMarkdown(result)
						if err != nil {
							return fmt.Errorf("converting output to Markdown: %w", err)
						}
					} else if strings.TrimSpace(output) == "" {
						output = result.Title
					}

					if _, err := fmt.Fprintln(cmd.Root().Writer, strings.TrimSpace(output)); err != nil {
						return fmt.Errorf("writing output: %w", err)
					}
					return nil
				},
			},
		},
	}
	return cmd
}

func renderMarkdown(result *decruft.Result) (string, error) {
	contentHTML := strings.TrimSpace(result.Content)
	if contentHTML == "" {
		contentHTML = plainTextHTML(result.Description)
	} else if !strings.Contains(contentHTML, "<") {
		contentHTML = plainTextHTML(contentHTML)
	}

	title := strings.TrimSpace(result.Title)
	if title != "" && !hasEquivalentH1(contentHTML, title) {
		contentHTML = "<h1>" + html.EscapeString(title) + "</h1>" + contentHTML
	}

	projection := &decruft.Result{Content: contentHTML}
	return projection.Markdown()
}

func hasEquivalentH1(contentHTML, title string) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(contentHTML))
	if err != nil {
		return false
	}
	leading := leadingContentNode(doc.Selection.Get(0))
	if leading == nil || leading.Type != xhtml.ElementNode || leading.Data != "h1" {
		return false
	}
	heading := goquery.NewDocumentFromNode(leading).Text()
	return strings.EqualFold(strings.TrimSpace(heading), strings.TrimSpace(title))
}

func leadingContentNode(node *xhtml.Node) *xhtml.Node {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case xhtml.TextNode:
			if strings.TrimSpace(child.Data) != "" {
				return child
			}
		case xhtml.ElementNode:
			switch child.Data {
			case "html", "head", "body", "article", "main", "div", "section":
				if leading := leadingContentNode(child); leading != nil {
					return leading
				}
			default:
				return child
			}
		}
	}
	return nil
}

func plainTextHTML(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var content strings.Builder
	for paragraph := range strings.SplitSeq(text, "\n\n") {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		escaped := html.EscapeString(paragraph)
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
		content.WriteString("<p>")
		content.WriteString(escaped)
		content.WriteString("</p>")
	}
	return content.String()
}
