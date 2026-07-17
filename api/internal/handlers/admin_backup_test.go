package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAdminBackupTimezonesEndpoint(t *testing.T) {
	root := t.TempDir()
	timezoneFile := filepath.Join(root, "zone1970.tab")
	if err := os.WriteFile(timezoneFile, []byte("US\t+415100-0873900\tAmerica/Chicago\n"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := &AdminBackupHandler{timezoneFile: timezoneFile}
	recorder := httptest.NewRecorder()
	handler.GetTimezones(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/backup/timezones", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Timezones []string `json:"timezones"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(response.Timezones, []string{"America/Chicago", "UTC"}) {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestAdminBackupTimezonesEndpointReportsMissingData(t *testing.T) {
	handler := &AdminBackupHandler{timezoneFile: "/missing/zone1970.tab"}
	recorder := httptest.NewRecorder()
	handler.GetTimezones(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/backup/timezones", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	var response struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected a clear JSON error: %v", err)
	}
	if response.Error != "server timezone data is unavailable" {
		t.Fatalf("unexpected missing-data error: %q", response.Error)
	}
}
