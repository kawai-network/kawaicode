package config

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

var dockerMCPVersionRunner = func(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "mcp", "version")
	return cmd.Run()
}

const dockerMCPAvailabilityTTL = 10 * time.Second

var dockerMCPAvailabilityCache struct {
	mu        sync.Mutex
	available bool
	checkedAt time.Time
	known     bool
}

// DockerMCPName is the name of the Docker MCP configuration.
const DockerMCPName = "docker"

// IsDockerMCPAvailable checks if Docker MCP is available by running
// 'docker mcp version'.
func IsDockerMCPAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := dockerMCPVersionRunner(ctx)
	return err == nil
}

// DockerMCPAvailabilityCached returns the cached Docker MCP availability and
// whether the cached value is still fresh.
func DockerMCPAvailabilityCached() (available bool, known bool) {
	dockerMCPAvailabilityCache.mu.Lock()
	defer dockerMCPAvailabilityCache.mu.Unlock()

	if !dockerMCPAvailabilityCache.known {
		return false, false
	}
	if time.Since(dockerMCPAvailabilityCache.checkedAt) > dockerMCPAvailabilityTTL {
		return dockerMCPAvailabilityCache.available, false
	}
	return dockerMCPAvailabilityCache.available, true
}

// RefreshDockerMCPAvailability refreshes and caches Docker MCP availability.
func RefreshDockerMCPAvailability() bool {
	available := IsDockerMCPAvailable()
	dockerMCPAvailabilityCache.mu.Lock()
	dockerMCPAvailabilityCache.available = available
	dockerMCPAvailabilityCache.checkedAt = time.Now()
	dockerMCPAvailabilityCache.known = true
	dockerMCPAvailabilityCache.mu.Unlock()
	return available
}

// IsDockerMCPEnabled checks if Docker MCP is already configured.
func (c *Config) IsDockerMCPEnabled() bool {
	if c.MCP == nil {
		return false
	}
	_, exists := c.MCP[DockerMCPName]
	return exists
}

// DockerMCPConfig returns the default Docker MCP stdio configuration.
func DockerMCPConfig() MCPConfig {
	return MCPConfig{
		Type:     MCPStdio,
		Command:  "docker",
		Args:     []string{"mcp", "gateway", "run"},
		Disabled: false,
	}
}

// PrepareDockerMCPConfig validates Docker MCP availability and stages the
// Docker MCP configuration in memory.
// Note: This modifies in-memory config directly. For atomic enable+persist,
// use EnableDockerMCP instead.
func (s *ConfigStore) PrepareDockerMCPConfig() (MCPConfig, error) {
	if !IsDockerMCPAvailable() {
		return MCPConfig{}, fmt.Errorf("docker mcp is not available, please ensure docker is installed and 'docker mcp version' succeeds")
	}

	mcpConfig := DockerMCPConfig()
	if s.config.MCP == nil {
		s.config.MCP = make(map[string]MCPConfig)
	}
	s.config.MCP[DockerMCPName] = mcpConfig
	return mcpConfig, nil
}

// PersistDockerMCPConfig persists a previously prepared Docker MCP
// configuration to the global config file.
func (s *ConfigStore) PersistDockerMCPConfig(mcpConfig MCPConfig) error {
	if err := s.SetConfigField(ScopeGlobal, "mcp."+DockerMCPName, mcpConfig); err != nil {
		return fmt.Errorf("failed to persist docker mcp configuration: %w", err)
	}
	return nil
}

// EnableDockerMCP adds Docker MCP configuration and persists it.
// Uses copy-on-write pattern: only updates in-memory config after
// successful persistence.
func (s *ConfigStore) EnableDockerMCP() error {
	if !IsDockerMCPAvailable() {
		return fmt.Errorf("docker mcp is not available, please ensure docker is installed and 'docker mcp version' succeeds")
	}

	mcpConfig := DockerMCPConfig()

	// Create a staged copy with Docker MCP added.
	staged := make(map[string]MCPConfig, len(s.config.MCP)+1)
	if s.config.MCP != nil {
		for k, v := range s.config.MCP {
			staged[k] = v
		}
	}
	staged[DockerMCPName] = mcpConfig

	// Persist the staged MCP map to the config file.
	if err := s.SetConfigField(ScopeGlobal, "mcp", staged); err != nil {
		return fmt.Errorf("failed to persist docker mcp configuration: %w", err)
	}

	// Only update in-memory config after successful persistence.
	s.config.MCP = staged
	return nil
}

// DisableDockerMCP removes Docker MCP configuration and persists the change.
func (s *ConfigStore) DisableDockerMCP() error {
	if s.config.MCP == nil {
		return nil
	}

	// Create a staged copy with Docker MCP removed.
	staged := make(map[string]MCPConfig, len(s.config.MCP)-1)
	for k, v := range s.config.MCP {
		if k != DockerMCPName {
			staged[k] = v
		}
	}

	// Persist the staged MCP map to the config file.
	if err := s.SetConfigField(ScopeGlobal, "mcp", staged); err != nil {
		return fmt.Errorf("failed to persist docker mcp removal: %w", err)
	}

	// Only update in-memory config after successful persistence.
	s.config.MCP = staged
	return nil
}
