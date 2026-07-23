package config

import "time"

type Config struct {
	ConfigVersion string           `yaml:"config_version" json:"config_version"`
	Runtime       RuntimeConfig    `yaml:"runtime" json:"runtime"`
	Agents        []AgentConfig    `yaml:"agents" json:"agents"`
	Providers     []ProviderConfig `yaml:"providers" json:"providers"`
	MCP           MCPConfig        `yaml:"mcp" json:"mcp"`
	Tools         ToolsConfig      `yaml:"tools" json:"tools"`
	Skills        SkillsConfig     `yaml:"skills" json:"skills"`
	Memory        MemoryConfig     `yaml:"memory" json:"memory"`
	Session       SessionConfig    `yaml:"session" json:"session"`
	Context       ContextConfig    `yaml:"context" json:"context"`
	Planner       PlannerConfig    `yaml:"planner" json:"planner"`
	Plugins       PluginsConfig    `yaml:"plugins" json:"plugins"`
	Log           LogConfig        `yaml:"log" json:"log"`
}

type RuntimeConfig struct {
	Storage StorageConfig `yaml:"storage" json:"storage"`
	API     APIConfig     `yaml:"api" json:"api"`
	Auth    AuthConfig    `yaml:"auth" json:"auth"`
}

type StorageConfig struct {
	Type string `yaml:"type" json:"type"`
	Path string `yaml:"path" json:"path"`
}

type APIConfig struct {
	HTTP HTTPConfig `yaml:"http" json:"http"`
	WS   WSConfig   `yaml:"ws" json:"ws"`
	SSE  SSEConfig  `yaml:"sse" json:"sse"`
}

type HTTPConfig struct {
	Addr           string        `yaml:"addr" json:"addr"`
	ReadTimeout    time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout" json:"write_timeout"`
	MaxHeaderBytes int           `yaml:"max_header_bytes" json:"max_header_bytes"`
}

type WSConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type SSEConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type AuthConfig struct {
	Enabled     bool          `yaml:"enabled" json:"enabled"`
	TokenType   string        `yaml:"token_type" json:"token_type"`
	Tokens      []TokenConfig `yaml:"tokens" json:"tokens"`
	Roles       []RoleConfig  `yaml:"roles" json:"roles"`
	PublicPaths []string      `yaml:"public_paths" json:"public_paths"`
	JWT         JWTConfig     `yaml:"jwt" json:"jwt"`
}

type TokenConfig struct {
	Name  string   `yaml:"name" json:"name"`
	Token string   `yaml:"token" json:"token"`
	Roles []string `yaml:"roles" json:"roles"`
}

type RoleConfig struct {
	Name        string             `yaml:"name" json:"name"`
	Permissions []PermissionConfig `yaml:"permissions" json:"permissions"`
}

type PermissionConfig struct {
	Action   string `yaml:"action" json:"action"`
	Resource string `yaml:"resource" json:"resource"`
	Effect   string `yaml:"effect" json:"effect"`
}

type JWTConfig struct {
	Secret    string        `yaml:"secret" json:"secret"`
	Issuer    string        `yaml:"issuer" json:"issuer"`
	Audience  string        `yaml:"audience" json:"audience"`
	ClockSkew time.Duration `yaml:"clock_skew" json:"clock_skew"`
}

type AgentConfig struct {
	ID           string                      `yaml:"id" json:"id"`
	Name         string                      `yaml:"name" json:"name"`
	Provider     string                      `yaml:"provider" json:"provider"`
	Model        string                      `yaml:"model" json:"model"`
	SystemPrompt string                      `yaml:"system_prompt" json:"system_prompt"`
	Tools        []string                    `yaml:"tools" json:"tools"`
	Skills       []string                    `yaml:"skills" json:"skills"`
	MaxTokens    int                         `yaml:"max_tokens" json:"max_tokens"`
	Temperature  *float64                    `yaml:"temperature" json:"temperature"`
	Memory       *MemoryOverride             `yaml:"memory" json:"memory"`
	Session      *SessionOverride            `yaml:"session" json:"session"`
	Context      *ContextOverride            `yaml:"context" json:"context"`
	Planner      *PlannerOverride            `yaml:"planner" json:"planner"`
	ToolsConfig  map[string]any              `yaml:"tools_config" json:"tools_config"`
	SkillsConfig map[string]AgentSkillConfig `yaml:"skills_config" json:"skills_config"`
}

type AgentSkillConfig struct {
	Options map[string]any `yaml:"options" json:"options"`
}

type ProviderConfig struct {
	ID            string         `yaml:"id" json:"id"`
	Type          string         `yaml:"type" json:"type"`
	APIKey        string         `yaml:"api_key" json:"api_key"`
	BaseURL       string         `yaml:"base_url" json:"base_url"`
	Timeout       time.Duration  `yaml:"timeout" json:"timeout"`
	MaxRetries    int            `yaml:"max_retries" json:"max_retries"`
	RetryInterval time.Duration  `yaml:"retry_interval" json:"retry_interval"`
	Models        []ModelConfig  `yaml:"models" json:"models"`
	Extra         map[string]any `yaml:"extra" json:"extra"`
}

type ModelConfig struct {
	ID                string   `yaml:"id" json:"id"`
	Name              string   `yaml:"name" json:"name"`
	ContextWindow     int      `yaml:"context_window" json:"context_window"`
	MaxOutput         int      `yaml:"max_output" json:"max_output"`
	SupportsTools     bool     `yaml:"supports_tools" json:"supports_tools"`
	SupportsVision    bool     `yaml:"supports_vision" json:"supports_vision"`
	SupportsStreaming bool     `yaml:"supports_streaming" json:"supports_streaming"`
	SupportsThinking  bool     `yaml:"supports_thinking" json:"supports_thinking"`
	ThinkingEfforts   []string `yaml:"thinking_efforts" json:"thinking_efforts"`
	MinThinkingBudget int      `yaml:"min_thinking_budget" json:"min_thinking_budget"`
}

type MCPConfig struct {
	Servers   []MCPServerConfig  `yaml:"servers" json:"servers"`
	Server    MCPExposeConfig    `yaml:"server" json:"server"`
	Timeout   MCPTimeoutConfig   `yaml:"timeout" json:"timeout"`
	Reconnect MCPReconnectConfig `yaml:"reconnect" json:"reconnect"`
}

type MCPServerConfig struct {
	Name      string            `yaml:"name" json:"name"`
	Command   string            `yaml:"command" json:"command"`
	Args      []string          `yaml:"args" json:"args"`
	Env       map[string]string `yaml:"env" json:"env"`
	Headers   map[string]string `yaml:"headers" json:"headers"`
	TLS       MCPTLSConfig      `yaml:"tls" json:"tls"`
	Transport string            `yaml:"transport" json:"transport"`
	URL       string            `yaml:"url" json:"url"`
	Timeout   time.Duration     `yaml:"timeout" json:"timeout"`
	AutoStart bool              `yaml:"auto_start" json:"auto_start"`
}

type MCPTLSConfig struct {
	CAFile string `yaml:"ca_file" json:"ca_file"`
}

type MCPExposeConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	AgentID         string   `yaml:"agent_id" json:"agent_id"`
	Transport       string   `yaml:"transport" json:"transport"`
	Addr            string   `yaml:"addr" json:"addr"`
	Path            string   `yaml:"path" json:"path"`
	MessagesPath    string   `yaml:"messages_path" json:"messages_path"`
	ExposedTools    []string `yaml:"exposed_tools" json:"exposed_tools"`
	OriginAllowlist []string `yaml:"origin_allowlist" json:"origin_allowlist"`
}

type MCPTimeoutConfig struct {
	Connect time.Duration `yaml:"connect" json:"connect"`
	Init    time.Duration `yaml:"init" json:"init"`
	Tool    time.Duration `yaml:"tool" json:"tool"`
}

type MCPReconnectConfig struct {
	Enabled      bool          `yaml:"enabled" json:"enabled"`
	MaxAttempts  int           `yaml:"max_attempts" json:"max_attempts"`
	InitialDelay time.Duration `yaml:"initial_delay" json:"initial_delay"`
	MaxDelay     time.Duration `yaml:"max_delay" json:"max_delay"`
}

type ToolsConfig struct {
	DefaultTimeout          time.Duration         `yaml:"default_timeout" json:"default_timeout"`
	MaxTimeout              time.Duration         `yaml:"max_timeout" json:"max_timeout"`
	MaxConcurrent           int                   `yaml:"max_concurrent" json:"max_concurrent"`
	MaxConcurrentPerSession int                   `yaml:"max_concurrent_per_session" json:"max_concurrent_per_session"`
	DefaultMaxRetry         int                   `yaml:"default_max_retry" json:"default_max_retry"`
	MaxResultTokens         int                   `yaml:"max_result_tokens" json:"max_result_tokens"`
	Builtin                 map[string]ToolConfig `yaml:"builtin" json:"builtin"`
}

type ToolConfig struct {
	Enabled bool           `yaml:"enabled" json:"enabled"`
	Timeout time.Duration  `yaml:"timeout" json:"timeout"`
	Options map[string]any `yaml:"options" json:"options"`
}

type SkillsConfig struct {
	Dir      string                     `yaml:"dir" json:"dir"`
	PerSkill map[string]SkillItemConfig `yaml:"per_skill" json:"per_skill"`
}

type SkillItemConfig struct {
	Enabled bool           `yaml:"enabled" json:"enabled"`
	Options map[string]any `yaml:"options" json:"options"`
}

type MemoryConfig struct {
	Enabled         bool                  `yaml:"enabled" json:"enabled"`
	MaxItems        int                   `yaml:"max_items" json:"max_items"`
	DefaultTTL      time.Duration         `yaml:"default_ttl" json:"default_ttl"`
	ExpireInterval  time.Duration         `yaml:"expire_interval" json:"expire_interval"`
	ExpireBatchSize int                   `yaml:"expire_batch_size" json:"expire_batch_size"`
	EvictionPolicy  string                `yaml:"eviction_policy" json:"eviction_policy"`
	Storage         MemoryStorageConfig   `yaml:"storage" json:"storage"`
	Vector          MemoryVectorConfig    `yaml:"vector" json:"vector"`
	Embedding       MemoryEmbeddingConfig `yaml:"embedding" json:"embedding"`
}

type MemoryStorageConfig struct {
	Type string `yaml:"type" json:"type"`
	Path string `yaml:"path" json:"path"`
}

type MemoryVectorConfig struct {
	Enabled             bool    `yaml:"enabled" json:"enabled"`
	SimilarityThreshold float64 `yaml:"similarity_threshold" json:"similarity_threshold"`
	TopK                int     `yaml:"top_k" json:"top_k"`
	FallbackToKeyword   bool    `yaml:"fallback_to_keyword" json:"fallback_to_keyword"`
}

type MemoryEmbeddingConfig struct {
	Provider  string        `yaml:"provider" json:"provider"`
	Model     string        `yaml:"model" json:"model"`
	APIKey    string        `yaml:"api_key" json:"api_key"`
	BaseURL   string        `yaml:"base_url" json:"base_url"`
	Dimension int           `yaml:"dimension" json:"dimension"`
	Timeout   time.Duration `yaml:"timeout" json:"timeout"`
}

type MemoryOverride struct {
	Enabled        *bool                 `yaml:"enabled" json:"enabled"`
	MaxItems       *int                  `yaml:"max_items" json:"max_items"`
	DefaultTTL     *time.Duration        `yaml:"default_ttl" json:"default_ttl"`
	EvictionPolicy *string               `yaml:"eviction_policy" json:"eviction_policy"`
	Vector         *MemoryVectorOverride `yaml:"vector" json:"vector"`
}

type MemoryVectorOverride struct {
	Enabled             *bool    `yaml:"enabled" json:"enabled"`
	SimilarityThreshold *float64 `yaml:"similarity_threshold" json:"similarity_threshold"`
	TopK                *int     `yaml:"top_k" json:"top_k"`
	FallbackToKeyword   *bool    `yaml:"fallback_to_keyword" json:"fallback_to_keyword"`
}

type SessionConfig struct {
	MaxMessages         int           `yaml:"max_messages" json:"max_messages"`
	MaxMessageBytes     int           `yaml:"max_message_bytes" json:"max_message_bytes"`
	TTL                 time.Duration `yaml:"ttl" json:"ttl"`
	MaxLifetime         time.Duration `yaml:"max_lifetime" json:"max_lifetime"`
	Persist             bool          `yaml:"persist" json:"persist"`
	MaxSessionsPerAgent int           `yaml:"max_sessions_per_agent" json:"max_sessions_per_agent"`
	CleanupInterval     time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
}

type SessionOverride struct {
	MaxMessages     *int           `yaml:"max_messages" json:"max_messages"`
	MaxMessageBytes *int           `yaml:"max_message_bytes" json:"max_message_bytes"`
	TTL             *time.Duration `yaml:"ttl" json:"ttl"`
	MaxLifetime     *time.Duration `yaml:"max_lifetime" json:"max_lifetime"`
	Persist         *bool          `yaml:"persist" json:"persist"`
}

type ContextConfig struct {
	MaxTokens      int                      `yaml:"max_tokens" json:"max_tokens"`
	ReservedTokens int                      `yaml:"reserved_tokens" json:"reserved_tokens"`
	Strategy       string                   `yaml:"strategy" json:"strategy"`
	Compression    ContextCompressionConfig `yaml:"compression" json:"compression"`
}

type ContextCompressionConfig struct {
	Enabled        bool          `yaml:"enabled" json:"enabled"`
	Threshold      float64       `yaml:"threshold" json:"threshold"`
	TargetRatio    float64       `yaml:"target_ratio" json:"target_ratio"`
	MinMessages    int           `yaml:"min_messages" json:"min_messages"`
	PreserveRecent int           `yaml:"preserve_recent" json:"preserve_recent"`
	Timeout        time.Duration `yaml:"timeout" json:"timeout"`
}

type ContextOverride struct {
	MaxTokens      *int                        `yaml:"max_tokens" json:"max_tokens"`
	ReservedTokens *int                        `yaml:"reserved_tokens" json:"reserved_tokens"`
	Strategy       *string                     `yaml:"strategy" json:"strategy"`
	Compression    *ContextCompressionOverride `yaml:"compression" json:"compression"`
}

type ContextCompressionOverride struct {
	Enabled        *bool          `yaml:"enabled" json:"enabled"`
	Threshold      *float64       `yaml:"threshold" json:"threshold"`
	TargetRatio    *float64       `yaml:"target_ratio" json:"target_ratio"`
	MinMessages    *int           `yaml:"min_messages" json:"min_messages"`
	PreserveRecent *int           `yaml:"preserve_recent" json:"preserve_recent"`
	Timeout        *time.Duration `yaml:"timeout" json:"timeout"`
}

type PlannerConfig struct {
	Type          string        `yaml:"type" json:"type"`
	Model         string        `yaml:"model" json:"model"`
	Temperature   *float64      `yaml:"temperature" json:"temperature"`
	MaxTokens     int           `yaml:"max_tokens" json:"max_tokens"`
	MaxSteps      int           `yaml:"max_steps" json:"max_steps"`
	MaxConcurrent int           `yaml:"max_concurrent" json:"max_concurrent"`
	Timeout       time.Duration `yaml:"timeout" json:"timeout"`
}

type PlannerOverride struct {
	Type          *string        `yaml:"type" json:"type"`
	Model         *string        `yaml:"model" json:"model"`
	Temperature   *float64       `yaml:"temperature" json:"temperature"`
	MaxTokens     *int           `yaml:"max_tokens" json:"max_tokens"`
	MaxSteps      *int           `yaml:"max_steps" json:"max_steps"`
	MaxConcurrent *int           `yaml:"max_concurrent" json:"max_concurrent"`
	Timeout       *time.Duration `yaml:"timeout" json:"timeout"`
}

type PluginsConfig struct {
	Paths          []string      `yaml:"paths" json:"paths"`
	AutoStart      bool          `yaml:"auto_start" json:"auto_start"`
	StartupTimeout time.Duration `yaml:"startup_timeout" json:"startup_timeout"`
	StopTimeout    time.Duration `yaml:"stop_timeout" json:"stop_timeout"`
	HealthInterval time.Duration `yaml:"health_interval" json:"health_interval"`
	HealthTimeout  time.Duration `yaml:"health_timeout" json:"health_timeout"`
	Restart        RestartConfig `yaml:"restart" json:"restart"`
	Entries        []PluginEntry `yaml:"entries" json:"entries"`
}

type RestartConfig struct {
	Enabled     bool          `yaml:"enabled" json:"enabled"`
	MaxAttempts int           `yaml:"max_attempts" json:"max_attempts"`
	Backoff     time.Duration `yaml:"backoff" json:"backoff"`
}

type PluginEntry struct {
	ID      string         `yaml:"id" json:"id"`
	Enabled *bool          `yaml:"enabled" json:"enabled"`
	Config  map[string]any `yaml:"config" json:"config"`
}

type LogConfig struct {
	Level  string `yaml:"level" json:"level"`
	Format string `yaml:"format" json:"format"`
	Output string `yaml:"output" json:"output"`
}
