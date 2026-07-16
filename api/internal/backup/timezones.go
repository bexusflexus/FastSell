package backup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const TimezoneDataFile = "/usr/share/zoneinfo/zone1970.tab"

func LoadTimezones(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("load timezone data: %w", err)
	}
	defer file.Close()

	timezones, err := ParseTimezones(file)
	if err != nil {
		return nil, fmt.Errorf("load timezone data: %w", err)
	}
	return timezones, nil
}

func ParseTimezones(reader io.Reader) ([]string, error) {
	unique := map[string]struct{}{"UTC": {}}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		columns := strings.Split(line, "\t")
		if len(columns) < 3 {
			continue
		}
		identifier := strings.TrimSpace(columns[2])
		if identifier == "" || strings.ContainsAny(identifier, " \t") {
			continue
		}
		unique[identifier] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	timezones := make([]string, 0, len(unique))
	for identifier := range unique {
		timezones = append(timezones, identifier)
	}
	sort.Strings(timezones)
	return timezones, nil
}
