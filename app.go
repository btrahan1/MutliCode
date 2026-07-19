package main

import (
	"context"
	"fmt"
	"sync"
)

// App struct
type App struct {
	ctx             context.Context
	agentCancels    map[string]context.CancelFunc
	cancelsMu       sync.Mutex
	planApprovals   map[string]chan string
	planApprovalsMu sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		agentCancels:  make(map[string]context.CancelFunc),
		planApprovals: make(map[string]chan string),
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
