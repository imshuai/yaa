package plugin

import "testing"

func TestParseVersionRange(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantN   int
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"single eq", "1.2.0", 1, false},
		{"gte", ">=1.0.0", 1, false},
		{"range", ">=0.1.0 <1.0.0", 2, false},
		{"bad version", ">=abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, err := parseVersionRange(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && len(cs) != tt.wantN {
				t.Fatalf("got %d constraints, want %d", len(cs), tt.wantN)
			}
		})
	}
}

func TestVersionInRange(t *testing.T) {
	tests := []struct {
		name string
		ver  string
		rng  string
		want bool
	}{
		{"no constraint", "0.5.0", "", true},
		{"exact match", "1.2.0", "1.2.0", true},
		{"exact mismatch", "1.3.0", "1.2.0", false},
		{"gte pass", "1.5.0", ">=1.0.0", true},
		{"gte fail", "0.5.0", ">=1.0.0", false},
		{"range in", "0.5.0", ">=0.1.0 <1.0.0", true},
		{"range lower bound", "0.1.0", ">=0.1.0 <1.0.0", true},
		{"range upper", "1.0.0", ">=0.1.0 <1.0.0", false},
		{"range above", "2.0.0", ">=0.1.0 <1.0.0", false},
		{"range below", "0.0.1", ">=0.1.0 <1.0.0", false},
		{"lte", "0.9.0", "<=1.0.0", true},
		{"gt", "1.0.1", ">1.0.0", true},
		{"gt equal", "1.0.0", ">1.0.0", false},
		{"lt", "0.9.0", "<1.0.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, err := parseVersionRange(tt.rng)
			if err != nil {
				t.Fatal(err)
			}
			got := versionInRange(tt.ver, cs)
			if got != tt.want {
				t.Fatalf("versionInRange(%q, %q) = %v, want %v", tt.ver, tt.rng, got, tt.want)
			}
		})
	}
}

func TestValidateRuntimeVersion(t *testing.T) {
	m := Manifest{ID: "test", RequiresRuntime: ">=0.1.0 <1.0.0"}
	// 兼容
	if err := ValidateRuntimeVersion(m, "0.5.0"); err != nil {
		t.Fatalf("expected pass: %v", err)
	}
	// 不兼容超过上限
	if err := ValidateRuntimeVersion(m, "1.5.0"); err == nil {
		t.Fatal("expected incompatibility")
	}
	// 无约束
	noConstraint := Manifest{ID: "x"}
	if err := ValidateRuntimeVersion(noConstraint, "99.0.0"); err != nil {
		t.Fatalf("expected pass for empty constraint: %v", err)
	}
}

func TestValidateDependencies(t *testing.T) {
	// 有效 range
	m := Manifest{ID: "x", Dependencies: []Dependency{
		{ID: "dep", Version: ">=1.0.0 <2.0.0"},
	}}
	if err := ValidateDependencies(m); err != nil {
		t.Fatalf("valid deps rejected: %v", err)
	}
	// 无效 range
	m2 := Manifest{ID: "x", Dependencies: []Dependency{
		{ID: "dep", Version: "not-a-version"},
	}}
	if err := ValidateDependencies(m2); err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestCanonicalSemVer(t *testing.T) {
	if got := canonicalSemVer("1.0.0"); got != "v1.0.0" {
		t.Fatalf("expected v1.0.0, got %s", got)
	}
	if got := canonicalSemVer("v2.0.0"); got != "v2.0.0" {
		t.Fatalf("expected v2.0.0, got %s", got)
	}
}
