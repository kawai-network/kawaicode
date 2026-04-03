package tools

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/getkawai/unillm"
)

//go:embed web_fetch.md
var webFetchToolDescription []byte

// NewWebFetchTool creates a simple web fetch tool for sub-agents (no permissions needed).
func NewWebFetchTool(workingDir string, client *http.Client) unillm.AgentTool {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 10
		transport.IdleConnTimeout = 90 * time.Second

		client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		}
	}

	return unillm.NewParallelAgentTool(
		WebFetchToolName,
		string(webFetchToolDescription),
		func(ctx context.Context, params WebFetchParams, call unillm.ToolCall) (unillm.ToolResponse, error) {
			if params.URL == "" {
				return unillm.NewTextErrorResponse("url is required"), nil
			}

			content, err := FetchURLAndConvert(ctx, client, params.URL)
			if err != nil {
				return unillm.NewTextErrorResponse(fmt.Sprintf("Failed to fetch URL: %s", err)), nil
			}

			hasLargeContent := len(content) > LargeContentThreshold
			var result strings.Builder

			if hasLargeContent {
				tempFile, err := os.CreateTemp(workingDir, "page-*.md")
				if err != nil {
					return unillm.NewTextErrorResponse(fmt.Sprintf("Failed to create temporary file: %s", err)), nil
				}
				tempFilePath := tempFile.Name()

				if _, err := tempFile.WriteString(content); err != nil {
					_ = tempFile.Close() // Best effort close
					return unillm.NewTextErrorResponse(fmt.Sprintf("Failed to write content to file: %s", err)), nil
				}
				if err := tempFile.Close(); err != nil {
					return unillm.NewTextErrorResponse(fmt.Sprintf("Failed to close temporary file: %s", err)), nil
				}

				fmt.Fprintf(&result, "Fetched content from %s (large page)\n\n", params.URL)
				fmt.Fprintf(&result, "Content saved to: %s\n\n", tempFilePath)
				result.WriteString("Use the view and grep tools to analyze this file.")
			} else {
				fmt.Fprintf(&result, "Fetched content from %s:\n\n", params.URL)
				result.WriteString(content)
			}

			return unillm.NewTextResponse(result.String()), nil
		})
}
