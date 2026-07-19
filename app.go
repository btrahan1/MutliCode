package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
)

type DiffProposal struct {
	FilePath        string `json:"filePath"`
	OriginalContent string `json:"originalContent"`
	ProposedContent string `json:"proposedContent"`
}

// App struct
type App struct {
	ctx             context.Context
	agentCancels    map[string]context.CancelFunc
	cancelsMu       sync.Mutex
	planApprovals   map[string]chan string
	planApprovalsMu sync.Mutex
	diffApprovals   map[string]chan string
	diffApprovalsMu sync.Mutex
	pendingDiffs    map[string]*DiffProposal
	pendingDiffsMu  sync.Mutex
	projectCmds     map[string]*exec.Cmd
	projectCmdsMu   sync.Mutex
	mcpClients      map[string]*McpClient
	mcpClientsMu    sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		agentCancels:  make(map[string]context.CancelFunc),
		planApprovals: make(map[string]chan string),
		diffApprovals: make(map[string]chan string),
		pendingDiffs:  make(map[string]*DiffProposal),
		projectCmds:   make(map[string]*exec.Cmd),
		mcpClients:    make(map[string]*McpClient),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.initMcpServers()
}

// shutdown is called when the app exits.
func (a *App) shutdown(ctx context.Context) {
	a.mcpClientsMu.Lock()
	defer a.mcpClientsMu.Unlock()
	for _, client := range a.mcpClients {
		client.Close()
	}
}

func (a *App) initMcpServers() {
	settings, err := a.LoadSettings()
	if err != nil {
		fmt.Printf("[MCP] Failed to load settings for MCP initialization: %v\n", err)
		return
	}

	a.mcpClientsMu.Lock()
	defer a.mcpClientsMu.Unlock()

	for name, config := range settings.McpServers {
		fmt.Printf("[MCP] Launching server '%s': %s %v\n", name, config.Command, config.Args)
		client := NewMcpClient(name, config)
		a.mcpClients[name] = client
		go func(c *McpClient) {
			if err := c.Start(); err != nil {
				fmt.Printf("[MCP] Server '%s' failed to start: %v\n", c.Name, err)
			} else {
				fmt.Printf("[MCP] Server '%s' initialized successfully. Discovered %d tools.\n", c.Name, len(c.Tools))
			}
		}(client)
	}
}

func (a *App) GetMcpServersStatus() map[string]string {
	a.mcpClientsMu.Lock()
	defer a.mcpClientsMu.Unlock()

	status := make(map[string]string)
	for name, client := range a.mcpClients {
		if client.IsReady {
			status[name] = "connected"
		} else if client.LastError != "" {
			status[name] = fmt.Sprintf("error: %s", client.LastError)
		} else {
			status[name] = "disconnected"
		}
	}
	return status
}

func (a *App) ReloadMcpServers(configJSON string) error {
	var mcpConfigs map[string]McpServerConfig
	if err := json.Unmarshal([]byte(configJSON), &mcpConfigs); err != nil {
		return fmt.Errorf("invalid config JSON: %w", err)
	}

	// Load existing settings, update and save
	settings, err := a.LoadSettings()
	if err == nil {
		settings.McpServers = mcpConfigs
		_ = a.SaveSettings(settings)
	}

	a.mcpClientsMu.Lock()
	for _, client := range a.mcpClients {
		client.Close()
	}
	a.mcpClients = make(map[string]*McpClient)
	a.mcpClientsMu.Unlock()

	a.initMcpServers()
	return nil
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

func (a *App) ApproveDiff(tabID string) error {
	a.diffApprovalsMu.Lock()
	ch, exists := a.diffApprovals[tabID]
	a.diffApprovalsMu.Unlock()

	if exists {
		ch <- "approve"
		return nil
	}
	return fmt.Errorf("no pending diff approval found for tab: %s", tabID)
}

func (a *App) RejectDiff(tabID string) error {
	a.diffApprovalsMu.Lock()
	ch, exists := a.diffApprovals[tabID]
	a.diffApprovalsMu.Unlock()

	if exists {
		ch <- "reject"
		return nil
	}
	return fmt.Errorf("no pending diff approval found for tab: %s", tabID)
}

func (a *App) GetPendingDiff(tabID string) (*DiffProposal, error) {
	a.pendingDiffsMu.Lock()
	diff, exists := a.pendingDiffs[tabID]
	a.pendingDiffsMu.Unlock()

	if exists {
		return diff, nil
	}
	return nil, fmt.Errorf("no pending diff found for tab: %s", tabID)
}
