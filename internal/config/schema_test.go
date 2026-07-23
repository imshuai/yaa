package config

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ConfigSchema
	}{
		{name: "current", raw: "1.0", want: ConfigSchema{Major: 1, Minor: 0}},
		{name: "zero", raw: "0.0", want: ConfigSchema{Major: 0, Minor: 0}},
		{name: "large", raw: "12.34", want: ConfigSchema{Major: 12, Minor: 34}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersion(tt.raw)
			if err != nil {
				t.Fatalf("ParseVersion(%q) returned error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseVersion(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseVersionRejectsMalformedInput(t *testing.T) {
	for _, raw := range []string{
		"",
		"1",
		"1.",
		".0",
		"1.0.1",
		"-1.0",
		"1.-1",
		"1.x",
		" 1.0",
		"1.0 ",
		"999999999999999999999999.0",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseVersion(raw); err == nil {
				t.Fatalf("ParseVersion(%q) unexpectedly succeeded", raw)
			}
		})
	}
}

func TestConfigSchemaCompareAndString(t *testing.T) {
	tests := []struct {
		left  ConfigSchema
		right ConfigSchema
		cmp   int
		text  string
	}{
		{left: ConfigSchema{Major: 1, Minor: 0}, right: ConfigSchema{Major: 1, Minor: 0}, cmp: 0, text: "1.0"},
		{left: ConfigSchema{Major: 1, Minor: 0}, right: ConfigSchema{Major: 1, Minor: 1}, cmp: -1, text: "1.0"},
		{left: ConfigSchema{Major: 2, Minor: 0}, right: ConfigSchema{Major: 1, Minor: 9}, cmp: 1, text: "2.0"},
	}

	for _, tt := range tests {
		if got := tt.left.Compare(tt.right); got != tt.cmp {
			t.Errorf("%v.Compare(%v) = %d, want %d", tt.left, tt.right, got, tt.cmp)
		}
		if got := tt.left.String(); got != tt.text {
			t.Errorf("%v.String() = %q, want %q", tt.left, got, tt.text)
		}
	}
}
