package config

import "time"

// SessionPolicy is the immutable session policy resolved for one session.
type SessionPolicy struct {
	MaxMessages     int
	MaxMessageBytes int
	TTL             time.Duration
	MaxLifetime     time.Duration
	Persist         bool
}

// ResolveSessionPolicy applies root, agent, then create overrides.
func ResolveSessionPolicy(root SessionConfig, agent, create *SessionOverride) SessionPolicy {
	out := SessionPolicy{
		MaxMessages:     root.MaxMessages,
		MaxMessageBytes: root.MaxMessageBytes,
		TTL:             root.TTL,
		MaxLifetime:     root.MaxLifetime,
		Persist:         root.Persist,
	}
	apply := func(override *SessionOverride) {
		if override == nil {
			return
		}
		if override.MaxMessages != nil {
			out.MaxMessages = *override.MaxMessages
		}
		if override.MaxMessageBytes != nil {
			out.MaxMessageBytes = *override.MaxMessageBytes
		}
		if override.TTL != nil {
			out.TTL = *override.TTL
		}
		if override.MaxLifetime != nil {
			out.MaxLifetime = *override.MaxLifetime
		}
		if override.Persist != nil {
			out.Persist = *override.Persist
		}
	}
	apply(agent)
	apply(create)
	return out
}

// MemoryPolicy is the agent-scoped memory policy resolved from root config.
type MemoryPolicy struct {
	Enabled        bool
	MaxItems       int
	DefaultTTL     time.Duration
	EvictionPolicy string
	Vector         MemoryVectorConfig
}

// ResolveMemoryPolicy applies an optional agent memory override.
func ResolveMemoryPolicy(root MemoryConfig, override *MemoryOverride) MemoryPolicy {
	out := MemoryPolicy{
		Enabled:        root.Enabled,
		MaxItems:       root.MaxItems,
		DefaultTTL:     root.DefaultTTL,
		EvictionPolicy: root.EvictionPolicy,
		Vector:         root.Vector,
	}
	if override == nil {
		return out
	}
	if override.Enabled != nil {
		out.Enabled = *override.Enabled
	}
	if override.MaxItems != nil {
		out.MaxItems = *override.MaxItems
	}
	if override.DefaultTTL != nil {
		out.DefaultTTL = *override.DefaultTTL
	}
	if override.EvictionPolicy != nil {
		out.EvictionPolicy = *override.EvictionPolicy
	}
	if vector := override.Vector; vector != nil {
		if vector.Enabled != nil {
			out.Vector.Enabled = *vector.Enabled
		}
		if vector.SimilarityThreshold != nil {
			out.Vector.SimilarityThreshold = *vector.SimilarityThreshold
		}
		if vector.TopK != nil {
			out.Vector.TopK = *vector.TopK
		}
		if vector.FallbackToKeyword != nil {
			out.Vector.FallbackToKeyword = *vector.FallbackToKeyword
		}
	}
	return out
}

// ResolveContextConfig applies an optional agent context override.
func ResolveContextConfig(root ContextConfig, override *ContextOverride) ContextConfig {
	out := root
	if override == nil {
		return out
	}
	if override.MaxTokens != nil {
		out.MaxTokens = *override.MaxTokens
	}
	if override.ReservedTokens != nil {
		out.ReservedTokens = *override.ReservedTokens
	}
	if override.Strategy != nil {
		out.Strategy = *override.Strategy
	}
	if compression := override.Compression; compression != nil {
		if compression.Enabled != nil {
			out.Compression.Enabled = *compression.Enabled
		}
		if compression.Threshold != nil {
			out.Compression.Threshold = *compression.Threshold
		}
		if compression.TargetRatio != nil {
			out.Compression.TargetRatio = *compression.TargetRatio
		}
		if compression.MinMessages != nil {
			out.Compression.MinMessages = *compression.MinMessages
		}
		if compression.PreserveRecent != nil {
			out.Compression.PreserveRecent = *compression.PreserveRecent
		}
		if compression.Timeout != nil {
			out.Compression.Timeout = *compression.Timeout
		}
	}
	return out
}

// ResolvePlannerConfig applies an optional agent planner override.
func ResolvePlannerConfig(root PlannerConfig, override *PlannerOverride) PlannerConfig {
	out := root
	if override == nil {
		return out
	}
	if override.Type != nil {
		out.Type = *override.Type
	}
	if override.Model != nil {
		out.Model = *override.Model
	}
	if override.Temperature != nil {
		value := *override.Temperature
		out.Temperature = &value
	}
	if override.MaxTokens != nil {
		out.MaxTokens = *override.MaxTokens
	}
	if override.MaxSteps != nil {
		out.MaxSteps = *override.MaxSteps
	}
	if override.MaxConcurrent != nil {
		out.MaxConcurrent = *override.MaxConcurrent
	}
	if override.Timeout != nil {
		out.Timeout = *override.Timeout
	}
	return out
}
