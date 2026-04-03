package tools

import (
	"context"
	"fmt"
	"slices"

	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/getkawai/unillm"
)

// whitelistDockerTools contains Docker MCP tools that don't require permission.
var whitelistDockerTools = []string{
	"mcp_docker_mcp-find",
	"mcp_docker_mcp-add",
	"mcp_docker_mcp-remove",
	"mcp_docker_mcp-config-set",
	"mcp_docker_code-mode",
}

// GetMCPTools gets all the currently available MCP tools.
func GetMCPTools(permissions permission.Service, cfg *config.ConfigStore, wd string) []*Tool {
	var result []*Tool
	for mcpName, tools := range mcp.Tools() {
		for _, tool := range tools {
			result = append(result, &Tool{
				mcpName:     mcpName,
				tool:        tool,
				permissions: permissions,
				workingDir:  wd,
				cfg:         cfg,
			})
		}
	}
	return result
}

// Tool is a tool from a MCP.
type Tool struct {
	mcpName         string
	tool            *mcp.Tool
	cfg             *config.ConfigStore
	permissions     permission.Service
	workingDir      string
	providerOptions unillm.ProviderOptions
}

func (m *Tool) SetProviderOptions(opts unillm.ProviderOptions) {
	m.providerOptions = opts
}

func (m *Tool) ProviderOptions() unillm.ProviderOptions {
	return m.providerOptions
}

func (m *Tool) Name() string {
	return fmt.Sprintf("mcp_%s_%s", m.mcpName, m.tool.Name)
}

func (m *Tool) MCP() string {
	return m.mcpName
}

func (m *Tool) MCPToolName() string {
	return m.tool.Name
}

func (m *Tool) Info() unillm.ToolInfo {
	parameters := make(map[string]any)
	required := make([]string, 0)

	if input, ok := m.tool.InputSchema.(map[string]any); ok {
		if props, ok := input["properties"].(map[string]any); ok {
			parameters = props
		}
		if req, ok := input["required"].([]any); ok {
			// Convert []any -> []string when elements are strings
			for _, v := range req {
				if s, ok := v.(string); ok {
					required = append(required, s)
				}
			}
		} else if reqStr, ok := input["required"].([]string); ok {
			// Handle case where it's already []string
			required = reqStr
		}
	}

	return unillm.ToolInfo{
		Name:        m.Name(),
		Description: m.tool.Description,
		Parameters:  parameters,
		Required:    required,
	}
}

func (m *Tool) Run(ctx context.Context, params unillm.ToolCall) (unillm.ToolResponse, error) {
	sessionID := GetSessionFromContext(ctx)
	if sessionID == "" {
		return unillm.ToolResponse{}, fmt.Errorf("session ID is required for creating a new file")
	}

	// Skip permission for whitelisted Docker MCP tools.
	// Use the bound tool's actual name (m.Name()) instead of caller-supplied params.Name
	// to prevent permission bypass via manipulated params.Name.
	if !slices.Contains(whitelistDockerTools, m.Name()) {
		permissionDescription := fmt.Sprintf("execute %s with the following parameters:", m.Info().Name)
		p, err := m.permissions.Request(ctx,
			permission.CreatePermissionRequest{
				SessionID:   sessionID,
				ToolCallID:  params.ID,
				Path:        m.workingDir,
				ToolName:    m.Info().Name,
				Action:      "execute",
				Description: permissionDescription,
				Params:      params.Input,
			},
		)
		if err != nil {
			return unillm.ToolResponse{}, err
		}
		if !p {
			return unillm.ToolResponse{}, permission.ErrorPermissionDenied
		}
	}

	result, err := mcp.RunTool(ctx, m.cfg, m.mcpName, m.tool.Name, params.Input)
	if err != nil {
		return unillm.NewTextErrorResponse(err.Error()), nil
	}

	switch result.Type {
	case "image", "media":
		if !GetSupportsImagesFromContext(ctx) {
			modelName := GetModelNameFromContext(ctx)
			return unillm.NewTextErrorResponse(fmt.Sprintf("This model (%s) does not support image data.", modelName)), nil
		}

		return newMediaToolResponse(result.Content, result.MediaType, result.Data), nil
	default:
		return unillm.NewTextResponse(result.Content), nil
	}
}
