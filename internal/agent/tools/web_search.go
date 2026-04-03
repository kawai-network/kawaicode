package tools

import (
	"context"
	_ "embed"
	"log/slog"
	"net/http"
	"time"

	"github.com/getkawai/unillm"
)

//go:embed web_search.md
var webSearchToolDescription []byte

// NewWebSearchTool creates a web search tool for sub-agents (no permissions needed).
func NewWebSearchTool(client *http.Client) unillm.AgentTool {
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
		WebSearchToolName,
		string(webSearchToolDescription),
		func(ctx context.Context, params WebSearchParams, call unillm.ToolCall) (unillm.ToolResponse, error) {
			if params.Query == "" {
				return unillm.NewTextErrorResponse("query is required"), nil
			}

			maxResults := params.MaxResults
			if maxResults <= 0 {
				maxResults = 10
			}
			if maxResults > 20 {
				maxResults = 20
			}

			maybeDelaySearch()
			results, err := searchDuckDuckGo(ctx, client, params.Query, maxResults)
			slog.Debug("Web search completed", "query", params.Query, "results", len(results), "err", err)
			if err != nil {
				return unillm.NewTextErrorResponse("Failed to search: " + err.Error()), nil
			}

			return unillm.NewTextResponse(formatSearchResults(results)), nil
		})
}
