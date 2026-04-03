package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filetracker"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/getkawai/unillm"
	"github.com/kawai-network/x/llm"
	"golang.org/x/sync/errgroup"
)

// Coordinator errors.
var (
	errCoderAgentNotConfigured = errors.New("coder agent not configured")
)

type Coordinator interface {
	// INFO: (kujtim) this is not used yet we will use this when we have multiple agents
	// SetMainAgent(string)
	Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) (*unillm.AgentResult, error)
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string) error
	Model() Model
	UpdateModels(ctx context.Context) error
}

type coordinator struct {
	cfg         *config.ConfigStore
	sessions    session.Service
	messages    message.Service
	permissions permission.Service
	history     history.Service
	filetracker filetracker.Service
	lspManager  *lsp.Manager
	notify      pubsub.Publisher[notify.Notification]

	currentAgent SessionAgent
	agents       map[string]SessionAgent

	readyWg errgroup.Group
}

func NewCoordinator(
	ctx context.Context,
	cfg *config.ConfigStore,
	sessions session.Service,
	messages message.Service,
	permissions permission.Service,
	history history.Service,
	filetracker filetracker.Service,
	lspManager *lsp.Manager,
	notify pubsub.Publisher[notify.Notification],
) (Coordinator, error) {
	c := &coordinator{
		cfg:         cfg,
		sessions:    sessions,
		messages:    messages,
		permissions: permissions,
		history:     history,
		filetracker: filetracker,
		lspManager:  lspManager,
		notify:      notify,
		agents:      make(map[string]SessionAgent),
	}

	agentCfg, ok := cfg.Config().Agents[config.AgentCoder]
	if !ok {
		return nil, errCoderAgentNotConfigured
	}

	// TODO: make this dynamic when we support multiple agents
	prompt, err := coderPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}

	agent, err := c.buildAgent(ctx, prompt, agentCfg, false)
	if err != nil {
		return nil, err
	}
	c.currentAgent = agent
	c.agents[config.AgentCoder] = agent
	return c, nil
}

// Run implements Coordinator.
func (c *coordinator) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) (*unillm.AgentResult, error) {
	if err := c.readyWg.Wait(); err != nil {
		return nil, err
	}

	// refresh models before each run
	if err := c.UpdateModels(ctx); err != nil {
		return nil, fmt.Errorf("failed to update models: %w", err)
	}

	model := c.currentAgent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	if !model.CatwalkCfg.SupportsImages && attachments != nil {
		// filter out image attachments
		filteredAttachments := make([]message.Attachment, 0, len(attachments))
		for _, att := range attachments {
			if att.IsText() {
				filteredAttachments = append(filteredAttachments, att)
			}
		}
		attachments = filteredAttachments
	}

	return c.currentAgent.Run(ctx, SessionAgentCall{
		SessionID:       sessionID,
		Prompt:          prompt,
		Attachments:     attachments,
		MaxOutputTokens: maxTokens,
	})
}

func (c *coordinator) buildAgent(ctx context.Context, prompt *prompt.Prompt, agent config.Agent, isSubAgent bool) (SessionAgent, error) {
	large, small, err := c.buildAgentModels(ctx, isSubAgent)
	if err != nil {
		return nil, err
	}

	result := NewSessionAgent(SessionAgentOptions{
		LargeModel:           large,
		SmallModel:           small,
		SystemPromptPrefix:   "",
		SystemPrompt:         "",
		IsSubAgent:           isSubAgent,
		DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
		IsYolo:               c.permissions.SkipRequests(),
		Sessions:             c.sessions,
		Messages:             c.messages,
		Tools:                nil,
		Notify:               c.notify,
	})

	c.readyWg.Go(func() error {
		systemPrompt, err := prompt.Build(ctx, large.Model.Provider(), large.Model.Model(), c.cfg)
		if err != nil {
			return err
		}
		result.SetSystemPrompt(systemPrompt)
		return nil
	})

	c.readyWg.Go(func() error {
		tools, err := c.buildTools(ctx, agent)
		if err != nil {
			return err
		}
		result.SetTools(tools)
		return nil
	})

	return result, nil
}

func (c *coordinator) buildTools(ctx context.Context, agent config.Agent) ([]unillm.AgentTool, error) {
	var allTools []unillm.AgentTool
	if slices.Contains(agent.AllowedTools, AgentToolName) {
		agentTool, err := c.agentTool(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agentTool)
	}

	if slices.Contains(agent.AllowedTools, tools.AgenticFetchToolName) {
		agenticFetchTool, err := c.agenticFetchTool(ctx, nil)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agenticFetchTool)
	}

	// Get the model name for the agent
	modelName := ""
	if modelCfg, ok := c.cfg.Config().Models[agent.Model]; ok {
		if model := c.cfg.Config().GetModel(modelCfg.Provider, modelCfg.Model); model != nil {
			modelName = model.Name
		}
	}

	allTools = append(allTools,
		tools.NewBashTool(c.permissions, c.cfg.WorkingDir(), c.cfg.Config().Options.Attribution, modelName),
		tools.NewJobOutputTool(),
		tools.NewJobKillTool(),
		tools.NewDownloadTool(c.permissions, c.cfg.WorkingDir(), nil),
		tools.NewEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		tools.NewMultiEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		tools.NewFetchTool(c.permissions, c.cfg.WorkingDir(), nil),
		tools.NewGlobTool(c.cfg.WorkingDir()),
		tools.NewGrepTool(c.cfg.WorkingDir(), c.cfg.Config().Tools.Grep),
		tools.NewLsTool(c.permissions, c.cfg.WorkingDir(), c.cfg.Config().Tools.Ls),
		tools.NewSourcegraphTool(nil),
		tools.NewTodosTool(c.sessions),
		tools.NewViewTool(c.lspManager, c.permissions, c.filetracker, c.cfg.WorkingDir(), c.cfg.Config().Options.SkillsPaths...),
		tools.NewWriteTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
	)

	// Add LSP tools if user has configured LSPs or auto_lsp is enabled (nil or true).
	if len(c.cfg.Config().LSP) > 0 || c.cfg.Config().Options.AutoLSP == nil || *c.cfg.Config().Options.AutoLSP {
		allTools = append(allTools, tools.NewDiagnosticsTool(c.lspManager), tools.NewReferencesTool(c.lspManager), tools.NewLSPRestartTool(c.lspManager))
	}

	if len(c.cfg.Config().MCP) > 0 {
		allTools = append(
			allTools,
			tools.NewListMCPResourcesTool(c.cfg, c.permissions),
			tools.NewReadMCPResourceTool(c.cfg, c.permissions),
		)
	}

	var filteredTools []unillm.AgentTool
	for _, tool := range allTools {
		if slices.Contains(agent.AllowedTools, tool.Info().Name) {
			filteredTools = append(filteredTools, tool)
		}
	}

	for _, tool := range tools.GetMCPTools(c.permissions, c.cfg, c.cfg.WorkingDir()) {
		if agent.AllowedMCP == nil {
			// No MCP restrictions
			filteredTools = append(filteredTools, tool)
			continue
		}
		if len(agent.AllowedMCP) == 0 {
			// No MCPs allowed
			slog.Debug("No MCPs allowed", "tool", tool.Name(), "agent", agent.Name)
			break
		}

		for mcp, tools := range agent.AllowedMCP {
			if mcp != tool.MCP() {
				continue
			}
			if len(tools) == 0 || slices.Contains(tools, tool.MCPToolName()) {
				filteredTools = append(filteredTools, tool)
				break
			}
			slog.Debug("MCP not allowed", "tool", tool.Name(), "agent", agent.Name)
		}
	}
	slices.SortFunc(filteredTools, func(a, b unillm.AgentTool) int {
		return strings.Compare(a.Info().Name, b.Info().Name)
	})
	return filteredTools, nil
}

// TODO: when we support multiple agents we need to change this so that we pass in the agent specific model config
func (c *coordinator) buildAgentModels(ctx context.Context, isSubAgent bool) (Model, Model, error) {
	chain := llm.BuildChain(llm.ModelChainConfig{
		Context:  ctx,
		TaskName: "crush",
	})
	if chain == nil {
		return Model{}, Model{}, fmt.Errorf("no models available in chain")
	}

	modelName := chain.Model()
	provider := chain.Provider()

	largeCatwalk := catwalk.Model{
		ID:               modelName,
		Name:             modelName,
		DefaultMaxTokens: 8192,
		SupportsImages:   true,
		CanReason:        false,
	}
	smallCatwalk := catwalk.Model{
		ID:               modelName,
		Name:             modelName,
		DefaultMaxTokens: 2048,
		SupportsImages:   true,
		CanReason:        false,
	}

	largeCfg := config.SelectedModel{
		Model:    modelName,
		Provider: provider,
	}
	smallCfg := config.SelectedModel{
		Model:    modelName,
		Provider: provider,
	}

	return Model{
			Model:      chain,
			CatwalkCfg: largeCatwalk,
			ModelCfg:   largeCfg,
		}, Model{
			Model:      chain,
			CatwalkCfg: smallCatwalk,
			ModelCfg:   smallCfg,
		}, nil
}

func (c *coordinator) Cancel(sessionID string) {
	c.currentAgent.Cancel(sessionID)
}

func (c *coordinator) CancelAll() {
	c.currentAgent.CancelAll()
}

func (c *coordinator) ClearQueue(sessionID string) {
	c.currentAgent.ClearQueue(sessionID)
}

func (c *coordinator) IsBusy() bool {
	return c.currentAgent.IsBusy()
}

func (c *coordinator) IsSessionBusy(sessionID string) bool {
	return c.currentAgent.IsSessionBusy(sessionID)
}

func (c *coordinator) Model() Model {
	return c.currentAgent.Model()
}

func (c *coordinator) UpdateModels(ctx context.Context) error {
	// build the models again so we make sure we get the latest config
	large, small, err := c.buildAgentModels(ctx, false)
	if err != nil {
		return err
	}
	c.currentAgent.SetModels(large, small)

	agentCfg, ok := c.cfg.Config().Agents[config.AgentCoder]
	if !ok {
		return errCoderAgentNotConfigured
	}

	tools, err := c.buildTools(ctx, agentCfg)
	if err != nil {
		return err
	}
	c.currentAgent.SetTools(tools)
	return nil
}

func (c *coordinator) QueuedPrompts(sessionID string) int {
	return c.currentAgent.QueuedPrompts(sessionID)
}

func (c *coordinator) QueuedPromptsList(sessionID string) []string {
	return c.currentAgent.QueuedPromptsList(sessionID)
}

func (c *coordinator) Summarize(ctx context.Context, sessionID string) error {
	return c.currentAgent.Summarize(ctx, sessionID, unillm.ProviderOptions{})
}

// subAgentParams holds the parameters for running a sub-agent.
type subAgentParams struct {
	Agent          SessionAgent
	SessionID      string
	AgentMessageID string
	ToolCallID     string
	Prompt         string
	SessionTitle   string
	// SessionSetup is an optional callback invoked after session creation
	// but before agent execution, for custom session configuration.
	SessionSetup func(sessionID string)
}

// runSubAgent runs a sub-agent and handles session management and cost accumulation.
// It creates a sub-session, runs the agent with the given prompt, and propagates
// the cost to the parent session.
func (c *coordinator) runSubAgent(ctx context.Context, params subAgentParams) (unillm.ToolResponse, error) {
	// Create sub-session
	agentToolSessionID := c.sessions.CreateAgentToolSessionID(params.AgentMessageID, params.ToolCallID)
	session, err := c.sessions.CreateTaskSession(ctx, agentToolSessionID, params.SessionID, params.SessionTitle)
	if err != nil {
		return unillm.ToolResponse{}, fmt.Errorf("create session: %w", err)
	}

	// Call session setup function if provided
	if params.SessionSetup != nil {
		params.SessionSetup(session.ID)
	}

	// Get model configuration
	model := params.Agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	// Run the agent
	result, err := params.Agent.Run(ctx, SessionAgentCall{
		SessionID:       session.ID,
		Prompt:          params.Prompt,
		MaxOutputTokens: maxTokens,
		NonInteractive:  true,
	})
	if err != nil {
		return unillm.NewTextErrorResponse("error generating response"), nil
	}

	// Update parent session cost
	if err := c.updateParentSessionCost(ctx, session.ID, params.SessionID); err != nil {
		return unillm.ToolResponse{}, err
	}

	return unillm.NewTextResponse(result.Response.Content.Text()), nil
}

// updateParentSessionCost accumulates the cost from a child session to its parent session.
func (c *coordinator) updateParentSessionCost(ctx context.Context, childSessionID, parentSessionID string) error {
	childSession, err := c.sessions.Get(ctx, childSessionID)
	if err != nil {
		return fmt.Errorf("get child session: %w", err)
	}

	parentSession, err := c.sessions.Get(ctx, parentSessionID)
	if err != nil {
		return fmt.Errorf("get parent session: %w", err)
	}

	parentSession.Cost += childSession.Cost

	if _, err := c.sessions.Save(ctx, parentSession); err != nil {
		return fmt.Errorf("save parent session: %w", err)
	}

	return nil
}
