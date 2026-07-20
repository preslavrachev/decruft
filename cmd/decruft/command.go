package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/preslavrachev/decruft"
	"github.com/urfave/cli/v3"
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
						output = renderMarkdown(result.Title, body)
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

func renderMarkdown(title, content string) string {
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)

	switch {
	case title == "":
		return content
	case content == "":
		return "# " + title
	default:
		return fmt.Sprintf("# %s\n\n%s", title, content)
	}
}
