package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type TerminalSession struct {
	Cmd    *exec.Cmd
	Stdin  io.WriteCloser
	Cancel context.CancelFunc
}

var (
	termSessions   = make(map[string]*TerminalSession)
	termSessionsMu sync.Mutex
)

type TerminalOutputEvent struct {
	TabID string `json:"tabId"`
	Data  string `json:"data"`
}

func (a *App) StartTerminal(tabID string, workspacePath string) error {
	termSessionsMu.Lock()
	defer termSessionsMu.Unlock()

	// Stop existing session if any
	if old, exists := termSessions[tabID]; exists {
		old.Cancel()
		delete(termSessions, tabID)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var cmd *exec.Cmd
	if os.Getenv("OS") == "Windows_NT" {
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoExit")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow: true,
		}
	} else {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd = exec.CommandContext(ctx, shell, "-i")
	}
	cmd.Dir = workspacePath

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start terminal process: %w", err)
	}

	session := &TerminalSession{
		Cmd:    cmd,
		Stdin:  stdin,
		Cancel: cancel,
	}
	termSessions[tabID] = session

	// Read stdout in a goroutine (directly from pipe to avoid delay buffering)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				runtime.EventsEmit(a.ctx, "terminal:output", TerminalOutputEvent{
					TabID: tabID,
					Data:  string(buf[:n]),
				})
			}
			if err != nil {
				break
			}
		}
	}()

	// Read stderr in a goroutine (directly from pipe to avoid delay buffering)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				runtime.EventsEmit(a.ctx, "terminal:output", TerminalOutputEvent{
					TabID: tabID,
					Data:  string(buf[:n]),
				})
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for process termination
	go func() {
		_ = cmd.Wait()
		cancel()
		termSessionsMu.Lock()
		if current, exists := termSessions[tabID]; exists && current == session {
			delete(termSessions, tabID)
		}
		termSessionsMu.Unlock()
	}()

	return nil
}

func (a *App) SendTerminalInput(tabID string, input string) error {
	termSessionsMu.Lock()
	session, exists := termSessions[tabID]
	termSessionsMu.Unlock()

	if !exists {
		return fmt.Errorf("no terminal session active for tab: %s", tabID)
	}

	// On Windows, translate standard backspace keycode \x7f to \x08
	if os.Getenv("OS") == "Windows_NT" {
		input = strings.ReplaceAll(input, "\x7f", "\x08")
	}

	_, err := session.Stdin.Write([]byte(input))
	return err
}

func (a *App) StopTerminal(tabID string) {
	termSessionsMu.Lock()
	if session, exists := termSessions[tabID]; exists {
		session.Cancel()
		delete(termSessions, tabID)
	}
	termSessionsMu.Unlock()
}

// ResizeTerminal tells the shell process to resize its internal buffer to match the xterm viewport.
// On Windows (PowerShell), we send commands directly to set the console buffer and window width.
func (a *App) ResizeTerminal(tabID string, cols int, rows int) error {
	termSessionsMu.Lock()
	session, exists := termSessions[tabID]
	termSessionsMu.Unlock()

	if !exists {
		return nil
	}

	if cols <= 0 || rows <= 0 {
		return nil
	}

	if os.Getenv("OS") == "Windows_NT" {
		// Set PowerShell buffer and window size to match xterm viewport
		cmd := fmt.Sprintf("[Console]::BufferWidth = %d; [Console]::WindowWidth = %d; [Console]::BufferHeight = 9999\r\n", cols, cols)
		_, err := session.Stdin.Write([]byte(cmd))
		return err
	}

	return nil
}
