package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var settingsMu sync.Mutex

type AppSettings struct {
	OpenWorkspaces   []string                 `json:"openWorkspaces"`
	ActiveWorkspace  string                   `json:"activeWorkspace"`
	GeminiApiKey     string                   `json:"geminiApiKey"`
	OpenCodeApiKey   string                   `json:"openCodeApiKey"`
	OpenRouterApiKey string                   `json:"openRouterApiKey"`
	OllamaEndpoint   string                   `json:"ollamaEndpoint"`
	WorkspaceModels  map[string]string        `json:"workspaceModels"`
	SidebarWidth     int                      `json:"sidebarWidth"`
	ChatWidth        int                      `json:"chatWidth"`
	WorkspaceHistory          map[string][]ChatMessage `json:"workspaceHistory"`
	Theme                     string                   `json:"theme"`
	EnableSearchCode          bool                     `json:"enableSearchCode"`
	EnableContextCompression  bool                     `json:"enableContextCompression"`
	UseRepoMap                bool                     `json:"useRepoMap"`
	RepoMapTokens             int                      `json:"repoMapTokens"`
}

func getSettingsPath() (string, error) {
	appData, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(appData, "MultiCode")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func importFreeCodeSettings(settings *AppSettings) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return
	}
	freecodePath := filepath.Join(localAppData, "FreeCode", "settings.json")
	if _, err := os.Stat(freecodePath); err != nil {
		return
	}

	fileBytes, err := os.ReadFile(freecodePath)
	if err != nil {
		return
	}

	var fcSettings struct {
		GeminiApiKey     string `json:"GeminiApiKey"`
		OpenCodeApiKey   string `json:"OpenCodeApiKey"`
		OpenRouterApiKey string `json:"OpenRouterApiKey"`
	}

	if err := json.Unmarshal(fileBytes, &fcSettings); err != nil {
		return
	}

	if fcSettings.GeminiApiKey != "" {
		settings.GeminiApiKey = fcSettings.GeminiApiKey
	}
	if fcSettings.OpenCodeApiKey != "" {
		settings.OpenCodeApiKey = fcSettings.OpenCodeApiKey
	}
	if fcSettings.OpenRouterApiKey != "" {
		settings.OpenRouterApiKey = fcSettings.OpenRouterApiKey
	}
}

func (a *App) LoadSettings() (AppSettings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	path, err := getSettingsPath()
	if err != nil {
		return AppSettings{}, err
	}

	var settings AppSettings
	settings.WorkspaceModels = make(map[string]string)
	settings.WorkspaceHistory = make(map[string][]ChatMessage)
	settings.OllamaEndpoint = "http://localhost:11434"
	settings.SidebarWidth = 260
	settings.ChatWidth = 320
	settings.Theme = "dark"
	settings.EnableSearchCode = true
	settings.EnableContextCompression = true

	if _, err := os.Stat(path); os.IsNotExist(err) {
		importFreeCodeSettings(&settings)
		
		settingsMu.Unlock()
		_ = a.SaveSettings(settings)
		settingsMu.Lock()
		
		return settings, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return AppSettings{}, err
	}

	if err := json.Unmarshal(data, &settings); err != nil {
		return AppSettings{}, err
	}

	if settings.WorkspaceModels == nil {
		settings.WorkspaceModels = make(map[string]string)
	}
	if settings.WorkspaceHistory == nil {
		settings.WorkspaceHistory = make(map[string][]ChatMessage)
	}
	if settings.OllamaEndpoint == "" {
		settings.OllamaEndpoint = "http://localhost:11434"
	}
	if settings.SidebarWidth <= 0 {
		settings.SidebarWidth = 260
	}
	if settings.ChatWidth <= 0 {
		settings.ChatWidth = 320
	}
	if settings.Theme == "" {
		settings.Theme = "dark"
	}
	if settings.RepoMapTokens <= 0 {
		settings.RepoMapTokens = 1024
	}

	if settings.GeminiApiKey == "" || settings.OpenCodeApiKey == "" {
		importFreeCodeSettings(&settings)
		
		settingsMu.Unlock()
		_ = a.SaveSettings(settings)
		settingsMu.Lock()
	}

	return settings, nil
}

func (a *App) SaveSettings(settings AppSettings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	path, err := getSettingsPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	fmt.Printf("[DEBUG] SaveSettings JSON written:\n%s\n", string(data))
	return os.WriteFile(path, data, 0644)
}
