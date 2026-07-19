package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func (a *App) executeTool(tabID string, workspacePath string, tool *ParsedTool) string {
	switch tool.Name {
	case "read_file":
		content, err := a.GetFileContent(workspacePath, tool.Path)
		if err != nil {
			return fmt.Sprintf("Error reading file: %v", err)
		}

		lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
		start := 1
		if tool.StartLine > 0 {
			start = tool.StartLine
		}
		end := len(lines)
		if tool.EndLine > 0 {
			end = tool.EndLine
		}

		if start < 1 {
			start = 1
		}
		if start > len(lines) {
			start = len(lines)
		}
		if end < start {
			end = start
		}
		if end > len(lines) {
			end = len(lines)
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("[FILE CONTENT OF %s (Lines %d-%d of %d)]:\n", tool.Path, start, end, len(lines)))
		for i := start; i <= end; i++ {
			sb.WriteString(fmt.Sprintf("%d: %s\n", i, lines[i-1]))
		}
		return sb.String()

	case "search_code":
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("[SEARCH RESULTS FOR '%s' in workspace]:\n", tool.Cmd))
		matchCount := 0

		err := filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if a.IsPathIgnored(workspacePath, path) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			if !info.IsDir() {
				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				
				contentStr := string(data)
				if !strings.Contains(strings.ToLower(contentStr), strings.ToLower(tool.Cmd)) {
					return nil
				}

				lines := strings.Split(strings.ReplaceAll(contentStr, "\r\n", "\n"), "\n")
				for i, line := range lines {
					if strings.Contains(strings.ToLower(line), strings.ToLower(tool.Cmd)) {
						matchCount++
						rel, _ := filepath.Rel(workspacePath, path)
						sb.WriteString(fmt.Sprintf("%s:%d: %s\n", rel, i+1, strings.TrimSpace(line)))
					}
				}
			}
			return nil
		})

		if err != nil {
			return fmt.Sprintf("Error walking workspace: %v", err)
		}
		if matchCount == 0 {
			return "No matches found."
		}
		return sb.String()

	case "replace_text":
		filePath := filepath.Join(workspacePath, tool.Path)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Sprintf("Error reading file: %v", err)
		}

		originalContent := string(data)
		lines := strings.Split(strings.ReplaceAll(originalContent, "\r\n", "\n"), "\n")

		searchArea := originalContent
		searchAreaLength := len(originalContent)
		startCharIdx := 0
		isScoped := tool.StartLine > 0 && tool.EndLine > 0

		if isScoped {
			startIdx := tool.StartLine - 1
			if startIdx < 0 {
				startIdx = 0
			}
			if startIdx >= len(lines) {
				startIdx = len(lines) - 1
			}
			endIdx := tool.EndLine - 1
			if endIdx < startIdx {
				endIdx = startIdx
			}
			if endIdx >= len(lines) {
				endIdx = len(lines) - 1
			}

			var sbSearch strings.Builder
			for i := startIdx; i <= endIdx; i++ {
				sbSearch.WriteString(lines[i] + "\n")
			}
			searchArea = sbSearch.String()
			searchAreaLength = len(searchArea)

			for i := 0; i < startIdx; i++ {
				startCharIdx += len(lines[i]) + 1
			}
		}

		targetStr := strings.ReplaceAll(tool.Target, "\r\n", "\n")
		repStr := strings.ReplaceAll(tool.Replacement, "\r\n", "\n")

		matchIdx := strings.Index(searchArea, targetStr)
		if matchIdx == -1 {
			return "Error: Target text not found in search area."
		}

		lastMatchIdx := strings.LastIndex(searchArea, targetStr)
		if matchIdx != lastMatchIdx {
			return "Error: Target text matches multiple locations in file/scope. Please narrow down range or target content."
		}

		newSearchArea := searchArea[:matchIdx] + repStr + searchArea[matchIdx+len(targetStr):]

		var finalContent string
		if isScoped {
			if startCharIdx > len(originalContent) {
				startCharIdx = len(originalContent)
			}
			endOffset := startCharIdx + searchAreaLength
			if endOffset > len(originalContent) {
				endOffset = len(originalContent)
			}
			finalContent = originalContent[:startCharIdx] + newSearchArea + originalContent[endOffset:]
		} else {
			finalContent = newSearchArea
		}

		err = a.requestDiffApproval(tabID, filePath, tool.Path, originalContent, finalContent)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		
		return fmt.Sprintf("Success: Replaced target text in '%s' successfully.", tool.Path)

	case "write_file":
		filePath := filepath.Join(workspacePath, tool.Path)
		originalContent := ""
		if _, statErr := os.Stat(filePath); statErr == nil {
			if data, readErr := os.ReadFile(filePath); readErr == nil {
				originalContent = string(data)
			}
		}

		err := a.requestDiffApproval(tabID, filePath, tool.Path, originalContent, tool.Content)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return fmt.Sprintf("Successfully wrote file: %s", tool.Path)

	case "run_command":
		var cmd *exec.Cmd
		if os.Getenv("OS") == "Windows_NT" {
			cmd = exec.Command("cmd", "/c", tool.Cmd)
			cmd.SysProcAttr = &syscall.SysProcAttr{
				HideWindow: true,
			}
		} else {
			cmd = exec.Command("sh", "-c", tool.Cmd)
		}
		cmd.Dir = workspacePath
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("Command failed with error: %v\nOutput:\n%s", err, string(output))
		}
		return fmt.Sprintf("Command output:\n%s", string(output))

	default:
		// Check MCP tools
		a.mcpClientsMu.Lock()
		var targetClient *McpClient
		for _, client := range a.mcpClients {
			if !client.IsReady {
				continue
			}
			for _, t := range client.Tools {
				if t.Name == tool.Name {
					targetClient = client
					break
				}
			}
			if targetClient != nil {
				break
			}
		}
		a.mcpClientsMu.Unlock()

		if targetClient != nil {
			var arguments map[string]interface{}
			if err := json.Unmarshal([]byte(tool.Content), &arguments); err != nil {
				trimmed := strings.TrimSpace(tool.Content)
				if err = json.Unmarshal([]byte(trimmed), &arguments); err != nil {
					return fmt.Sprintf("Error: Failed to parse tool arguments JSON: %v. Raw content: %s", err, tool.Content)
				}
			}

			output, err := targetClient.CallTool(tool.Name, arguments)
			if err != nil {
				return fmt.Sprintf("Error executing MCP tool '%s': %v", tool.Name, err)
			}
			return output
		}

		return fmt.Sprintf("Unknown tool: %s", tool.Name)
	}
}
