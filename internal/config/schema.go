package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ConfigSchema identifies a configuration schema version.
type ConfigSchema struct {
	Major int
	Minor int
}

// CurrentSchemaVersion is the schema understood by this runtime.
var CurrentSchemaVersion = ConfigSchema{Major: 1, Minor: 0}

// ParseVersion parses the strict major.minor configuration version syntax.
func ParseVersion(raw string) (ConfigSchema, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return ConfigSchema{}, fmt.Errorf("invalid config_version %q", raw)
	}

	major, ok := parseVersionPart(parts[0])
	if !ok {
		return ConfigSchema{}, fmt.Errorf("invalid config_version %q", raw)
	}
	minor, ok := parseVersionPart(parts[1])
	if !ok {
		return ConfigSchema{}, fmt.Errorf("invalid config_version %q", raw)
	}
	return ConfigSchema{Major: major, Minor: minor}, nil
}

func parseVersionPart(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil
}

// Compare returns -1 when v is older, 0 when equal, and 1 when newer.
func (v ConfigSchema) Compare(other ConfigSchema) int {
	if v.Major < other.Major {
		return -1
	}
	if v.Major > other.Major {
		return 1
	}
	if v.Minor < other.Minor {
		return -1
	}
	if v.Minor > other.Minor {
		return 1
	}
	return 0
}

func (v ConfigSchema) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}
