package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	goRuntime "runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type McpTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema struct {
		Type       string                 `json:"type"`
		Properties map[string]interface{} `json:"properties"`
		Required   []string               `json:"required"`
	} `json:"inputSchema"`
}

type JsonRpcRequest struct {
	JsonRpc string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id,omitempty"` // can be int64 or string, empty for notifications
}

type JsonRpcResponse struct {
	JsonRpc string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JsonRpcError   `json:"error,omitempty"`
	ID      *json.RawMessage `json:"id,omitempty"`
}

type JsonRpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type McpClient struct {
	Name       string
	Config     McpServerConfig
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderrBuf  *bytes.Buffer
	nextID     int64
	pending    map[string]chan []byte
	pendingMu  sync.Mutex
	Tools      []McpTool
	IsReady    bool
	LastError  string
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewMcpClient(name string, config McpServerConfig) *McpClient {
	return &McpClient{
		Name:    name,
		Config:  config,
		pending: make(map[string]chan []byte),
	}
}

func (m *McpClient) Start() (err error) {
	m.LastError = ""
	defer func() {
		if err != nil {
			m.pendingMu.Lock()
			if m.LastError == "" {
				m.LastError = err.Error()
			}
			m.pendingMu.Unlock()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel

	command := m.Config.Command
	args := m.Config.Args
	if goRuntime.GOOS == "windows" {
		if command == "npx" {
			command = "npx.cmd"
		} else if command == "npm" {
			command = "npm.cmd"
		}
	}

	cmd := exec.Command(command, args...)
	if goRuntime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow: true,
		}
	}
	if len(m.Config.Env) > 0 {
		env := os.Environ()
		for k, v := range m.Config.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}

	m.stderrBuf = new(bytes.Buffer)
	cmd.Stderr = m.stderrBuf

	if err = cmd.Start(); err != nil {
		cancel()
		return err
	}

	m.stdin = stdin
	m.stdout = stdout

	// Start reading standard output in background goroutine
	m.wg.Add(1)
	go m.readLoop()

	// Monitor process exit and stderr in background goroutine
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		waitErr := cmd.Wait()
		m.pendingMu.Lock()
		if waitErr != nil {
			m.LastError = fmt.Sprintf("Process exited: %v. Stderr: %s", waitErr, m.stderrBuf.String())
		} else {
			m.LastError = "Process exited cleanly"
		}
		m.pendingMu.Unlock()
		cancel()
	}()

	// Perform Handshake
	if err = m.handshake(); err != nil {
		m.Close()
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Fetch tools
	if err = m.refreshTools(); err != nil {
		m.Close()
		return fmt.Errorf("failed to fetch tools: %w", err)
	}

	m.IsReady = true
	return nil
}

func (m *McpClient) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.stdin != nil {
		_ = m.stdin.Close()
	}
	if m.cmd != nil {
		_ = m.cmd.Process.Kill()
	}
	m.wg.Wait()
	m.IsReady = false
}

func (m *McpClient) sendRequest(method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&m.nextID, 1)
	idStr := fmt.Sprintf("%d", id)

	req := JsonRpcRequest{
		JsonRpc: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	jsonBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan []byte, 1)
	m.pendingMu.Lock()
	m.pending[idStr] = ch
	m.pendingMu.Unlock()

	defer func() {
		m.pendingMu.Lock()
		delete(m.pending, idStr)
		m.pendingMu.Unlock()
	}()

	// Write payload with trailing newline
	payload := append(jsonBytes, '\n')
	if _, err := m.stdin.Write(payload); err != nil {
		return nil, fmt.Errorf("write to stdin failed: %w", err)
	}

	select {
	case respBytes := <-ch:
		var resp JsonRpcResponse
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			return nil, err
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp.Result, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("request timed out after 30 seconds")
	case <-m.ctx.Done():
		return nil, fmt.Errorf("client closed")
	}
}

func (m *McpClient) sendNotification(method string, params interface{}) error {
	req := JsonRpcRequest{
		JsonRpc: "2.0",
		Method:  method,
		Params:  params,
	}

	jsonBytes, err := json.Marshal(req)
	if err != nil {
		return err
	}

	payload := append(jsonBytes, '\n')
	if _, err := m.stdin.Write(payload); err != nil {
		return err
	}

	return nil
}

func (m *McpClient) handshake() error {
	initParams := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "MultiCode",
			"version": "1.0.0",
		},
	}

	_, err := m.sendRequest("initialize", initParams)
	if err != nil {
		return err
	}

	return m.sendNotification("notifications/initialized", map[string]interface{}{})
}

func (m *McpClient) refreshTools() error {
	resultBytes, err := m.sendRequest("tools/list", nil)
	if err != nil {
		return err
	}

	var result struct {
		Tools []McpTool `json:"tools"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return err
	}

	m.Tools = result.Tools
	return nil
}

func (m *McpClient) CallTool(name string, arguments map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}

	resultBytes, err := m.sendRequest("tools/call", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}

	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	for _, c := range result.Content {
		if c.Type == "text" {
			buf.WriteString(c.Text)
		}
	}

	output := buf.String()
	if result.IsError {
		return output, fmt.Errorf("tool execution failed: %s", output)
	}

	return output, nil
}

func (m *McpClient) readLoop() {
	defer m.wg.Done()
	reader := bufio.NewReader(m.stdout)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				m.LastError = fmt.Sprintf("stdout read error: %v", err)
			}
			break
		}

		if len(line) == 0 {
			continue
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		idRaw, exists := raw["id"]
		if !exists {
			// This is a notification or request from server to client, ignore for now
			continue
		}

		var idStr string
		// Try to unmarshal as string or number
		var idInt int64
		if err := json.Unmarshal(idRaw, &idInt); err == nil {
			idStr = fmt.Sprintf("%d", idInt)
		} else {
			var idVal string
			if err := json.Unmarshal(idRaw, &idVal); err == nil {
				idStr = idVal
			} else {
				idStr = string(idRaw)
			}
		}

		m.pendingMu.Lock()
		ch, ok := m.pending[idStr]
		m.pendingMu.Unlock()

		if ok {
			select {
			case ch <- line:
			default:
			}
		}
	}
}
