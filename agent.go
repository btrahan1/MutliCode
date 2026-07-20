package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type TaskItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"` // "pending" | "in_progress" | "completed" | "failed"
}

type AgentPlan struct {
	Description string     `json:"description"`
	Tasks       []TaskItem `json:"tasks"`
}

type AgentEvent struct {
	TabID    string        `json:"tabId"`
	Type     string        `json:"type"` // "message" | "status" | "history_update" | "plan" | "command_approval" | "diff_proposal"
	Message  ChatMessage   `json:"message"`
	Messages []ChatMessage `json:"messages"`
	Status   string        `json:"status"` // "idle" | "running" | "completed" | "waiting_for_approval" | "waiting_for_command_approval" | "waiting_for_diff_approval"
	Plan     *AgentPlan    `json:"plan,omitempty"`
	Command  string        `json:"command,omitempty"`
	FilePath string        `json:"filePath,omitempty"`
}

// StartAgent launches the background agent execution loop.
func (a *App) StartAgent(tabID string, workspacePath string, modelName string, prompt string, image string, history []ChatMessage) {
	ctx, cancel := context.WithCancel(context.Background())

	a.cancelsMu.Lock()
	if oldCancel, exists := a.agentCancels[tabID]; exists {
		oldCancel()
	}
	a.agentCancels[tabID] = cancel
	a.cancelsMu.Unlock()

	go func() {
		defer func() {
			a.cancelsMu.Lock()
			if currentCancel, exists := a.agentCancels[tabID]; exists {
				_ = currentCancel
				delete(a.agentCancels, tabID)
			}
			a.cancelsMu.Unlock()
		}()

		// Emit initial running status
		a.emitAgentStatus(tabID, "running")

		// Run pre-flight context bootstrap to inject files related to prompt & workspace blueprints
		bootstrappedCtx := a.BootstrapContext(workspacePath, prompt)
		finalPrompt := prompt
		if bootstrappedCtx != "" {
			finalPrompt = prompt + "\n" + bootstrappedCtx
		}

		// Create workspace message history
		messages := append(history, ChatMessage{Role: "user", Content: finalPrompt, Image: image})

		failedReplaces := make(map[string]int)
		var currentPlan *AgentPlan

		for {
			// Check if cancelled
			if ctx.Err() != nil {
				a.emitAgentMessage(tabID, ChatMessage{
					Role:    "assistant",
					Content: "[Agent execution stopped by user]",
				})
				a.emitAgentStatus(tabID, "completed")
				return
			}

			// Load settings dynamically
			appSettings, _ := a.LoadSettings()
			projectSettings, _ := a.GetProjectSettings(workspacePath)

			// 1. Build directory structure layout or Repo Map
			dirLayout := ""
			if appSettings.UseRepoMap {
				activeFiles := a.GetActiveFiles(messages)
				rme := NewRepoMapEngine(workspacePath)
				repoMap, err := rme.BuildRepoMap(activeFiles, appSettings.RepoMapTokens, func(path string) bool {
					return a.IsPathIgnored(workspacePath, path)
				})
				if err != nil {
					tree, err := a.GetDirectoryTree(workspacePath)
					if err == nil {
						dirLayout = serializeTree(tree, "", 0)
					}
				} else {
					dirLayout = repoMap
				}
			} else {
				tree, err := a.GetDirectoryTree(workspacePath)
				if err == nil {
					dirLayout = serializeTree(tree, "", 0)
				}
			}

			var toolList []string
			toolList = append(toolList, `1. Read file contents:
___xml
<tool name="read_file">
  <path>relative/path/to/file.ext</path>
  <start_line>1</start_line>   <!-- Optional: start line -->
  <end_line>50</end_line>     <!-- Optional: end line -->
</tool>
___`)

			if appSettings.EnableSearchCode {
				toolList = append(toolList, `2. Find files containing a text string (Workspace search):
___xml
<tool name="search_code">
  <command>string to search for</command>
</tool>
___`)
			}

			toolIndex := len(toolList) + 1
			toolList = append(toolList, fmt.Sprintf(`%d. Surgical find-and-replace text edits (Preferred over full rewrites for edits):
___xml
<tool name="replace_text">
  <path>relative/path/to/file.ext</path>
  <target>
  exact code block to change
  </target>
  <replacement>
  new code block to insert
  </replacement>
  <start_line>10</start_line> <!-- Optional: limit search scope -->
  <end_line>30</end_line>   <!-- Optional: limit search scope -->
</tool>
___`, toolIndex))

			toolIndex++
			toolList = append(toolList, fmt.Sprintf(`%d. Write a new file or fully overwrite an existing file:
___xml
<tool name="write_file">
  <path>relative/path/to/file.ext</path>
  <content>
  full file content here
  </content>
</tool>
___`, toolIndex))

			toolIndex++
			toolList = append(toolList, fmt.Sprintf(`%d. Run a shell command in the terminal:
___xml
<tool name="run_command">
  <command>npm run build</command>
</tool>
___`, toolIndex))

			toolIndex++
			toolList = append(toolList, fmt.Sprintf(`%d. Submit an execution plan and task checklist (MANDATORY at the start of any new task):
___xml
<tool name="submit_plan">
  <description>High-level plan summary/description</description>
  <tasks>
    <task id="task1">First discrete step description</task>
    <task id="task2">Second discrete step description</task>
  </tasks>
</tool>
___`, toolIndex))

			toolIndex++
			toolList = append(toolList, fmt.Sprintf(`%d. Update the progress status of a plan task:
___xml
<tool name="update_task">
  <id>task1</id>
  <status>in_progress</status> <!-- 'pending' | 'in_progress' | 'completed' | 'failed' -->
</tool>
___`, toolIndex))

			// Append active MCP server tools dynamically
			a.mcpClientsMu.Lock()
			for _, client := range a.mcpClients {
				if !client.IsReady {
					continue
				}
				for _, mcpTool := range client.Tools {
					toolIndex++
					schemaBytes, _ := json.MarshalIndent(mcpTool.InputSchema, "", "  ")
					toolList = append(toolList, fmt.Sprintf(`%d. MCP Tool: %s (Provided by server '%s')
Description: %s
___xml
<tool name="%s">
  <!-- Provide arguments inside <content> as a single JSON object matching this schema:
%s
  -->
  <content>
    {
      "arg1": "value"
    }
  </content>
</tool>
___`, toolIndex, mcpTool.Name, client.Name, mcpTool.Description, mcpTool.Name, string(schemaBytes)))
				}
			}
			a.mcpClientsMu.Unlock()

			toolsSpec := strings.Join(toolList, "\n\n")

			systemPromptRaw := `You are MultiCode Agent, an autonomous coding assistant connected to my developer workspace.
You can read and modify files, scan directories, and run commands.
My active workspace folder is: %s

Here is the current directory structure:
%s

### TIGHT TOOLKIT SPECIFICATIONS:
You can invoke the following tools using XML blocks. Output ONLY one tool block at a time, wait for the response (which will be returned as '### TOOL OUTPUT:'), and then decide the next step.

%s

### RULES & GUIDELINES:` + (func() string {
				var rules []string
				if appSettings.EnableSearchCode {
					rules = append(rules, "- **Use search_code to locate code**: If you do not know where a class or function is defined, use <tool name=\"search_code\"> to locate it before reading files.")
				}
				if appSettings.EnforcePlanning {
					rules = append(rules, "- **MANDATORY PLANNING PHASE:** Before executing any code changes, file creation, or terminal commands, you MUST submit a step-by-step plan using <tool name=\"submit_plan\">. Wait for user approval before proceeding.",
						"- **TICK OFF TASKS:** Always mark tasks as 'in_progress', 'completed', or 'failed' using <tool name=\"update_task\"> as you work through your plan.")
				}
				if len(rules) > 0 {
					return "\n" + strings.Join(rules, "\n")
				}
				return ""
			})() + `
- **Use replace_text for modifications**: If a file already exists, always prefer <tool name="replace_text"> instead of <tool name="write_file">.
- **One Action per Message**: Do not combine multiple tool calls in a single message.
- **Wrap in Markdown Code Blocks**: Always wrap your XML tool block in a ___xml ... ___ code block.
- **No placeholders**: Do not use placeholders or comments like '// rest of code remains the same'. You must output full segments.

### YOUR RESPONSE FORMAT:
If you want to use a tool, you MUST output a tool XML block.
If you have finished the task, output a clear wrap-up explanation without any tool blocks.`

			techStackMsg := ""
			if len(projectSettings.TechStack) > 0 {
				techStackMsg = fmt.Sprintf("\n### PROJECT TECH STACK:\nThis project is configured with the following tech stack: %s. Please ensure all code modifications, command choices, and technical recommendations are tailored to this stack.\n", strings.Join(projectSettings.TechStack, ", "))
			}

			systemPrompt := fmt.Sprintf(strings.ReplaceAll(systemPromptRaw, "___", "```"), workspacePath, dirLayout, toolsSpec)
			if techStackMsg != "" {
				systemPrompt = techStackMsg + "\n" + systemPrompt
			}

			// 2. Call the LLM
			reply, err := a.SendChatMessage(modelName, prompt, messages, systemPrompt)
			if err != nil {
				a.emitAgentMessage(tabID, ChatMessage{
					Role:    "assistant",
					Content: fmt.Sprintf("[Error calling model]: %v", err),
				})
				a.emitAgentStatus(tabID, "completed")
				return
			}

			// Emit LLM thoughts/reply to frontend
			a.emitAgentMessage(tabID, ChatMessage{Role: "assistant", Content: reply})
			messages = append(messages, ChatMessage{Role: "assistant", Content: reply})

			// 3. Parse tool calls
			toolCalls := parseToolCalls(reply)
			if len(toolCalls) == 0 {
				if currentPlan == nil && appSettings.EnforcePlanning {
					fallbackPlan := parseFallbackPlan(reply)
					if fallbackPlan != nil {
						toolCalls = []*ParsedTool{
							{
								Name:    "submit_plan",
								Content: fallbackPlan.Description,
								Tasks:   fallbackPlan.Tasks,
							},
						}
					}
				}
			}

			if len(toolCalls) == 0 {
				// No more tool calls, agent is finished!
				a.emitAgentStatus(tabID, "completed")
				return
			}

			var toolOutputs []string
			for _, toolCall := range toolCalls {
				var toolOutput string
				if toolCall.Name == "submit_plan" {
					currentPlan = &AgentPlan{
						Description: toolCall.Content,
						Tasks:       toolCall.Tasks,
					}

					if appSettings.EnforcePlanning {
						// Emit plan update and change status to wait for approval
						a.emitAgentPlan(tabID, currentPlan, "waiting_for_approval")

						ch := make(chan string)
						a.planApprovalsMu.Lock()
						a.planApprovals[tabID] = ch
						a.planApprovalsMu.Unlock()

						// Wait on channel
						approvalResult := <-ch

						a.planApprovalsMu.Lock()
						delete(a.planApprovals, tabID)
						a.planApprovalsMu.Unlock()

						if approvalResult == "approved" {
							toolOutput = "Plan approved by user. You may now start executing your tasks. Remember to update task status using update_task."
							a.emitAgentPlan(tabID, currentPlan, "running")
						} else {
							feedback := strings.TrimPrefix(approvalResult, "rejected:")
							toolOutput = fmt.Sprintf("Plan rejected by user. Feedback: %s. Please revise your plan and submit a new one.", feedback)
							a.emitAgentPlan(tabID, nil, "running")
						}
					} else {
						// Auto approve
						toolOutput = "Plan accepted automatically. You may start executing your tasks."
						a.emitAgentPlan(tabID, currentPlan, "running")
					}
				} else if toolCall.Name == "update_task" {
					if currentPlan != nil {
						found := false
						for i, t := range currentPlan.Tasks {
							if t.ID == toolCall.TaskID {
								currentPlan.Tasks[i].Status = toolCall.TaskStatus
								found = true
								break
							}
						}
						if found {
							toolOutput = fmt.Sprintf("Task '%s' status updated to '%s'.", toolCall.TaskID, toolCall.TaskStatus)
							a.emitAgentPlan(tabID, currentPlan, "running")
						} else {
							toolOutput = fmt.Sprintf("Error: Task ID '%s' not found in current plan.", toolCall.TaskID)
						}
					} else {
						toolOutput = "Error: No active plan found. Please submit a plan first using submit_plan."
					}
				} else if toolCall.Name == "run_command" {
					if isDangerousCommand(toolCall.Cmd) {
						// Emit command approval request to frontend
						a.emitAgentCommandApproval(tabID, toolCall.Cmd, "waiting_for_command_approval")

						ch := make(chan string)
						a.planApprovalsMu.Lock()
						a.planApprovals[tabID] = ch
						a.planApprovalsMu.Unlock()

						// Wait on channel
						approvalResult := <-ch

						a.planApprovalsMu.Lock()
						delete(a.planApprovals, tabID)
						a.planApprovalsMu.Unlock()

						if approvalResult == "approved" {
							a.emitAgentStatus(tabID, "running")
							toolOutput = a.executeTool(tabID, workspacePath, toolCall)
						} else {
							toolOutput = fmt.Sprintf("Error: Command '%s' was denied by the user due to safety policies.", toolCall.Cmd)
							a.emitAgentStatus(tabID, "running")
						}
					} else {
						toolOutput = a.executeTool(tabID, workspacePath, toolCall)
					}
				} else {
					// Execute tool call
					toolOutput = a.executeTool(tabID, workspacePath, toolCall)
				}

				if toolCall.Name == "replace_text" {
					if strings.HasPrefix(toolOutput, "Error:") {
						failedReplaces[toolCall.Path]++
						if failedReplaces[toolCall.Path] >= 4 {
							toolOutput = fmt.Sprintf("Error: Surgical replace_text has failed %d times on '%s'. YOU MUST NOW USE <tool name=\"write_file\"> to overwrite the entire file with your updated code to prevent further matching errors.", failedReplaces[toolCall.Path], toolCall.Path)
						}
					} else {
						failedReplaces[toolCall.Path] = 0
					}
				} else if toolCall.Name == "write_file" {
					failedReplaces[toolCall.Path] = 0
				}

				toolOutputs = append(toolOutputs, toolOutput)
			}

			combinedOutput := strings.Join(toolOutputs, "\n\n")

			// Emit tool result as user prompt for next iteration
			a.emitAgentMessage(tabID, ChatMessage{
				Role:    "user",
				Content: fmt.Sprintf("### TOOL OUTPUT:\n%s", combinedOutput),
			})
			messages = append(messages, ChatMessage{
				Role:    "user",
				Content: fmt.Sprintf("### TOOL OUTPUT:\n%s", combinedOutput),
			})

			// Compress historical messages if enabled
			if appSettings.EnableContextCompression {
				messages = compressHistory(messages)
			}

			// Emit complete updated history to frontend so it matches what LLM sees
			a.emitAgentHistory(tabID, messages)

			// Update the prompt to focus on the tool outcome for the next turn
			prompt = fmt.Sprintf("Continue executing the task. Last tool output: %s", combinedOutput)
		}
	}()
}

type ParsedTool struct {
	Name        string
	Path        string
	Content     string
	Cmd         string
	StartLine   int
	EndLine     int
	Target      string
	Replacement string
	Tasks       []TaskItem
	TaskID      string
	TaskStatus  string
}

func parseToolCall(text string) *ParsedTool {
	calls := parseToolCalls(text)
	if len(calls) > 0 {
		return calls[0]
	}
	return nil
}

func parseToolCalls(text string) []*ParsedTool {
	// Unescape HTML entities just in case
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&amp;", "&")

	var toolCalls []*ParsedTool

	// Find all <tool name="..."> matches
	startRegex := regexp.MustCompile(`(?i)<tool\s+name=["']?([^"'\s>]+)["']?\s*>`)
	startMatches := startRegex.FindAllStringSubmatchIndex(text, -1)
	if len(startMatches) == 0 {
		return nil
	}

	endRegex := regexp.MustCompile(`(?i)</tool\s*>`)
	endMatches := endRegex.FindAllStringIndex(text, -1)

	// Pair opening and closing tags sequentially
	for i, startMatch := range startMatches {
		if i >= len(endMatches) {
			break
		}

		toolName := text[startMatch[2]:startMatch[3]]
		startIdx := startMatch[1] // End of <tool ...> tag
		endIdx := endMatches[i][0] // Start of </tool> tag

		if endIdx <= startIdx {
			continue
		}

		innerContent := text[startIdx:endIdx]

		var tool ParsedTool
		tool.Name = toolName

		// Extract path
		pathRegex := regexp.MustCompile(`(?i)<path>([\s\S]*?)</path>`)
		if pathMatch := pathRegex.FindStringSubmatch(innerContent); len(pathMatch) > 1 {
			tool.Path = strings.TrimSpace(pathMatch[1])
		}

		// Extract content
		contentRegex := regexp.MustCompile(`(?i)<content>([\s\S]*?)</content>`)
		if contentMatch := contentRegex.FindStringSubmatch(innerContent); len(contentMatch) > 1 {
			tool.Content = contentMatch[1]
		}

		// Extract command
		cmdRegex := regexp.MustCompile(`(?i)<command>([\s\S]*?)</command>`)
		if cmdMatch := cmdRegex.FindStringSubmatch(innerContent); len(cmdMatch) > 1 {
			tool.Cmd = strings.TrimSpace(cmdMatch[1])
		}

		// Extract start_line
		startLineRegex := regexp.MustCompile(`(?i)<start_line>(\d+)</start_line>`)
		if startLineMatch := startLineRegex.FindStringSubmatch(innerContent); len(startLineMatch) > 1 {
			if val, err := strconv.Atoi(strings.TrimSpace(startLineMatch[1])); err == nil {
				tool.StartLine = val
			}
		}

		// Extract end_line
		endLineRegex := regexp.MustCompile(`(?i)<end_line>(\d+)</end_line>`)
		if endLineMatch := endLineRegex.FindStringSubmatch(innerContent); len(endLineMatch) > 1 {
			if val, err := strconv.Atoi(strings.TrimSpace(endLineMatch[1])); err == nil {
				tool.EndLine = val
			}
		}

		// Extract target
		targetRegex := regexp.MustCompile(`(?i)<target>([\s\S]*?)</target>`)
		if targetMatch := targetRegex.FindStringSubmatch(innerContent); len(targetMatch) > 1 {
			tool.Target = targetMatch[1]
		}

		// Extract replacement
		repRegex := regexp.MustCompile(`(?i)<replacement>([\s\S]*?)</replacement>`)
		if repMatch := repRegex.FindStringSubmatch(innerContent); len(repMatch) > 1 {
			tool.Replacement = repMatch[1]
		}

		// Extract task id
		idRegex := regexp.MustCompile(`(?i)<id>([\s\S]*?)</id>`)
		if idMatch := idRegex.FindStringSubmatch(innerContent); len(idMatch) > 1 {
			tool.TaskID = strings.TrimSpace(idMatch[1])
		}

		// Extract task status
		statusRegex := regexp.MustCompile(`(?i)<status>([\s\S]*?)</status>`)
		if statusMatch := statusRegex.FindStringSubmatch(innerContent); len(statusMatch) > 1 {
			tool.TaskStatus = strings.TrimSpace(statusMatch[1])
		}

		// Extract fallback task list
		taskRegex := regexp.MustCompile(`(?i)<task\s+id=["']?([^"'\s>]+)["']?\s*>([\s\S]*?)</task>`)
		taskMatches := taskRegex.FindAllStringSubmatch(innerContent, -1)
		for _, tm := range taskMatches {
			if len(tm) > 2 {
				tool.Tasks = append(tool.Tasks, TaskItem{
					ID:          strings.TrimSpace(tm[1]),
					Description: strings.TrimSpace(tm[2]),
					Status:      "pending",
				})
			}
		}

		toolCalls = append(toolCalls, &tool)
	}

	return toolCalls
}



func serializeTree(node *FileNode, indent string, depth int) string {
	if node == nil || depth > 4 {
		return ""
	}
	var sb strings.Builder
	if node.Path != "" {
		sb.WriteString(fmt.Sprintf("%s- %s\n", indent, node.Name))
	}
	if node.IsDir && node.Children != nil {
		for _, child := range node.Children {
			sb.WriteString(serializeTree(child, indent+"  ", depth+1))
		}
	}
	return sb.String()
}

func compressHistory(messages []ChatMessage) []ChatMessage {
	if len(messages) <= 4 {
		return messages
	}

	pruned := make([]ChatMessage, len(messages))
	copy(pruned, messages)

	for i := 0; i < len(pruned)-3; i++ {
		msg := pruned[i]
		if msg.Role == "user" && strings.HasPrefix(msg.Content, "### TOOL OUTPUT:\n") {
			lines := strings.Split(msg.Content, "\n")
			toolName := "tool output"
			if len(lines) > 1 {
				toolName = strings.TrimSpace(lines[1])
				toolName = strings.TrimPrefix(toolName, "[")
				toolName = strings.TrimSuffix(toolName, "]")
			}
			pruned[i].Content = fmt.Sprintf("### TOOL OUTPUT:\n%s (content compressed to save context space)", toolName)
		}
	}
	return pruned
}

func (a *App) emitAgentMessage(tabID string, message ChatMessage) {
	runtime.EventsEmit(a.ctx, "agent:message", AgentEvent{
		TabID:   tabID,
		Type:    "message",
		Message: message,
	})
}

func (a *App) emitAgentHistory(tabID string, messages []ChatMessage) {
	runtime.EventsEmit(a.ctx, "agent:history_update", AgentEvent{
		TabID:    tabID,
		Type:     "history_update",
		Messages: messages,
	})
}

func (a *App) emitAgentStatus(tabID string, status string) {
	runtime.EventsEmit(a.ctx, "agent:status", AgentEvent{
		TabID:  tabID,
		Type:   "status",
		Status: status,
	})
}

// StopAgent terminates a running agent on a specific tab.
func (a *App) StopAgent(tabID string) {
	a.cancelsMu.Lock()
	defer a.cancelsMu.Unlock()
	if cancel, exists := a.agentCancels[tabID]; exists {
		cancel()
		delete(a.agentCancels, tabID)
	}
	a.emitAgentStatus(tabID, "completed")
}

func (a *App) GetActiveFiles(messages []ChatMessage) []string {
	activeMap := make(map[string]bool)
	rePath := regexp.MustCompile(`(?s)<path>(.*?)</path>`)
	for _, msg := range messages {
		matches := rePath.FindAllStringSubmatch(msg.Content, -1)
		for _, m := range matches {
			if len(m) > 1 {
				p := strings.TrimSpace(m[1])
				if p != "" {
					activeMap[p] = true
				}
			}
		}
	}
	var list []string
	for p := range activeMap {
		list = append(list, p)
	}
	return list
}

func (a *App) emitAgentPlan(tabID string, plan *AgentPlan, status string) {
	runtime.EventsEmit(a.ctx, "agent:plan", AgentEvent{
		TabID:  tabID,
		Type:   "plan",
		Status: status,
		Plan:   plan,
	})
}

func (a *App) ApprovePlan(tabID string) {
	a.planApprovalsMu.Lock()
	ch, exists := a.planApprovals[tabID]
	a.planApprovalsMu.Unlock()
	if exists {
		ch <- "approved"
	}
}

func (a *App) RejectPlan(tabID string, feedback string) {
	a.planApprovalsMu.Lock()
	ch, exists := a.planApprovals[tabID]
	a.planApprovalsMu.Unlock()
	if exists {
		ch <- "rejected:" + feedback
	}
}

func parseFallbackPlan(text string) *AgentPlan {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var tasks []TaskItem
	var descLines []string

	tableMode := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			parts := strings.Split(trimmed, "|")
			if len(parts) >= 4 {
				col1 := strings.TrimSpace(parts[1])
				col2 := strings.TrimSpace(parts[2])

				if strings.ToLower(col1) == "task" || strings.Contains(col1, "---") {
					continue
				}

				if col1 != "" && col2 != "" {
					col1 = strings.Trim(col1, "* ")
					tasks = append(tasks, TaskItem{
						ID:          fmt.Sprintf("task%d", len(tasks)+1),
						Description: fmt.Sprintf("%s: %s", col1, col2),
						Status:      "pending",
					})
				}
			}
			tableMode = true
		} else if !tableMode {
			if trimmed != "" {
				descLines = append(descLines, trimmed)
			}
		}
	}

	if len(tasks) == 0 {
		descLines = nil
		listMode := false
		listRegex := regexp.MustCompile(`^\s*(\d+)\.\s*([^:-]+)(?:[: -]+)?(.*)`)
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if match := listRegex.FindStringSubmatch(trimmed); match != nil {
				title := strings.TrimSpace(match[2])
				body := strings.TrimSpace(match[3])
				desc := title
				if body != "" {
					desc = fmt.Sprintf("%s: %s", title, body)
				}
				tasks = append(tasks, TaskItem{
					ID:          fmt.Sprintf("task%d", len(tasks)+1),
					Description: desc,
					Status:      "pending",
				})
				listMode = true
			} else if !listMode {
				if trimmed != "" {
					descLines = append(descLines, trimmed)
				}
			}
		}
	}

	if len(tasks) > 0 {
		desc := "Fallback plan extracted from assistant reply."
		if len(descLines) > 0 {
			desc = strings.Join(descLines, "\n")
			if len(desc) > 300 {
				desc = desc[:297] + "..."
			}
		}
		return &AgentPlan{
			Description: desc,
			Tasks:       tasks,
		}
	}

	return nil
}

func isDangerousCommand(cmd string) bool {
	cmdLower := strings.ToLower(cmd)
	dangerousTerms := []string{
		"rm ", "rmdir", "del ", "erase", "rd ", "git clean", "git reset --hard", "format",
	}
	for _, term := range dangerousTerms {
		if strings.Contains(cmdLower, term) {
			return true
		}
	}
	return false
}

func (a *App) emitAgentCommandApproval(tabID string, command string, status string) {
	runtime.EventsEmit(a.ctx, "agent:command_approval", AgentEvent{
		TabID:   tabID,
		Type:    "command_approval",
		Status:  status,
		Command: command,
	})
}

func (a *App) requestDiffApproval(tabID string, filePath string, relPath string, original string, proposed string) error {
	appSettings, err := a.LoadSettings()
	if err != nil || !appSettings.EnableDiffViewer {
		// YOLO mode: Write directly
		return os.WriteFile(filePath, []byte(proposed), 0644)
	}

	ch := make(chan string)
	a.diffApprovalsMu.Lock()
	a.diffApprovals[tabID] = ch
	a.diffApprovalsMu.Unlock()

	a.pendingDiffsMu.Lock()
	a.pendingDiffs[tabID] = &DiffProposal{
		FilePath:        filepath.ToSlash(relPath),
		OriginalContent: original,
		ProposedContent: proposed,
	}
	a.pendingDiffsMu.Unlock()

	defer func() {
		a.diffApprovalsMu.Lock()
		delete(a.diffApprovals, tabID)
		a.diffApprovalsMu.Unlock()

		a.pendingDiffsMu.Lock()
		delete(a.pendingDiffs, tabID)
		a.pendingDiffsMu.Unlock()
	}()

	// Emit event to frontend
	runtime.EventsEmit(a.ctx, "agent:diff_proposal", AgentEvent{
		TabID:    tabID,
		Type:     "diff_proposal",
		Status:   "waiting_for_diff_approval",
		FilePath: filepath.ToSlash(relPath),
	})
	a.emitAgentStatus(tabID, "waiting_for_diff_approval")

	// Block until approved or rejected
	action := <-ch
	if action == "approve" {
		err = os.WriteFile(filePath, []byte(proposed), 0644)
		if err != nil {
			return fmt.Errorf("failed to save approved edits: %w", err)
		}
		a.emitAgentStatus(tabID, "running")
		return nil
	}

	// Rejected
	a.emitAgentStatus(tabID, "running")
	return fmt.Errorf("user rejected proposed changes to file: %s", relPath)
}
