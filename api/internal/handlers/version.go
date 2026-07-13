package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"
	"fastsell-api/internal/version"
)

const (
	defaultGitHubReleasesURL = "https://api.github.com/repos/bexusflexus/FastSell/releases?per_page=100"
	defaultReleaseCacheTTL   = 6 * time.Hour
)

type VersionHandler struct {
	installedVersion string
	lookup           StableReleaseLookup
}

type StableReleaseLookup interface {
	LatestStable(ctx context.Context) (string, error)
}

type githubReleaseLookup struct {
	url        string
	client     *http.Client
	cacheTTL   time.Duration
	now        func() time.Time
	mu         sync.Mutex
	fetchedAt  time.Time
	latest     *string
	lastStatus bool
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func NewVersionHandler(installedVersion string, lookup StableReleaseLookup) *VersionHandler {
	return &VersionHandler{installedVersion: installedVersion, lookup: lookup}
}

func NewGitHubReleaseLookup() StableReleaseLookup {
	return &githubReleaseLookup{
		url:      defaultGitHubReleasesURL,
		client:   &http.Client{Timeout: 5 * time.Second},
		cacheTTL: defaultReleaseCacheTTL,
		now:      time.Now,
	}
}

func NewGitHubReleaseLookupForTest(url string, client *http.Client, cacheTTL time.Duration, now func() time.Time) StableReleaseLookup {
	return &githubReleaseLookup{url: url, client: client, cacheTTL: cacheTTL, now: now}
}

func (h *VersionHandler) Get(w http.ResponseWriter, r *http.Request) {
	response := models.SystemVersionResponse{InstalledVersion: h.installedVersion}
	latest, err := h.lookup.LatestStable(r.Context())
	if err != nil {
		log.Printf("stable release lookup unavailable: %v", err)
		respond.JSON(w, http.StatusOK, response)
		return
	}

	response.LatestVersion = &latest
	installed, installedErr := version.Parse(h.installedVersion)
	latestParsed, latestErr := version.Parse(latest)
	if installedErr == nil && latestErr == nil && version.Compare(installed, latestParsed) < 0 {
		response.UpdateAvailable = true
	}
	respond.JSON(w, http.StatusOK, response)
}

func (l *githubReleaseLookup) LatestStable(ctx context.Context) (string, error) {
	l.mu.Lock()
	if !l.fetchedAt.IsZero() && l.now().Sub(l.fetchedAt) < l.cacheTTL {
		latest := ""
		if l.latest != nil {
			latest = *l.latest
		}
		if l.lastStatus {
			l.mu.Unlock()
			return latest, nil
		}
		l.mu.Unlock()
		return "", fmt.Errorf("cached release lookup failed")
	}
	l.mu.Unlock()

	latest, err := l.fetch(ctx)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fetchedAt = l.now()
	l.lastStatus = err == nil
	if err == nil {
		l.latest = &latest
	} else {
		l.latest = nil
	}
	return latest, err
}

func (l *githubReleaseLookup) fetch(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.url, nil)
	if err != nil {
		return "", fmt.Errorf("build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FastSell-release-check")

	response, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request release metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("release metadata returned HTTP %d", response.StatusCode)
	}

	var releases []githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&releases); err != nil {
		return "", fmt.Errorf("decode release metadata: %w", err)
	}

	var latest version.Version
	latestDisplay := ""
	for _, release := range releases {
		if release.Draft || release.Prerelease || !version.IsStable(release.TagName) {
			continue
		}
		parsed, err := version.Parse(release.TagName)
		if err != nil {
			continue
		}
		if latestDisplay == "" || version.Compare(parsed, latest) > 0 {
			latest = parsed
			latestDisplay, err = version.NormalizeStable(release.TagName)
			if err != nil {
				return "", fmt.Errorf("normalize release metadata: %w", err)
			}
		}
	}
	if latestDisplay == "" {
		return "", fmt.Errorf("no stable release found")
	}
	return latestDisplay, nil
}
