package main

import (
	"strings"
	"testing"

	"fastsell-api/internal/models"
)

func TestEvaluateDockerHealthMatchesExpectedContainerNameAlias(t *testing.T) {
	servicesByName := make(map[string]models.SystemDockerService)
	addDockerServiceAliases(servicesByName, models.SystemDockerService{
		ServiceName:   "postgres",
		ContainerName: "fastsell-postgres",
		State:         "running",
		Health:        "healthy",
		Status:        models.SystemStatusOK,
	})

	health := evaluateDockerHealth([]string{"fastsell-postgres"}, servicesByName)
	if health.Status != models.SystemStatusOK {
		t.Fatalf("expected docker status ok, got %q", health.Status)
	}
	if len(health.Alerts) != 0 {
		t.Fatalf("expected no docker alerts, got %#v", health.Alerts)
	}
	if len(health.Services) != 1 {
		t.Fatalf("expected one docker service, got %d", len(health.Services))
	}
	if health.Services[0].ServiceName != "postgres" {
		t.Fatalf("expected compose service name postgres, got %q", health.Services[0].ServiceName)
	}
	if health.Services[0].ContainerName != "fastsell-postgres" {
		t.Fatalf("expected container name fastsell-postgres, got %q", health.Services[0].ContainerName)
	}
}

func TestEvaluateDockerHealthReportsTrulyMissingService(t *testing.T) {
	health := evaluateDockerHealth([]string{"fastsell-postgres"}, map[string]models.SystemDockerService{})
	if health.Status != models.SystemStatusFailed {
		t.Fatalf("expected docker status failed, got %q", health.Status)
	}
	if len(health.Alerts) != 1 {
		t.Fatalf("expected one docker alert, got %d", len(health.Alerts))
	}
	if !strings.Contains(health.Alerts[0].Message, "Expected Docker service is missing: fastsell-postgres.") {
		t.Fatalf("unexpected alert message: %q", health.Alerts[0].Message)
	}
	if len(health.Services) != 1 {
		t.Fatalf("expected one placeholder service, got %d", len(health.Services))
	}
	if health.Services[0].Status != models.SystemStatusFailed {
		t.Fatalf("expected placeholder service status failed, got %q", health.Services[0].Status)
	}
}
