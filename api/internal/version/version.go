package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed Semantic Version. The leading v is accepted for Git tags
// but is not part of the SemVer value.
type Version struct {
	Major      int64
	Minor      int64
	Patch      int64
	PreRelease []Identifier
}

type Identifier struct {
	Value   string
	Numeric bool
}

func Parse(value string) (Version, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "v") {
		value = value[1:]
	}
	if value == "" {
		return Version{}, fmt.Errorf("version is empty")
	}

	withoutBuild := value
	if buildIndex := strings.IndexByte(withoutBuild, '+'); buildIndex >= 0 {
		build := withoutBuild[buildIndex+1:]
		if err := validateIdentifiers(build); err != nil {
			return Version{}, fmt.Errorf("version %q has invalid build metadata: %w", value, err)
		}
		withoutBuild = withoutBuild[:buildIndex]
	}
	parts := strings.SplitN(withoutBuild, "-", 2)
	core := strings.Split(parts[0], ".")
	if len(core) != 3 {
		return Version{}, fmt.Errorf("version %q is not x.y.z", value)
	}

	parsed := Version{}
	for index, component := range core {
		if component == "" || (len(component) > 1 && component[0] == '0') {
			return Version{}, fmt.Errorf("version %q has an invalid numeric component", value)
		}
		number, err := strconv.ParseInt(component, 10, 64)
		if err != nil || number < 0 {
			return Version{}, fmt.Errorf("version %q has an invalid numeric component", value)
		}
		switch index {
		case 0:
			parsed.Major = number
		case 1:
			parsed.Minor = number
		case 2:
			parsed.Patch = number
		}
	}

	if len(parts) == 2 {
		if err := parseIdentifiers(parts[1], &parsed.PreRelease); err != nil {
			return Version{}, fmt.Errorf("version %q has invalid prerelease: %w", value, err)
		}
	}
	if strings.Contains(value, " ") {
		return Version{}, fmt.Errorf("version %q contains whitespace", value)
	}
	return parsed, nil
}

func IsStable(value string) bool {
	parsed, err := Parse(value)
	return err == nil && len(parsed.PreRelease) == 0
}

func Compare(left Version, right Version) int {
	if left.Major != right.Major {
		return compareInt(left.Major, right.Major)
	}
	if left.Minor != right.Minor {
		return compareInt(left.Minor, right.Minor)
	}
	if left.Patch != right.Patch {
		return compareInt(left.Patch, right.Patch)
	}
	return comparePreRelease(left.PreRelease, right.PreRelease)
}

func NormalizeStable(value string) (string, error) {
	parsed, err := Parse(value)
	if err != nil || len(parsed.PreRelease) != 0 {
		if err == nil {
			err = fmt.Errorf("version %q is not stable", value)
		}
		return "", err
	}
	return fmt.Sprintf("v%d.%d.%d", parsed.Major, parsed.Minor, parsed.Patch), nil
}

func parseIdentifiers(value string, output *[]Identifier) error {
	if err := validateIdentifiers(value); err != nil {
		return err
	}
	for _, raw := range strings.Split(value, ".") {
		numeric := true
		if len(raw) > 1 && raw[0] == '0' {
			return fmt.Errorf("numeric identifier has leading zero")
		}
		for _, char := range raw {
			if char < '0' || char > '9' {
				numeric = false
				break
			}
		}
		*output = append(*output, Identifier{Value: raw, Numeric: numeric})
	}
	return nil
}

func validateIdentifiers(value string) error {
	if value == "" {
		return fmt.Errorf("empty identifier")
	}
	for _, raw := range strings.Split(value, ".") {
		if raw == "" {
			return fmt.Errorf("empty identifier")
		}
		for _, char := range raw {
			if !((char >= '0' && char <= '9') || (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char == '-') {
				return fmt.Errorf("invalid character")
			}
		}
	}
	return nil
}

func compareInt(left int64, right int64) int {
	if left < right {
		return -1
	}
	return 1
}

func comparePreRelease(left []Identifier, right []Identifier) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}
	for index := 0; index < len(left) && index < len(right); index++ {
		leftIdentifier := left[index]
		rightIdentifier := right[index]
		if leftIdentifier.Numeric && rightIdentifier.Numeric {
			if len(leftIdentifier.Value) != len(rightIdentifier.Value) {
				return compareIntValue(len(leftIdentifier.Value), len(rightIdentifier.Value))
			}
			if leftIdentifier.Value != rightIdentifier.Value {
				if leftIdentifier.Value < rightIdentifier.Value {
					return -1
				}
				return 1
			}
			continue
		}
		if leftIdentifier.Numeric != rightIdentifier.Numeric {
			if leftIdentifier.Numeric {
				return -1
			}
			return 1
		}
		if leftIdentifier.Value != rightIdentifier.Value {
			if leftIdentifier.Value < rightIdentifier.Value {
				return -1
			}
			return 1
		}
	}
	return compareIntValue(len(left), len(right))
}

func compareIntValue(left int, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
