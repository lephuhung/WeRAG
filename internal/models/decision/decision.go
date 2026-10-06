// Package decision calls typed decision models — TypeSafe Jev and
// Cloudflare Clef, which share the Jev API: a state plus a map of typed
// questions (noul / choice / score) in, calibrated probabilities out. The
// model never generates free text, so callers fully control the question
// set and only receive scores for it.
package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
)

// MaxQuestions is the Jev/Clef per-request question limit.
const MaxQuestions = 64

// Question types.
const (
	QuestionNoul   = "noul"
	QuestionChoice = "choice"
	QuestionScore  = "score"
)

// Question is one typed question. Criteria is a map[string]string for
// choice and a []string rubric for score; nil for noul.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Answer is the model's answer to one question.
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Score         *float64           `json:"score,omitempty"`
}

// Decider asks a decision model a batch of typed questions about state.
type Decider interface {
	// Provider is the provider name (jev or clef).
	Provider() string
	// Decide returns one answer per question ID. At most MaxQuestions.
	Decide(ctx context.Context, state string, questions map[string]Question) (map[string]Answer, error)
}

// Config is the runtime configuration of a decision model.
type Config struct {
	Provider  string
	BaseURL   string
	APIKey    string
	ModelName string
	// AccountID is required by Clef (Cloudflare account).
	AccountID     string
	CustomHeaders map[string]string
	Timeout       time.Duration
}

// ConfigFromModel maps a stored Decision model to its runtime config.
func ConfigFromModel(m *types.Model) *Config {
	if m == nil {
		return nil
	}
	return &Config{
		Provider:      m.Parameters.Provider,
		BaseURL:       m.Parameters.BaseURL,
		APIKey:        m.Parameters.APIKey,
		ModelName:     m.Name,
		AccountID:     strings.TrimSpace(m.Parameters.ExtraConfig[provider.ClefAccountIDKey]),
		CustomHeaders: m.Parameters.CustomHeaders,
	}
}

// NewDecider builds the client for cfg.Provider (jev or clef).
func NewDecider(cfg *Config) (Decider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("decision model config is nil")
	}
	model := strings.TrimSpace(cfg.ModelName)
	if model == "" {
		return nil, fmt.Errorf("decision model name is required")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	var url string
	switch provider.ProviderName(cfg.Provider) {
	case provider.ProviderJev:
		if base == "" {
			base = provider.JevBaseURL
		}
		url = base + "/systemone"
	case provider.ProviderClef:
		if cfg.AccountID == "" {
			return nil, fmt.Errorf("clef requires %s", provider.ClefAccountIDKey)
		}
		if base == "" {
			base = provider.ClefBaseURL
		}
		// Accept either "clef-flash" or the Workers AI id "@cf/cloudflare/clef-flash".
		model = strings.TrimPrefix(model, "@cf/cloudflare/")
		url = base + "/accounts/" + cfg.AccountID + "/ai/run/@cf/cloudflare/" + model
	default:
		return nil, fmt.Errorf("unsupported decision provider %q (expected jev or clef)", cfg.Provider)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &client{
		provider: cfg.Provider,
		url:      url,
		apiKey:   cfg.APIKey,
		model:    model,
		headers:  cfg.CustomHeaders,
		http:     &http.Client{Timeout: timeout},
	}, nil
}

type client struct {
	provider string
	url      string
	apiKey   string
	model    string
	headers  map[string]string
	http     *http.Client
}

func (c *client) Provider() string { return c.provider }

type request struct {
	Model     string              `json:"model"`
	State     string              `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type result struct {
	Answers map[string]Answer `json:"answers"`
}

// response accepts both the Workers AI envelope ({"result": …, "success"})
// and the bare Jev response ({"answers": …}).
type response struct {
	result
	Result  *result `json:"result"`
	Success *bool   `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *client) Decide(ctx context.Context, state string, questions map[string]Question) (map[string]Answer, error) {
	if len(questions) == 0 {
		return map[string]Answer{}, nil
	}
	if len(questions) > MaxQuestions {
		return nil, fmt.Errorf("%d questions exceed the %d-question limit", len(questions), MaxQuestions)
	}
	body, err := json.Marshal(request{Model: c.model, State: state, Questions: questions})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", c.provider, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%s read: %w", c.provider, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s status %d: %s", c.provider, resp.StatusCode, truncate(raw, 300))
	}
	var parsed response
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("%s decode: %w", c.provider, err)
	}
	if parsed.Success != nil && !*parsed.Success {
		msg := "unsuccessful response"
		if len(parsed.Errors) > 0 {
			msg = parsed.Errors[0].Message
		}
		return nil, fmt.Errorf("%s: %s", c.provider, msg)
	}
	answers := parsed.Answers
	if parsed.Result != nil {
		answers = parsed.Result.Answers
	}
	for id := range questions {
		if _, ok := answers[id]; !ok {
			return nil, fmt.Errorf("%s: missing answer for question %q", c.provider, id)
		}
	}
	return answers, nil
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}
