package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Remote calls an HTTP translation API (e.g. LibreTranslate-compatible, or
// any operator-hosted endpoint accepting {q, source, target} JSON and
// returning {translatedText}) — kept generic and self-hosted-friendly so
// no cloud vendor is hard-coded.
type Remote struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

// NewRemote builds a Remote translator targeting the given endpoint.
func NewRemote(endpoint, apiKey string) *Remote {
	return &Remote{
		Endpoint: endpoint,
		APIKey:   apiKey,
		Client:   &http.Client{Timeout: 8 * time.Second},
	}
}

type remoteReq struct {
	Q      string `json:"q"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type remoteResp struct {
	TranslatedText string `json:"translatedText"`
}

// Translate implements usecase.Translator.
func (r *Remote) Translate(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
	if r.Endpoint == "" {
		return "", fmt.Errorf("translate: Remote has no Endpoint configured")
	}
	body, err := json.Marshal(remoteReq{Q: text, Source: sourceLang, Target: targetLang})
	if err != nil {
		return "", fmt.Errorf("translate: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("translate: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.APIKey)
	}

	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("translate: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("translate: remote returned %d: %s", resp.StatusCode, string(b))
	}

	var out remoteResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("translate: decode response: %w", err)
	}
	return out.TranslatedText, nil
}
