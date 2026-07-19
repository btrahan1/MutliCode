package main

import (
	"context"
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
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		agentCancels:  make(map[string]context.CancelFunc),
		planApprovals: make(map[string]chan string),
		diffApprovals: make(map[string]chan string),
		pendingDiffs:  make(map[string]*DiffProposal),
		projectCmds:   make(map[string]*exec.Cmd),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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
