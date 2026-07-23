package config

import (
	"reflect"
	"testing"
	"time"
)

func TestResolveSessionPolicyAppliesOverridesInOrder(t *testing.T) {
	root := DefaultSessionConfig()
	agentTTL := 2 * time.Hour
	agentPersist := false
	createTTL := time.Duration(0)
	createMessages := 25

	got := ResolveSessionPolicy(root,
		&SessionOverride{TTL: &agentTTL, Persist: &agentPersist},
		&SessionOverride{TTL: &createTTL, MaxMessages: &createMessages},
	)
	want := SessionPolicy{
		MaxMessages:     25,
		MaxMessageBytes: root.MaxMessageBytes,
		TTL:             0,
		MaxLifetime:     root.MaxLifetime,
		Persist:         false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveSessionPolicy = %#v, want %#v", got, want)
	}
}

func TestResolveMemoryPolicyAppliesNestedOverride(t *testing.T) {
	root := DefaultMemoryConfig()
	enabled := false
	maxItems := 0
	vectorEnabled := true
	topK := 3

	got := ResolveMemoryPolicy(root, &MemoryOverride{
		Enabled:  &enabled,
		MaxItems: &maxItems,
		Vector: &MemoryVectorOverride{
			Enabled: &vectorEnabled,
			TopK:    &topK,
		},
	})
	want := MemoryPolicy{
		Enabled:        false,
		MaxItems:       0,
		DefaultTTL:     root.DefaultTTL,
		EvictionPolicy: root.EvictionPolicy,
		Vector:         root.Vector,
	}
	want.Vector.Enabled = true
	want.Vector.TopK = 3
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveMemoryPolicy = %#v, want %#v", got, want)
	}
}

func TestResolveContextConfigAppliesNestedOverride(t *testing.T) {
	root := DefaultContextConfig()
	maxTokens := 0
	compressionEnabled := false
	targetRatio := 0.25

	got := ResolveContextConfig(root, &ContextOverride{
		MaxTokens: &maxTokens,
		Compression: &ContextCompressionOverride{
			Enabled:     &compressionEnabled,
			TargetRatio: &targetRatio,
		},
	})
	want := root
	want.MaxTokens = 0
	want.Compression.Enabled = false
	want.Compression.TargetRatio = 0.25
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveContextConfig = %#v, want %#v", got, want)
	}
}
