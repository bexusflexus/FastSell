package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fastsell-api/internal/models"
)

type systemAgentClient struct {
	baseURL string
	client  *http.Client
}

func newSystemAgentClient(baseURL string) *systemAgentClient {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return nil
	}

	return &systemAgentClient{
		baseURL: trimmed,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *systemAgentClient) GetDockerHealth(ctx context.Context) (models.SystemDockerHealth, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health/docker", nil)
	if err != nil {
		return models.SystemDockerHealth{}, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return models.SystemDockerHealth{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.SystemDockerHealth{}, fmt.Errorf("system agent returned HTTP %d", resp.StatusCode)
	}

	var payload models.SystemDockerHealth
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return models.SystemDockerHealth{}, err
	}

	return payload, nil
}
