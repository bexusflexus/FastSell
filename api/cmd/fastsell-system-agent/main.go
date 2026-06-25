package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fastsell-api/internal/models"
)

type agentConfig struct {
	port             string
	expectedServices []string
	socketPath       string
}

type dockerClient struct {
	httpClient *http.Client
}

type dockerContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
	Ports  []dockerPort      `json:"Ports"`
}

type dockerPort struct {
	IP          string `json:"IP"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

type dockerInspect struct {
	Name  string `json:"Name"`
	Image string `json:"Image"`
	State struct {
		Status       string `json:"Status"`
		RestartCount int64  `json:"RestartCount"`
		StartedAt    string `json:"StartedAt"`
		FinishedAt   string `json:"FinishedAt"`
		Health       *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

func main() {
	cfg := loadAgentConfig()
	client := &dockerClient{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					var dialer net.Dialer
					return dialer.DialContext(ctx, "unix", cfg.socketPath)
				},
			},
			Timeout: 5 * time.Second,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health/docker", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		health, err := client.getDockerHealth(ctx, cfg.expectedServices)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(models.SystemDockerHealth{
				Status:  models.SystemStatusUnknown,
				Message: "Docker query failed inside fastsell-system-agent.",
				Alerts: []models.SystemHealthAlert{{
					Severity: models.SystemStatusWarning,
					Area:     "docker",
					Message:  "Docker system agent could not query the Docker daemon.",
				}},
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(health)
	})

	server := &http.Server{
		Addr:              "0.0.0.0:" + cfg.port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stopCh:
		log.Printf("fastsell-system-agent shutdown signal received: %s", sig)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("fastsell-system-agent failed: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("fastsell-system-agent shutdown failed: %v", err)
	}
}

func loadAgentConfig() agentConfig {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8081"
	}

	socketPath := strings.TrimSpace(os.Getenv("DOCKER_SOCKET_PATH"))
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}

	expectedRaw := strings.TrimSpace(os.Getenv("FASTSELL_DOCKER_EXPECTED_SERVICES"))
	if expectedRaw == "" {
		expectedRaw = "fastsell-web,fastsell-api,postgres,fastsell-system-agent"
	}

	parts := strings.Split(expectedRaw, ",")
	expected := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			expected = append(expected, value)
		}
	}

	return agentConfig{
		port:             port,
		expectedServices: expected,
		socketPath:       socketPath,
	}
}

func (c *dockerClient) getDockerHealth(ctx context.Context, expectedServices []string) (models.SystemDockerHealth, error) {
	containers, err := c.listContainers(ctx)
	if err != nil {
		return models.SystemDockerHealth{}, err
	}

	servicesByName := make(map[string]models.SystemDockerService)
	for _, container := range containers {
		serviceName := strings.TrimSpace(container.Labels["com.docker.compose.service"])
		if serviceName == "" {
			continue
		}

		inspect, err := c.inspectContainer(ctx, container.ID)
		if err != nil {
			return models.SystemDockerHealth{}, err
		}

		service := buildDockerService(serviceName, container, inspect)
		addDockerServiceAliases(servicesByName, service)
	}

	return evaluateDockerHealth(expectedServices, servicesByName), nil
}

func addDockerServiceAliases(servicesByName map[string]models.SystemDockerService, service models.SystemDockerService) {
	for _, alias := range []string{service.ServiceName, service.ContainerName} {
		key := dockerServiceKey(alias)
		if key == "" {
			continue
		}
		if _, exists := servicesByName[key]; !exists {
			servicesByName[key] = service
		}
	}
}

func dockerServiceKey(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "/"))
}

func evaluateDockerHealth(expectedServices []string, servicesByName map[string]models.SystemDockerService) models.SystemDockerHealth {
	alerts := make([]models.SystemHealthAlert, 0)
	services := make([]models.SystemDockerService, 0, len(expectedServices))
	overall := models.SystemStatusOK
	for _, expected := range expectedServices {
		service, ok := servicesByName[dockerServiceKey(expected)]
		if !ok {
			overall = models.SystemStatusFailed
			alerts = append(alerts, models.SystemHealthAlert{
				Severity: models.SystemStatusFailed,
				Area:     "docker",
				Message:  "Expected Docker service is missing: " + expected + ".",
			})
			services = append(services, models.SystemDockerService{
				ServiceName: expected,
				State:       "unknown",
				Health:      "unknown",
				Status:      models.SystemStatusFailed,
			})
			continue
		}

		services = append(services, service)
		switch service.Status {
		case models.SystemStatusFailed:
			overall = models.SystemStatusFailed
			alerts = append(alerts, models.SystemHealthAlert{
				Severity: models.SystemStatusFailed,
				Area:     "docker",
				Message:  "Docker service is not healthy: " + expected + ".",
			})
		case models.SystemStatusWarning:
			if overall != models.SystemStatusFailed {
				overall = models.SystemStatusWarning
			}
			alerts = append(alerts, models.SystemHealthAlert{
				Severity: models.SystemStatusWarning,
				Area:     "docker",
				Message:  "Docker service health is limited or unknown: " + expected + ".",
			})
		}
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].ServiceName < services[j].ServiceName
	})

	now := time.Now().UTC()
	message := "Docker service health was read from fastsell-system-agent."
	if overall == models.SystemStatusFailed {
		message = "One or more critical Docker services are failed or missing."
	} else if overall == models.SystemStatusWarning {
		message = "Docker services are running, but at least one health signal is limited."
	}

	return models.SystemDockerHealth{
		Status:            overall,
		Message:           message,
		GeneratedDatetime: &now,
		Services:          services,
		Alerts:            alerts,
	}
}

func (c *dockerClient) listContainers(ctx context.Context) ([]dockerContainerSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/v1.41/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("failed to list Docker containers")
	}

	var payload []dockerContainerSummary
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *dockerClient) inspectContainer(ctx context.Context, id string) (dockerInspect, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/v1.41/containers/"+id+"/json", nil)
	if err != nil {
		return dockerInspect{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return dockerInspect{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return dockerInspect{}, errors.New("failed to inspect Docker container")
	}

	var payload dockerInspect
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return dockerInspect{}, err
	}
	return payload, nil
}

func buildDockerService(serviceName string, summary dockerContainerSummary, inspect dockerInspect) models.SystemDockerService {
	state := strings.TrimSpace(inspect.State.Status)
	if state == "" {
		state = strings.TrimSpace(summary.State)
	}
	if state == "" {
		state = "unknown"
	}

	health := "none"
	if inspect.State.Health != nil && strings.TrimSpace(inspect.State.Health.Status) != "" {
		health = strings.TrimSpace(inspect.State.Health.Status)
	}

	startedAt := parseDockerTime(inspect.State.StartedAt)
	finishedAt := parseDockerTime(inspect.State.FinishedAt)
	image := strings.TrimSpace(inspect.Config.Image)
	if image == "" {
		image = strings.TrimSpace(summary.Image)
	}

	containerName := strings.TrimPrefix(strings.TrimSpace(inspect.Name), "/")
	if containerName == "" && len(summary.Names) > 0 {
		containerName = strings.TrimPrefix(summary.Names[0], "/")
	}

	status := models.SystemStatusOK
	switch {
	case state == "exited" || state == "dead" || state == "created" || state == "removing":
		status = models.SystemStatusFailed
	case state != "running":
		status = models.SystemStatusWarning
	case health == "unhealthy":
		status = models.SystemStatusFailed
	case health == "starting" || health == "unknown":
		status = models.SystemStatusWarning
	default:
		status = models.SystemStatusOK
	}

	return models.SystemDockerService{
		ServiceName:   serviceName,
		ContainerName: containerName,
		Image:         image,
		State:         state,
		Health:        health,
		RestartCount:  inspect.State.RestartCount,
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		Ports:         formatPorts(summary.Ports),
		Status:        status,
	}
}

func formatPorts(ports []dockerPort) []string {
	if len(ports) == 0 {
		return nil
	}

	values := make([]string, 0, len(ports))
	for _, port := range ports {
		privatePort := strconv.Itoa(port.PrivatePort)
		if port.PublicPort > 0 {
			values = append(values, strconv.Itoa(port.PublicPort)+":"+privatePort)
			continue
		}
		values = append(values, privatePort+"/"+port.Type)
	}
	sort.Strings(values)
	return values
}

func parseDockerTime(value string) *time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "0001-01-01T00:00:00Z" {
		return nil
	}

	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return nil
	}

	utc := parsed.UTC()
	return &utc
}
