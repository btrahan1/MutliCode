package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Image   string `json:"image,omitempty"` // Base64 data URL
}

type GeminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *GeminiInlineData `json:"inlineData,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role"`
	Parts []GeminiPart `json:"parts"`
}

// SendChatMessage routes the request to Ollama, Gemini, or OpenCode based on modelName.
func (a *App) SendChatMessage(modelName string, prompt string, history []ChatMessage, systemPrompt string) (string, error) {
	settings, err := a.LoadSettings()
	if err != nil {
		return "", fmt.Errorf("failed to load settings: %w", err)
	}

	normalizedModel := strings.ToLower(modelName)

	// Route 1: OpenCode Models (big-pickle, deepseek-v4-flash-free, etc.)
	if modelName == "big-pickle" || modelName == "DeepSeek Flash Free" || normalizedModel == "deepseek-v4-flash-free" {
		if settings.OpenCodeApiKey == "" {
			return "", fmt.Errorf("OpenCode API key is not configured. Please verify your FreeCode settings.")
		}

		apiModel := modelName
		if modelName == "DeepSeek Flash Free" {
			apiModel = "deepseek-v4-flash-free"
		}

		return callOpenCode(apiModel, prompt, history, systemPrompt, settings.OpenCodeApiKey)
	}

	// Route 2: Gemini Models
	if strings.Contains(normalizedModel, "gemini") {
		apiKey := settings.GeminiApiKey
		if apiKey == "" {
			return "", fmt.Errorf("Gemini API key is not configured. Please verify your FreeCode settings.")
		}
		return callGemini(modelName, prompt, history, systemPrompt, apiKey)
	}

	// Route 3: OpenRouter Models
	if strings.Contains(modelName, "/") {
		apiKey := settings.OpenRouterApiKey
		if apiKey == "" {
			return "", fmt.Errorf("OpenRouter API key is not configured. Please verify your settings.")
		}
		return callOpenRouter(modelName, prompt, history, systemPrompt, apiKey)
	}

	// Route 4: Local Ollama
	ollamaURL := "http://localhost:11434"
	if settings.OllamaEndpoint != "" {
		ollamaURL = settings.OllamaEndpoint
	}

	return callOllama(modelName, prompt, history, systemPrompt, ollamaURL)
}

func sendJSONRequest(client *http.Client, method, url string, headers map[string]string, body []byte) ([]byte, int, error) {
	backoff := 2 * time.Second
	maxRetries := 5

	for attempt := 0; attempt < maxRetries; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewBuffer(body)
		}

		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return nil, 0, err
		}

		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			if attempt < maxRetries-1 {
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			return nil, 0, err
		}
		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, resp.StatusCode, err
		}

		// Handle HTTP 429 Rate Limiting with exponential backoff
		if resp.StatusCode == 429 {
			if attempt < maxRetries-1 {
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
		}

		return respBytes, resp.StatusCode, nil
	}
	return nil, 0, fmt.Errorf("max retries exceeded")
}

func callGemini(modelName string, prompt string, history []ChatMessage, systemPrompt string, apiKey string) (string, error) {
	apiModel := "gemini-2.5-flash"
	normalized := strings.ToLower(modelName)
	if strings.Contains(normalized, "1.5-pro") || strings.Contains(normalized, "pro") {
		apiModel = "gemini-1.5-pro"
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", apiModel, apiKey)

	var contents []GeminiContent
	for _, msg := range history {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		
		parts := []GeminiPart{{Text: msg.Content}}
		if msg.Image != "" {
			mimeType := "image/png"
			base64Data := msg.Image
			if idx := strings.Index(msg.Image, ";base64,"); idx != -1 {
				mimeType = strings.TrimPrefix(msg.Image[:idx], "data:")
				base64Data = msg.Image[idx+8:]
			}
			parts = append(parts, GeminiPart{
				InlineData: &GeminiInlineData{
					MimeType: mimeType,
					Data:     base64Data,
				},
			})
		}

		contents = append(contents, GeminiContent{
			Role:  role,
			Parts: parts,
		})
	}

	// Handled prompt with potential image inside history construction in StartAgent
	// But in case SendChatMessage is called with a separate prompt directly,
	// we append it here:
	if prompt != "" {
		contents = append(contents, GeminiContent{
			Role:  "user",
			Parts: []GeminiPart{{Text: prompt}},
		})
	}

	reqBody := map[string]interface{}{
		"contents": contents,
	}

	if systemPrompt != "" {
		reqBody["systemInstruction"] = map[string]interface{}{
			"parts": []interface{}{
				map[string]interface{}{
					"text": systemPrompt,
				},
			},
		}
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 90 * time.Second}
	respBytes, statusCode, err := sendJSONRequest(client, "POST", url, nil, jsonBytes)
	if err != nil {
		return "", err
	}

	if statusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API error %d: %s", statusCode, string(respBytes))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		return result.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("empty response from Gemini API")
}

func callOpenCode(modelName string, prompt string, history []ChatMessage, systemPrompt string, apiKey string) (string, error) {
	url := "https://opencode.ai/zen/v1/chat/completions"

	type OpenCodeMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var messages []OpenCodeMsg
	if systemPrompt != "" {
		messages = append(messages, OpenCodeMsg{Role: "system", Content: systemPrompt})
	}

	for _, m := range history {
		messages = append(messages, OpenCodeMsg{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, OpenCodeMsg{Role: "user", Content: prompt})

	reqBody := map[string]interface{}{
		"model":    modelName,
		"messages": messages,
		"stream":   false,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	client := &http.Client{Timeout: 90 * time.Second}
	respBytes, statusCode, err := sendJSONRequest(client, "POST", url, headers, jsonBytes)
	if err != nil {
		return "", err
	}

	if statusCode != http.StatusOK {
		return "", fmt.Errorf("OpenCode API error %d: %s", statusCode, string(respBytes))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("empty response from OpenCode API")
}

func callOllama(modelName string, prompt string, history []ChatMessage, systemPrompt string, endpoint string) (string, error) {
	cleanedEndpoint := strings.TrimSuffix(endpoint, "/")
	cleanedEndpoint = strings.TrimSuffix(cleanedEndpoint, "/v1")
	cleanedEndpoint = strings.TrimSuffix(cleanedEndpoint, "/")
	url := fmt.Sprintf("%s/api/chat", cleanedEndpoint)

	type OllamaMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var messages []OllamaMsg
	if systemPrompt != "" {
		messages = append(messages, OllamaMsg{Role: "system", Content: systemPrompt})
	}

	for _, m := range history {
		messages = append(messages, OllamaMsg{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, OllamaMsg{Role: "user", Content: prompt})

	reqBody := map[string]interface{}{
		"model":    modelName,
		"messages": messages,
		"stream":   false,
		"options": map[string]interface{}{
			"num_ctx": 32768,
		},
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 90 * time.Second}
	respBytes, statusCode, err := sendJSONRequest(client, "POST", url, nil, jsonBytes)
	if err != nil {
		return "", err
	}

	if statusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama error %d: %s", statusCode, string(respBytes))
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	return result.Message.Content, nil
}

func callOpenRouter(modelName string, prompt string, history []ChatMessage, systemPrompt string, apiKey string) (string, error) {
	url := "https://openrouter.ai/api/v1/chat/completions"

	type OpenRouterMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var messages []OpenRouterMsg
	if systemPrompt != "" {
		messages = append(messages, OpenRouterMsg{Role: "system", Content: systemPrompt})
	}

	for _, m := range history {
		messages = append(messages, OpenRouterMsg{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, OpenRouterMsg{Role: "user", Content: prompt})

	reqBody := map[string]interface{}{
		"model":    modelName,
		"messages": messages,
		"stream":   false,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"HTTP-Referer":  "https://github.com/btrahan1/MutliCode",
		"X-Title":       "MultiCode",
	}

	client := &http.Client{Timeout: 90 * time.Second}
	respBytes, statusCode, err := sendJSONRequest(client, "POST", url, headers, jsonBytes)
	if err != nil {
		return "", err
	}

	if statusCode != http.StatusOK {
		return "", fmt.Errorf("OpenRouter API error %d: %s", statusCode, string(respBytes))
	}

	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	if len(result.Choices) > 0 {
		content := result.Choices[0].Message.Content
		if result.Model != "" {
			content = fmt.Sprintf("%s\n\n*(OpenRouter: Routed to `%s`)*", content, result.Model)
		}
		return content, nil
	}

	return "", fmt.Errorf("empty response from OpenRouter API")
}
