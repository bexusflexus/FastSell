package backup

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseTimezonesIgnoresCommentsMalformedRowsAndDeduplicates(t *testing.T) {
	input := strings.NewReader("\n# comment\n" +
		"US\t+404251-0740023\tAmerica/New_York\tEastern\n" +
		"US\t+415100-0873900\tAmerica/Chicago\n" +
		"malformed\n" +
		"US\t+415100-0873900\t\n" +
		"US\t+415100-0873900\tAmerica/Chicago\n" +
		"XX\t+0000+00000\tInvalid Zone\n")
	timezones, err := ParseTimezones(input)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"America/Chicago", "America/New_York", "UTC"}
	if !slices.Equal(timezones, expected) {
		t.Fatalf("unexpected parsed timezones: %#v", timezones)
	}
}

func TestProductionTimezonesLoadAndValidate(t *testing.T) {
	timezones, err := LoadTimezones(TimezoneDataFile)
	if err != nil {
		t.Fatalf("production timezone data did not load: %v", err)
	}
	if !slices.Contains(timezones, "UTC") || !slices.Contains(timezones, "America/Chicago") {
		t.Fatalf("representative timezones missing: %#v", timezones)
	}
	if !slices.IsSorted(timezones) {
		t.Fatal("production timezone results are not sorted")
	}
	for _, identifier := range timezones {
		if _, err := time.LoadLocation(identifier); err != nil {
			t.Fatalf("returned timezone %q does not load: %v", identifier, err)
		}
	}
}

func TestLoadTimezonesMissingSourceReturnsClearError(t *testing.T) {
	_, err := LoadTimezones("/definitely/missing/zone1970.tab")
	if err == nil || !strings.Contains(err.Error(), "load timezone data") {
		t.Fatalf("missing source error was not clear: %v", err)
	}
}
