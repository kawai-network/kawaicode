package tools

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/getkawai/unillm"
	"github.com/charmbracelet/crush/internal/shell"
)

const (
	JobKillToolName = "job_kill"
)

//go:embed job_kill.md
var jobKillDescription []byte

type JobKillParams struct {
	ShellID string `json:"shell_id" description:"The ID of the background shell to terminate"`
}

type JobKillResponseMetadata struct {
	ShellID     string `json:"shell_id"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

func NewJobKillTool() unillm.AgentTool {
	return unillm.NewAgentTool(
		JobKillToolName,
		string(jobKillDescription),
		func(ctx context.Context, params JobKillParams, call unillm.ToolCall) (unillm.ToolResponse, error) {
			if params.ShellID == "" {
				return unillm.NewTextErrorResponse("missing shell_id"), nil
			}

			bgManager := shell.GetBackgroundShellManager()

			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return unillm.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			metadata := JobKillResponseMetadata{
				ShellID:     params.ShellID,
				Command:     bgShell.Command,
				Description: bgShell.Description,
			}

			err := bgManager.Kill(params.ShellID)
			if err != nil {
				return unillm.NewTextErrorResponse(err.Error()), nil
			}

			result := fmt.Sprintf("Background shell %s terminated successfully", params.ShellID)
			return unillm.WithResponseMetadata(unillm.NewTextResponse(result), metadata), nil
		})
}
