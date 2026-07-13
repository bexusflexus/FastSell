package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type stubReleaseLookup struct {
	latest string
	err    error
}

func (s stubReleaseLookup) LatestStable(context.Context) (string, error) {
	return s.latest, s.err
}

func TestVersionHandlerReportsUpdateStates(t *testing.T) {
	tests := []struct {
		name      string
		installed string
		latest    string
		available bool
	}{
		{name: "installed older", installed: "v0.1.3", latest: "v0.1.4", available: true},
		{name: "installed equal", installed: "v0.1.3", latest: "v0.1.3", available: false},
		{name: "installed newer", installed: "v0.1.4", latest: "v0.1.3", available: false},
		{name: "candidate does not claim update", installed: "candidate-a1b2c3d", latest: "v0.1.4", available: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewVersionHandler(test.installed, stubReleaseLookup{latest: test.latest})
			request := httptest.NewRequest(http.MethodGet, "/api/system/version", nil)
			recorder := httptest.NewRecorder()

			handler.Get(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", recorder.Code)
			}
			var response struct {
				InstalledVersion string  `json:"installed_version"`
				LatestVersion    *string `json:"latest_version"`
				UpdateAvailable  bool    `json:"update_available"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.InstalledVersion != test.installed {
				t.Fatalf("expected installed version %q, got %q", test.installed, response.InstalledVersion)
			}
			if response.LatestVersion == nil || *response.LatestVersion != test.latest {
				t.Fatalf("expected latest version %q, got %#v", test.latest, response.LatestVersion)
			}
			if response.UpdateAvailable != test.available {
				t.Fatalf("expected update_available=%t, got %t", test.available, response.UpdateAvailable)
			}
		})
	}
}

func TestVersionHandlerHidesLookupFailure(t *testing.T) {
	handler := NewVersionHandler("v0.1.3", stubReleaseLookup{err: context.Canceled})
	recorder := httptest.NewRecorder()
	handler.Get(recorder, httptest.NewRequest(http.MethodGet, "/api/system/version", nil))

	var response struct {
		InstalledVersion string  `json:"installed_version"`
		LatestVersion    *string `json:"latest_version"`
		UpdateAvailable  bool    `json:"update_available"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.InstalledVersion != "v0.1.3" || response.LatestVersion != nil || response.UpdateAvailable {
		t.Fatalf("expected installed-only response after lookup failure, got %#v", response)
	}
}

func TestGitHubReleaseLookupIgnoresDraftsAndPrereleasesAndCaches(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
          {"tag_name":"v0.1.20","draft":true,"prerelease":false},
          {"tag_name":"v0.1.19-rc.1","draft":false,"prerelease":true},
          {"tag_name":"v0.1.10","draft":false,"prerelease":false},
          {"tag_name":"v0.1.9","draft":false,"prerelease":false}
        ]`))
	}))
	defer server.Close()

	lookup := NewGitHubReleaseLookupForTest(server.URL, server.Client(), time.Hour, time.Now)
	first, err := lookup.LatestStable(context.Background())
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	second, err := lookup.LatestStable(context.Background())
	if err != nil {
		t.Fatalf("cached lookup: %v", err)
	}
	if first != "v0.1.10" || second != first {
		t.Fatalf("expected v0.1.10 from both lookups, got %q and %q", first, second)
	}
	if requests.Load() != 1 {
		t.Fatalf("expected one GitHub request while cache is fresh, got %d", requests.Load())
	}
}

func TestGitHubReleaseLookupFailureIsCached(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	lookup := NewGitHubReleaseLookupForTest(server.URL, server.Client(), time.Hour, time.Now)
	if _, err := lookup.LatestStable(context.Background()); err == nil {
		t.Fatal("expected failed release lookup")
	}
	if _, err := lookup.LatestStable(context.Background()); err == nil {
		t.Fatal("expected cached failed release lookup")
	}
	if requests.Load() != 1 {
		t.Fatalf("expected one GitHub request while failure cache is fresh, got %d", requests.Load())
	}
}
