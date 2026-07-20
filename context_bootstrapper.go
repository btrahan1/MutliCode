package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// BootstrapContext scans the prompt and the workspace to gather highly relevant files.
// It returns a formatted string containing the contents of these files to serve as "immediate context".
func (a *App) BootstrapContext(workspacePath string, prompt string) string {
	if workspacePath == "" || prompt == "" {
		return ""
	}

	// 1. Gather blueprint files (e.g. wails.json, go.mod, package.json, etc.)
	blueprints := a.findBlueprintFiles(workspacePath)

	// 2. Extract potential keywords/filenames from prompt
	mentioned := a.findMentionedFiles(workspacePath, prompt)

	// Combine files, avoiding duplicates
	seen := make(map[string]bool)
	var filesToRead []string

	// Prioritize mentioned files first
	for _, f := range mentioned {
		rel, err := filepath.Rel(workspacePath, f)
		if err == nil && !seen[rel] {
			seen[rel] = true
			filesToRead = append(filesToRead, rel)
		}
	}

	// Add blueprint files up to a limit
	for _, f := range blueprints {
		rel, err := filepath.Rel(workspacePath, f)
		if err == nil && !seen[rel] {
			seen[rel] = true
			filesToRead = append(filesToRead, rel)
		}
	}

	if len(filesToRead) == 0 {
		return ""
	}

	// Read and format files (limit total size/tokens injected)
	var sb strings.Builder
	sb.WriteString("\n### AUTOMATICALLY RETRIEVED CONTEXT:\n")
	sb.WriteString("To help you start with immediate situational awareness, the following files were automatically loaded from the workspace:\n\n")

	maxFileLimit := 4
	maxSizeLimit := 15 * 1024 // 15KB per file
	loadedCount := 0

	for _, relPath := range filesToRead {
		if loadedCount >= maxFileLimit {
			break
		}

		fullPath := filepath.Join(workspacePath, relPath)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			continue
		}

		if info.Size() > int64(maxSizeLimit) {
			continue // skip overly large files
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		sb.WriteString(fmt.Sprintf("--- File: %s ---\n", filepath.ToSlash(relPath)))
		sb.WriteString(string(content))
		sb.WriteString("\n\n")
		loadedCount++
	}

	if loadedCount == 0 {
		return ""
	}

	return sb.String()
}

// findBlueprintFiles looks for project configurations in the root directory.
func (a *App) findBlueprintFiles(workspacePath string) []string {
	var blueprints []string
	candidates := []string{
		"wails.json",
		"go.mod",
		"package.json",
		"main.go",
		"app.go",
	}

	for _, name := range candidates {
		fullPath := filepath.Join(workspacePath, name)
		if _, err := os.Stat(fullPath); err == nil {
			blueprints = append(blueprints, fullPath)
		}
	}
	return blueprints
}

// findMentionedFiles extracts words/paths from the prompt and searches for matching filenames in the workspace.
func (a *App) findMentionedFiles(workspacePath string, prompt string) []string {
	// Simple word extractor
	wordRegex := regexp.MustCompile(`[a-zA-Z0-9_\-\./]+`)
	words := wordRegex.FindAllString(prompt, -1)
	if len(words) == 0 {
		return nil
	}

	// Filter and clean candidate words
	candidateNames := make(map[string]bool)
	for _, w := range words {
		// Clean punctuation
		wClean := strings.Trim(w, ".,()[]{}'\"`?!")
		if len(wClean) < 3 {
			continue
		}
		candidateNames[strings.ToLower(wClean)] = true
	}

	var matchedFiles []string
	// Limit traversal depth to stay fast
	filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
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
			filenameLower := strings.ToLower(info.Name())
			relPath, err := filepath.Rel(workspacePath, path)
			if err != nil {
				return nil
			}
			relPathLower := strings.ToLower(filepath.ToSlash(relPath))

			// Check if any candidate keyword matches the filename or relative path
			for term := range candidateNames {
				if filenameLower == term || relPathLower == term || strings.HasSuffix(filenameLower, "."+term) {
					matchedFiles = append(matchedFiles, path)
					break
				}
			}
		}
		return nil
	})

	return matchedFiles
}
