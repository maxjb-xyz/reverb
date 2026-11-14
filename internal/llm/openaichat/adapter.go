// Package openaichat is an LLM adapter for any server that speaks the OpenAI
// chat completions API (POST {base}/chat/completions): Ollama, llama.cpp's
// server, LM Studio, vLLM, LocalAI, OpenAI, OpenRouter and others.
package openaichat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/maxjb-xyz/reverb/internal/llm"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

// Adapter is the OpenAI-compatible chat LLM.
type Adapter struct {
	baseURL string
	apiKey  string
	model   string
	hc      *http.Client
}

// New returns an unconfigured adapter (the registry factory).
func New() *Adapter { return &Adapter{hc: &http.Client{Timeout: 60 * time.Second}} }

func (a *Adapter) Type() string { return llm.Type }
func (a *Adapter) Name() string { return "openai-chat" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "base_url", Label: "Server URL", Type: "string", Required: true,
			Help: "The API root, ending before /chat/completions. Ollama: http://<host>:11434/v1. llama.cpp: http://<host>:8080/v1. OpenAI: https://api.openai.com/v1."},
		{Key: "model", Label: "Model", Type: "string", Required: true,
			Help: "For example llama3.2 or qwen2.5:7b on Ollama. A small instruct model is plenty for DJ lines."},
		{Key: "api_key", Label: "API key", Type: "string", Secret: true,
			Help: "Only needed for hosted services. Local servers usually take none."},
	}}
}

func cfgString(cfg map[string]any, key string) string {
	if s, ok := cfg[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func (a *Adapter) Init(cfg map[string]any) error {
	a.baseURL = strings.TrimRight(cfgString(cfg, "base_url"), "/")
	a.model = cfgString(cfg, "model")
	a.apiKey = cfgString(cfg, "api_key")
	if a.baseURL == "" || a.model == "" {
		return fmt.Errorf("openai-chat: base_url and model are required")
	}
	return nil
}

// TestConnection asks for a one-word answer.
func (a *Adapter) TestConnection(ctx context.Context) error {
	_, err := a.Complete(ctx, llm.Request{Prompt: "Reply with the word OK.", MaxTokens: 16})
	return err
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (a *Adapter) Complete(ctx context.Context, req llm.Request) (string, error) {
	if err := llm.CheckRequest(req); err != nil {
		return "", err
	}
	if a.baseURL == "" {
		return "", fmt.Errorf("openai-chat: not initialized")
	}
	body := chatRequest{Model: a.model, MaxTokens: req.MaxTokens}
	if strings.TrimSpace(req.System) != "" {
		body.Messages = append(body.Messages, chatMessage{Role: "system", Content: req.System})
	}
	body.Messages = append(body.Messages, chatMessage{Role: "user", Content: req.Prompt})
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	hr.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		hr.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	resp, err := a.hc.Do(hr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return "", fmt.Errorf("openai-chat: HTTP %d: %s", resp.StatusCode, msg)
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("openai-chat: bad answer: %w", err)
	}
	if len(cr.Choices) == 0 || strings.TrimSpace(cr.Choices[0].Message.Content) == "" {
		return "", llm.ErrNoText
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}
