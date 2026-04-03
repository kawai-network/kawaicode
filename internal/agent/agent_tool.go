package agent

import (
	"context"
	_ "embed"
	"errors"

	"github.com/getkawai/unillm"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
)

//go:embed templates/agent_tool.md
var agentToolDescription []byte

type AgentParams struct {
	Prompt string `json:"prompt" description:"The task for the agent to perform"`
}

const (
	AgentToolName = "agent"
)

func (c *coordinator) agentTool(ctx context.Context) (unillm.AgentTool, error) {
	agentCfg, ok := c.cfg.Config().Agents[config.AgentTask]
	if !ok {
		return nil, errors.New("task agent not configured")
	}
	prompt, err := taskPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}

	agent, err := c.buildAgent(ctx, prompt, agentCfg, true)
	if err != nil {
		return nil, err
	}
	return unillm.NewParallelAgentTool(
		AgentToolName,
		string(agentToolDescription),
		func(ctx context.Context, params AgentParams, call unillm.ToolCall) (unillm.ToolResponse, error) {
			if params.Prompt == "" {
				return unillm.NewTextErrorResponse("prompt is required"), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return unillm.ToolResponse{}, errors.New("session id missing from context")
			}

			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return unillm.ToolResponse{}, errors.New("agent message id missing from context")
			}

			return c.runSubAgent(ctx, subAgentParams{
				Agent:          agent,
				SessionID:      sessionID,
				AgentMessageID: agentMessageID,
				ToolCallID:     call.ID,
				Prompt:         params.Prompt,
				SessionTitle:   "New Agent Session",
			})
		}), nil
}
