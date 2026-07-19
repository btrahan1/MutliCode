package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goRuntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SelectWorkspace opens a directory selector dialog.
func (a *App) SelectWorkspace() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Workspace Folder",
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Children []*FileNode `json:"children"`
}

// Simple ignore checker for typical workspace noise.
func isIgnored(name string) bool {
	if name == "." || name == ".." {
		return false
	}
	ignoredNames := map[string]bool{
		".git":              true,
		"node_modules":      true,
		".wails":            true,
		"dist":              true,
		"build":             true,
		".vs":               true,
		"bin":               true,
		"obj":               true,
		".next":             true,
		"out":               true,
		"coverage":          true,
		".cache":            true,
		"yarn.lock":         true,
		"package-lock.json": true,
	}
	return ignoredNames[name] || strings.HasPrefix(name, ".")
}

func (a *App) IsPathIgnored(workspacePath, path string) bool {
	rel, err := filepath.Rel(workspacePath, path)
	if err != nil {
		return true
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	for _, part := range parts {
		if isIgnored(part) {
			return true
		}
	}
	return false
}

func (a *App) GetDirectoryTree(workspacePath string) (*FileNode, error) {
	if workspacePath == "" {
		return nil, fmt.Errorf("workspace path is empty")
	}

	_, err := os.Stat(workspacePath)
	if err != nil {
		return nil, err
	}

	rootNode := &FileNode{
		Name:     filepath.Base(workspacePath),
		Path:     "",
		IsDir:    true,
		Children: []*FileNode{},
	}

	var buildTree func(dir string, parentNode *FileNode) error
	buildTree = func(dir string, parentNode *FileNode) error {
		files, err := os.ReadDir(dir)
		if err != nil {
			return err
		}

		for _, file := range files {
			name := file.Name()
			if isIgnored(name) {
				continue
			}

			fullPath := filepath.Join(dir, name)
			relPath, err := filepath.Rel(workspacePath, fullPath)
			if err != nil {
				continue
			}

			node := &FileNode{
				Name:  name,
				Path:  filepath.ToSlash(relPath),
				IsDir: file.IsDir(),
			}

			if file.IsDir() {
				node.Children = []*FileNode{}
				if err := buildTree(fullPath, node); err != nil {
					return err
				}
			}

			parentNode.Children = append(parentNode.Children, node)
		}
		return nil
	}

	if err := buildTree(workspacePath, rootNode); err != nil {
		return nil, err
	}

	return rootNode, nil
}

func (a *App) GetFileContent(workspacePath string, relPath string) (string, error) {
	fullPath := filepath.Join(workspacePath, relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) SaveFileContent(workspacePath string, relPath string, content string) error {
	fullPath := filepath.Join(workspacePath, relPath)
	return os.WriteFile(fullPath, []byte(content), 0644)
}

func (a *App) OpenPathInExplorer(workspacePath, relPath string) error {
	fullPath := filepath.Clean(filepath.Join(workspacePath, relPath))
	fmt.Printf("[DEBUG] OpenPathInExplorer - workspacePath: %q, relPath: %q, fullPath: %q\n", workspacePath, relPath, fullPath)

	info, err := os.Stat(fullPath)
	if err != nil {
		fmt.Printf("[DEBUG] OpenPathInExplorer os.Stat error: %v\n", err)
		return err
	}

	var cmd *exec.Cmd
	if info.IsDir() {
		cmd = exec.Command("explorer", fullPath)
	} else {
		cmd = exec.Command("explorer", "/select,"+fullPath)
	}

	fmt.Printf("[DEBUG] OpenPathInExplorer running: %s %v\n", cmd.Path, cmd.Args)
	err = cmd.Start()
	if err != nil {
		fmt.Printf("[DEBUG] OpenPathInExplorer cmd.Start() error: %v\n", err)
		return err
	}
	return nil
}

func (a *App) CreateFile(workspacePath string, relPath string) error {
	fullPath := filepath.Join(workspacePath, relPath)
	// Ensure directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	return f.Close()
}

func (a *App) CreateDirectory(workspacePath string, relPath string) error {
	fullPath := filepath.Join(workspacePath, relPath)
	return os.MkdirAll(fullPath, 0755)
}

func (a *App) DeletePath(workspacePath string, relPath string) error {
	fullPath := filepath.Join(workspacePath, relPath)
	return os.RemoveAll(fullPath)
}

func (a *App) RenamePath(workspacePath string, oldRelPath string, newRelPath string) error {
	oldPath := filepath.Join(workspacePath, oldRelPath)
	newPath := filepath.Join(workspacePath, newRelPath)
	// Ensure parent of target exists
	dir := filepath.Dir(newPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

type ProjectSettings struct {
	TechStack []string `json:"techStack"`
}

func (a *App) GetProjectSettings(projectPath string) (ProjectSettings, error) {
	var settings ProjectSettings
	settings.TechStack = []string{}
	filePath := filepath.Join(projectPath, ".multicode.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return settings, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return settings, err
	}
	err = json.Unmarshal(data, &settings)
	return settings, err
}

func (a *App) SaveProjectSettings(projectPath string, settings ProjectSettings) error {
	filePath := filepath.Join(projectPath, ".multicode.json")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func (a *App) CreateNewProject(parentDir string, projectName string, techStack []string) (string, error) {
	if parentDir == "" {
		return "", fmt.Errorf("parent directory cannot be empty")
	}
	if projectName == "" {
		return "", fmt.Errorf("project name cannot be empty")
	}

	projectPath := filepath.Join(parentDir, projectName)
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return "", err
	}

	settings := ProjectSettings{
		TechStack: techStack,
	}

	if err := a.SaveProjectSettings(projectPath, settings); err != nil {
		return "", err
	}

	return projectPath, nil
}

func (a *App) RunProject(tabID string, projectPath string) error {
	a.projectCmdsMu.Lock()
	existingCmd, exists := a.projectCmds[tabID]
	a.projectCmdsMu.Unlock()

	if exists && existingCmd != nil {
		_ = a.StopProject(tabID)
		time.Sleep(500 * time.Millisecond) // Give OS a moment to release ports
	}

	name, args := detectRunCommand(projectPath)
	if name == "" {
		return fmt.Errorf("could not detect project type or run command for path: %s", projectPath)
	}

	runtime.EventsEmit(a.ctx, "project:status", map[string]string{
		"tabId":  tabID,
		"status": "starting",
	})

	cmd := createCommand(projectPath, name, args)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		runtime.EventsEmit(a.ctx, "project:status", map[string]string{
			"tabId":  tabID,
			"status": "error",
		})
		return err
	}

	a.projectCmdsMu.Lock()
	a.projectCmds[tabID] = cmd
	a.projectCmdsMu.Unlock()

	// Regex to extract URL
	urlRegex := regexp.MustCompile(`https?://(localhost|127\.0\.0\.1|0\.0\.0\.0):[0-9]+`)

	scanLogs := func(reader io.ReadCloser) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Printf("[%s Output]: %s\n", tabID, line)
			match := urlRegex.FindString(line)
			if match != "" {
				// Normalize 0.0.0.0 to localhost for browser-open ease
				match = strings.Replace(match, "0.0.0.0", "localhost", 1)
				runtime.EventsEmit(a.ctx, "project:url", map[string]string{
					"tabId": tabID,
					"url":   match,
				})
			}
		}
	}

	go scanLogs(stdout)
	go scanLogs(stderr)

	// Wait for process exit
	go func() {
		_ = cmd.Wait()

		a.projectCmdsMu.Lock()
		delete(a.projectCmds, tabID)
		a.projectCmdsMu.Unlock()

		runtime.EventsEmit(a.ctx, "project:status", map[string]string{
			"tabId":  tabID,
			"status": "idle",
		})
	}()

	return nil
}

func (a *App) StopProject(tabID string) error {
	a.projectCmdsMu.Lock()
	cmd, exists := a.projectCmds[tabID]
	a.projectCmdsMu.Unlock()

	if !exists || cmd == nil {
		return nil
	}

	if goRuntime.GOOS == "windows" {
		killCmd := exec.Command("taskkill", "/t", "/f", "/pid", fmt.Sprintf("%d", cmd.Process.Pid))
		_ = killCmd.Run()
	} else {
		_ = cmd.Process.Kill()
	}

	a.projectCmdsMu.Lock()
	delete(a.projectCmds, tabID)
	a.projectCmdsMu.Unlock()

	runtime.EventsEmit(a.ctx, "project:status", map[string]string{
		"tabId":  tabID,
		"status": "idle",
	})

	return nil
}

func (a *App) OpenBrowserURL(url string) {
	runtime.BrowserOpenURL(a.ctx, url)
}

func detectRunCommand(projectPath string) (string, []string) {
	if _, err := os.Stat(filepath.Join(projectPath, "wails.json")); err == nil {
		return "wails", []string{"dev"}
	}

	if _, err := os.Stat(filepath.Join(projectPath, "package.json")); err == nil {
		pData, err := os.ReadFile(filepath.Join(projectPath, "package.json"))
		if err == nil {
			var pkg struct {
				Scripts map[string]string `json:"scripts"`
			}
			if json.Unmarshal(pData, &pkg) == nil {
				if _, ok := pkg.Scripts["dev"]; ok {
					return "npm", []string{"run", "dev"}
				}
				if _, ok := pkg.Scripts["start"]; ok {
					return "npm", []string{"start"}
				}
			}
		}
		return "npm", []string{"run", "dev"}
	}

	// Try in root or recursively (e.g. Server folders)
	matches, _ := filepath.Glob(filepath.Join(projectPath, "*.csproj"))
	if len(matches) > 0 {
		return "dotnet", []string{"run"}
	}

	var serverCsproj string
	_ = filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".csproj") {
			if strings.Contains(strings.ToLower(info.Name()), "server") {
				serverCsproj = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	if serverCsproj != "" {
		rel, err := filepath.Rel(projectPath, serverCsproj)
		if err == nil {
			return "dotnet", []string{"run", "--project", rel}
		}
	}

	if _, err := os.Stat(filepath.Join(projectPath, "main.go")); err == nil {
		return "go", []string{"run", "."}
	}

	if _, err := os.Stat(filepath.Join(projectPath, "index.html")); err == nil {
		return "npx", []string{"serve", "."}
	}

	return "", nil
}

func createCommand(dir string, name string, args []string) *exec.Cmd {
	var cmd *exec.Cmd
	if goRuntime.GOOS == "windows" {
		fullArgs := append([]string{"/c", name}, args...)
		cmd = exec.Command("cmd.exe", fullArgs...)
	} else {
		cmd = exec.Command(name, args...)
	}
	cmd.Dir = dir
	return cmd
}

func (a *App) GetProjectSourceString(projectPath string) (string, error) {
	if projectPath == "" {
		return "", fmt.Errorf("project path is empty")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Project Source: %s\n\n", filepath.Base(projectPath)))

	allowedExtensions := map[string]bool{
		".go": true, ".cs": true, ".ts": true, ".tsx": true,
		".js": true, ".jsx": true, ".html": true, ".css": true,
		".json": true, ".md": true, ".cpp": true, ".hpp": true,
		".c": true, ".h": true, ".py": true, ".rs": true,
		".yaml": true, ".yml": true, ".xml": true, ".sh": true,
		".bat": true, ".sql": true, ".csproj": true, ".sln": true,
		".config": true, ".gitignore": true, ".razor": true,
	}

	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if a.IsPathIgnored(projectPath, path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		baseName := strings.ToLower(info.Name())
		isSpecialText := baseName == "dockerfile" || baseName == "makefile" || baseName == "go.mod" || baseName == "go.sum" || baseName == ".gitignore"

		if allowedExtensions[ext] || isSpecialText {
			if info.Size() > 500*1024 {
				return nil
			}

			rel, err := filepath.Rel(projectPath, path)
			if err != nil {
				return nil
			}

			contentBytes, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			cleanExt := strings.TrimPrefix(ext, ".")
			if cleanExt == "" {
				cleanExt = "text"
			}
			if cleanExt == "razor" {
				cleanExt = "html"
			}

			sb.WriteString(fmt.Sprintf("# File: %s\n", filepath.ToSlash(rel)))
			sb.WriteString(fmt.Sprintf("```%s\n", cleanExt))
			sb.Write(contentBytes)
			sb.WriteString("\n```\n\n")
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return sb.String(), nil
}

type GraphNode struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Score    float64 `json:"score"`
	Language string  `json:"language"`
}

type GraphLink struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
}

type RepoGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Links []GraphLink `json:"links"`
}

func (a *App) GetRepoGraph(projectPath string) (*RepoGraph, error) {
	if projectPath == "" {
		return nil, fmt.Errorf("project path is empty")
	}

	allDefs := make(map[string][]Tag)
	symbolToDefFile := make(map[string]string)
	allRefs := make(map[string][]string)

	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if a.IsPathIgnored(projectPath, path) {
			return nil
		}

		relPath, err := filepath.Rel(projectPath, path)
		if err != nil {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		var defs []Tag
		var refs []string

		if ext == ".go" {
			defs, refs, _ = ParseGoFile(path, relPath)
		} else if ext == ".py" || ext == ".js" || ext == ".ts" || ext == ".tsx" || ext == ".cs" || ext == ".cpp" || ext == ".hpp" || ext == ".c" || ext == ".h" {
			defs, refs, _ = ParseRegexFile(path, relPath)
		} else {
			return nil
		}

		allDefs[relPath] = defs
		allRefs[relPath] = refs
		for _, def := range defs {
			symbolToDefFile[def.SymbolName] = relPath
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	nodes := make(map[string]bool)
	edges := make(map[string]map[string]float64)

	for file := range allDefs {
		nodes[file] = true
	}

	for file, refs := range allRefs {
		for _, ref := range refs {
			if targetFile, exists := symbolToDefFile[ref]; exists && targetFile != file {
				if _, ok := edges[file]; !ok {
					edges[file] = make(map[string]float64)
				}
				edges[file][targetFile] += 1.0
			}
		}
	}

	damping := 0.85
	maxIterations := 20
	ranks := computePageRank(nodes, edges, nil, damping, maxIterations)

	var graphNodes []GraphNode
	for file := range nodes {
		score := ranks[file]
		if score == 0 {
			score = 0.15
		}
		graphNodes = append(graphNodes, GraphNode{
			ID:       filepath.ToSlash(file),
			Name:     filepath.Base(file),
			Score:    score,
			Language: detectLanguage(file),
		})
	}

	var graphLinks []GraphLink
	for source, toMap := range edges {
		for target, weight := range toMap {
			graphLinks = append(graphLinks, GraphLink{
				Source: filepath.ToSlash(source),
				Target: filepath.ToSlash(target),
				Weight: weight,
			})
		}
	}

	return &RepoGraph{
		Nodes: graphNodes,
		Links: graphLinks,
	}, nil
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "Go"
	case ".cs":
		return "C#"
	case ".ts":
		return "TypeScript"
	case ".tsx":
		return "TypeScript React"
	case ".js", ".jsx":
		return "JavaScript"
	case ".cpp", ".hpp", ".cc", ".cxx":
		return "C++"
	case ".c", ".h":
		return "C"
	case ".py":
		return "Python"
	case ".rs":
		return "Rust"
	case ".html", ".htm", ".razor":
		return "HTML"
	case ".css":
		return "CSS"
	case ".json":
		return "JSON"
	case ".md":
		return "Markdown"
	default:
		return "Other"
	}
}


