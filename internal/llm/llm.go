// Package llm is the OPTIONAL model layer, adapted from COGO's: Searchgirl
// is fully functional without a model — the "Respuesta IA" mode simply stays
// hidden. One OpenAI-compatible client covers local (Ollama, LM Studio) and
// cheap remote APIs (OpenRouter, DeepSeek); anthropic.go adds the native
// Anthropic provider.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Provider is the minimal contract a model must satisfy for Searchgirl.
type Provider interface {
	Available() bool
	Name() string
	Complete(ctx context.Context, prompt string) (string, error)
}

// Noop is the default: no model configured.
type Noop struct{}

func (Noop) Available() bool { return false }
func (Noop) Name() string    { return "none" }
func (Noop) Complete(context.Context, string) (string, error) {
	return "", fmt.Errorf("no LLM provider configured")
}

// OpenAICompatible talks to any /chat/completions endpoint.
// Local: BaseURL "http://localhost:11434/v1". Remote: an APIKey on top.
type OpenAICompatible struct {
	BaseURL string
	Model   string
	APIKey  string
	Referer string       // optional, sent as HTTP-Referer (OpenRouter attribution)
	Client  *http.Client // optional; a 60s client is used if nil
}

func (o *OpenAICompatible) Available() bool { return o.BaseURL != "" && o.Model != "" }
func (o *OpenAICompatible) Name() string    { return o.Model }

func (o *OpenAICompatible) Complete(ctx context.Context, prompt string) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"model":       o.Model,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": 0,
		"stream":      false,
	})
	url := strings.TrimRight(o.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	req.Header.Set("X-Title", "Searchgirl")
	if o.Referer != "" {
		req.Header.Set("HTTP-Referer", o.Referer)
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm http %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: empty response")
	}
	return out.Choices[0].Message.Content, nil
}

// FromEnv picks the provider. Priority: Anthropic native (ANTHROPIC_API_KEY),
// then OpenAI-compatible (LLM_BASE_URL + LLM_MODEL), otherwise Noop (off).
func FromEnv() Provider {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		model := os.Getenv("ANTHROPIC_MODEL")
		if model == "" {
			model = "claude-haiku-4-5-20251001" // rápido y barato: el default sensato para síntesis de búsqueda
		}
		return &Anthropic{APIKey: key, Model: model}
	}
	base, model := os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_MODEL")
	if base == "" || model == "" {
		return Noop{}
	}
	return &OpenAICompatible{BaseURL: base, Model: model, APIKey: os.Getenv("LLM_API_KEY"), Referer: os.Getenv("LLM_REFERER")}
}
