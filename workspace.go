package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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


