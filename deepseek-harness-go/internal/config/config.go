// Package config 定义配置结构以及 YAML+环境变量的加载管线。
//
// 加载顺序（后者优先）为：
//
//	默认值  →  YAML 文件（若存在）  →  环境变量
//
// 校验最后执行；无效的 Config 会附带描述性错误被拒绝。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是 cmd/dsh 使用的顶层配置。
type Config struct {
	LLM        LLMConfig        `yaml:"llm"        json:"llm"`
	Agent      AgentConfig      `yaml:"agent"      json:"agent"`
	Server     ServerConfig     `yaml:"server"     json:"server"`
	Obs        ObsConfig        `yaml:"obs"        json:"obs"`
	Skills     SkillsConfig     `yaml:"skills"     json:"skills"`
	Compaction CompactionConfig `yaml:"compaction" json:"compaction"`
	Audit      AuditConfig      `yaml:"audit"      json:"audit"`
	Sandbox    SandboxConfig    `yaml:"sandbox"    json:"sandbox"`
	Shell      ShellConfig      `yaml:"shell"      json:"shell"`
	Channels   []ChannelConfig  `yaml:"channels"   json:"channels"` // v5 P5-3
	Approval   ApprovalConfig   `yaml:"approval"   json:"approval"` // v5 P5-5
	Hooks      HooksConfig      `yaml:"hooks"      json:"hooks"`    // v5 P5-6
	Credentials CredentialsConfig `yaml:"credentials" json:"credentials"` // v5 P5-4
}

// ServerConfig 保存 HTTP 服务器设置。
type ServerConfig struct {
	Enabled     bool          `yaml:"enabled"            env:"DSH_SERVER_ENABLED"`
	Listen      string        `yaml:"listen"             env:"DSH_SERVER_LISTEN"`
	AuthToken   string        `yaml:"auth-token"         env:"DSH_SERVER_AUTH_TOKEN"`
	Timeout     time.Duration `yaml:"request-timeout"    env:"DSH_SERVER_TIMEOUT"`
	MaxSessions int           `yaml:"max-concurrent-sessions" env:"DSH_SERVER_MAX_SESSIONS"`
}

// LLMConfig 保存上游 LLM 的连接设置。Provider 决定协议：
// openai（默认，Chat Completions 兼容）| anthropic | ollama | gemini。
type LLMConfig struct {
	Provider string        `yaml:"provider"   env:"DSH_LLM_PROVIDER"`
	BaseURL  string        `yaml:"base-url"   env:"DEEPSEEK_BASE_URL"`
	APIKey   string        `yaml:"api-key"    env:"DEEPSEEK_API_KEY"`
	Model    string        `yaml:"model"      env:"DEEPSEEK_DEFAULT_MODEL"`
	MaxTokens int          `yaml:"max-tokens" env:"DEEPSEEK_MAX_TOKENS"`
	Timeout  time.Duration `yaml:"timeout"    env:"DEEPSEEK_TIMEOUT"`
}

// ObsConfig 控制 internal/obs 的实现选择（DESIGN-v3 §C.4）。
type ObsConfig struct {
	Provider string  `yaml:"provider"     env:"DSH_OBS_PROVIDER"` // noop | otel
	Endpoint string  `yaml:"endpoint"     env:"DSH_OBS_OTEL_ENDPOINT"`
	ServiceName string `yaml:"service-name" env:"DSH_OBS_OTEL_SERVICE_NAME"`
	SampleRatio float64 `yaml:"sample-ratio" env:"DSH_OBS_OTEL_SAMPLE_RATIO"`
}

// SkillsConfig 是 skill 系统（DESIGN-v3 §A）的配置。
type SkillsConfig struct {
	Dir string `yaml:"dir" env:"DSH_SKILLS_DIR"` // 默认 ~/.dsh/skills
}

// CompactionConfig 是上下文压缩（DESIGN-v3 §D.4）的配置。
type CompactionConfig struct {
	Enabled       bool   `yaml:"enabled"        env:"DSH_COMPACTION_ENABLED"`
	TriggerTokens int    `yaml:"trigger-tokens" env:"DSH_COMPACTION_TRIGGER_TOKENS"`
	Strategy      string `yaml:"strategy"       env:"DSH_COMPACTION_STRATEGY"` // truncate | llm-summary
	KeepRecent    int    `yaml:"keep-recent"    env:"DSH_COMPACTION_KEEP_RECENT"`
}

// AuditConfig 是审计日志（DESIGN-v3 §E）的配置。
type AuditConfig struct {
	Path  string `yaml:"path"  env:"DSH_AUDIT_PATH"`  // 空表示关闭
	Full  bool   `yaml:"full"  env:"DSH_AUDIT_FULL"`  // true 记录 args 原文
}

// SandboxConfig 是 OS sandbox（DESIGN-v3 §F）的配置。
type SandboxConfig struct {
	Provider string `yaml:"provider" env:"DSH_SANDBOX_PROVIDER"` // noop | windows_acl | linux_ns | auto
}

// ShellConfig 是 shell 工具的装配配置。
type ShellConfig struct {
	Enabled   bool          `yaml:"enabled"    env:"DSH_SHELL_ENABLED"`
	AllowList []string      `yaml:"allowlist"  env:"DSH_SHELL_ALLOWLIST"` // 环境变量为逗号分隔
	Timeout   time.Duration `yaml:"timeout"    env:"DSH_SHELL_TIMEOUT"`
}

// AgentConfig 保存 agent runner 的循环与工作区设置。
type AgentConfig struct {
	MaxRounds     int      `yaml:"max-rounds"     env:"DSH_AGENT_MAX_ROUNDS"`
	WorkspaceRoot string   `yaml:"workspace"      env:"DSH_AGENT_WORKSPACE"`
	Temperature   *float64 `yaml:"temperature"    env:"DSH_AGENT_TEMPERATURE"`
	SystemPrompt  string   `yaml:"system-prompt"  env:"DSH_AGENT_SYSTEM_PROMPT"`
	Debug         bool     `yaml:"debug"          env:"DSH_AGENT_DEBUG"`
	// Profile 是 v5 P5-5 引入的审批矩阵 profile 选择；空 = default。
	Profile string `yaml:"profile" env:"DSH_AGENT_PROFILE"`
}

// ChannelConfig 是 v5 P5-3 引入的多渠道配置项（独立于顶层 llm 配置）。
type ChannelConfig struct {
	Code      string `yaml:"code"        env:"DSH_CHANNEL_CODE"`
	Provider  string `yaml:"provider"    env:"DSH_CHANNEL_PROVIDER"`
	BaseURL   string `yaml:"base-url"    env:"DSH_CHANNEL_BASE_URL"`
	APIKey    string `yaml:"api-key"     env:"DSH_CHANNEL_API_KEY"`
	Model     string `yaml:"model"       env:"DSH_CHANNEL_MODEL"`
	MaxTokens int    `yaml:"max-tokens"  env:"DSH_CHANNEL_MAX_TOKENS"`
	Timeout   string `yaml:"timeout"     env:"DSH_CHANNEL_TIMEOUT"` // duration string
}

// ApprovalConfig 是 v5 P5-5 引入的审批矩阵配置。
type ApprovalConfig struct {
	MatrixPath string `yaml:"matrix-path" env:"DSH_APPROVAL_MATRIX_PATH"` // 可选；空用 inline Matrix
}

// HooksConfig 是 v5 P5-6 引入的钩子配置。
type HooksConfig struct {
	// PreToolUse / PostToolUse 是钩子命令列表（每行一个 shell 命令）。
	PreToolUse  []string `yaml:"pre-tool-use"  env:"DSH_HOOKS_PRE"`
	PostToolUse []string `yaml:"post-tool-use" env:"DSH_HOOKS_POST"`
}

// CredentialsConfig 是 v5 P5-4 引入的凭据配置。
type CredentialsConfig struct {
	// File 是 JSON 文件路径（空 = 仅 env）。
	File string `yaml:"file" env:"DSH_CREDENTIALS_FILE"`
}

// 当 YAML 和环境变量都未提供值时应用的默认值。
func defaults() Config {
	temp := 0.2
	maxTokens := 8192
	return Config{
		LLM: LLMConfig{
			Provider:  "openai",
			BaseURL:   "http://127.0.0.1:8777/v1",
			Model:     "glm-5.3-flash",
			MaxTokens: maxTokens,
			Timeout:   120 * time.Second,
		},
		Agent: AgentConfig{
			MaxRounds:     16,
			WorkspaceRoot: "./.dsh/workspace",
			Temperature:   &temp,
			Debug:         false,
		},
		Server: ServerConfig{
			Enabled:     false,
			Listen:      "127.0.0.1:8080",
			AuthToken:   "",
			Timeout:     0,
			MaxSessions: 16,
		},
		Obs: ObsConfig{
			Provider:    "noop",
			Endpoint:    "localhost:4317",
			ServiceName: "dsh",
			SampleRatio: 1.0,
		},
		Skills: SkillsConfig{
			Dir: "", // 空表示 ~/.dsh/skills
		},
		Compaction: CompactionConfig{
			Enabled:       false,
			TriggerTokens: 16000,
			Strategy:      "truncate",
			KeepRecent:    6,
		},
		Audit: AuditConfig{
			Path: "",
			Full: false,
		},
		Sandbox: SandboxConfig{
			Provider: "noop",
		},
	}
}

// Load 读取 path 处的 YAML 文件（若存在），与默认值合并，应用非空的
// 环境变量，并校验结果。
//
// 路径不存在不算错误；直接跳过该文件。
func Load(path string) (Config, error) {
	cfg := defaults()

	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("config: read %s: %w", path, err)
		}
	}

	problems := overrideEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	if len(problems) > 0 {
		return Config{}, fmt.Errorf("config: invalid environment overrides:\n  - %s",
			strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// overrideEnv 将非空的环境变量应用到 cfg。
//
// 类型转换失败时（例如 DEEPSEEK_MAX_TOKENS="abc"），记录一条描述性
// 问题并保留原值（默认值或 YAML 中的值）。收集到的问题会被返回；
// Load 将它们转换为致命错误，让用户察觉而不是默默继承陈旧值。
func overrideEnv(cfg *Config) []string {
	var problems []string

	if v := os.Getenv("DSH_LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("DEEPSEEK_DEFAULT_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("DEEPSEEK_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.LLM.MaxTokens = n
		} else {
			problems = append(problems, fmt.Sprintf("DEEPSEEK_MAX_TOKENS=%q is not an integer: %v", v, err))
		}
	}
	if v := os.Getenv("DEEPSEEK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.LLM.Timeout = d
		} else {
			problems = append(problems, fmt.Sprintf("DEEPSEEK_TIMEOUT=%q is not a duration: %v", v, err))
		}
	}

	if v := os.Getenv("DSH_AGENT_MAX_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Agent.MaxRounds = n
		} else {
			problems = append(problems, fmt.Sprintf("DSH_AGENT_MAX_ROUNDS=%q is not an integer: %v", v, err))
		}
	}
	if v := os.Getenv("DSH_AGENT_WORKSPACE"); v != "" {
		cfg.Agent.WorkspaceRoot = v
	}
	if v := os.Getenv("DSH_AGENT_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Agent.Temperature = &f
		} else {
			problems = append(problems, fmt.Sprintf("DSH_AGENT_TEMPERATURE=%q is not a float: %v", v, err))
		}
	}
	if v := os.Getenv("DSH_AGENT_SYSTEM_PROMPT"); v != "" {
		cfg.Agent.SystemPrompt = v
	}
	if v := os.Getenv("DSH_AGENT_DEBUG"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Agent.Debug = b
		} else {
			problems = append(problems, fmt.Sprintf("DSH_AGENT_DEBUG=%q is not a bool: %v", v, err))
		}
	}

	if v := os.Getenv("DSH_OBS_PROVIDER"); v != "" {
		cfg.Obs.Provider = v
	}
	if v := os.Getenv("DSH_OBS_OTEL_ENDPOINT"); v != "" {
		cfg.Obs.Endpoint = v
	}
	if v := os.Getenv("DSH_OBS_OTEL_SERVICE_NAME"); v != "" {
		cfg.Obs.ServiceName = v
	}
	if v := os.Getenv("DSH_OBS_OTEL_SAMPLE_RATIO"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Obs.SampleRatio = f
		} else {
			problems = append(problems, fmt.Sprintf("DSH_OBS_OTEL_SAMPLE_RATIO=%q is not a float: %v", v, err))
		}
	}

	if v := os.Getenv("DSH_SKILLS_DIR"); v != "" {
		cfg.Skills.Dir = v
	}

	if v := os.Getenv("DSH_COMPACTION_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Compaction.Enabled = b
		} else {
			problems = append(problems, fmt.Sprintf("DSH_COMPACTION_ENABLED=%q is not a bool: %v", v, err))
		}
	}
	if v := os.Getenv("DSH_COMPACTION_TRIGGER_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Compaction.TriggerTokens = n
		} else {
			problems = append(problems, fmt.Sprintf("DSH_COMPACTION_TRIGGER_TOKENS=%q is not an integer: %v", v, err))
		}
	}
	if v := os.Getenv("DSH_COMPACTION_STRATEGY"); v != "" {
		cfg.Compaction.Strategy = v
	}
	if v := os.Getenv("DSH_COMPACTION_KEEP_RECENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Compaction.KeepRecent = n
		} else {
			problems = append(problems, fmt.Sprintf("DSH_COMPACTION_KEEP_RECENT=%q is not an integer: %v", v, err))
		}
	}

	if v := os.Getenv("DSH_AUDIT_PATH"); v != "" {
		cfg.Audit.Path = v
	}
	if v := os.Getenv("DSH_AUDIT_FULL"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Audit.Full = b
		} else {
			problems = append(problems, fmt.Sprintf("DSH_AUDIT_FULL=%q is not a bool: %v", v, err))
		}
	}

	if v := os.Getenv("DSH_SANDBOX_PROVIDER"); v != "" {
		cfg.Sandbox.Provider = v
	}

	if v := os.Getenv("DSH_SHELL_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Shell.Enabled = b
		} else {
			problems = append(problems, fmt.Sprintf("DSH_SHELL_ENABLED=%q is not a bool: %v", v, err))
		}
	}
	if v := os.Getenv("DSH_SHELL_ALLOWLIST"); v != "" {
		var items []string
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				items = append(items, p)
			}
		}
		cfg.Shell.AllowList = items
	}
	if v := os.Getenv("DSH_SHELL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Shell.Timeout = d
		} else {
			problems = append(problems, fmt.Sprintf("DSH_SHELL_TIMEOUT=%q is not a duration: %v", v, err))
		}
	}

	if v := os.Getenv("DSH_SERVER_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Server.Enabled = b
		} else {
			problems = append(problems, fmt.Sprintf("DSH_SERVER_ENABLED=%q is not a bool: %v", v, err))
		}
	}
	if v := os.Getenv("DSH_SERVER_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}
	if v := os.Getenv("DSH_SERVER_AUTH_TOKEN"); v != "" {
		cfg.Server.AuthToken = v
	}
	if v := os.Getenv("DSH_SERVER_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.Timeout = d
		} else {
			problems = append(problems, fmt.Sprintf("DSH_SERVER_TIMEOUT=%q is not a duration: %v", v, err))
		}
	}
	if v := os.Getenv("DSH_SERVER_MAX_SESSIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.MaxSessions = n
		} else {
			problems = append(problems, fmt.Sprintf("DSH_SERVER_MAX_SESSIONS=%q is not an integer: %v", v, err))
		}
	}
	return problems
}

// Validate 强制执行程序其余部分所依赖的最小不变量。
func (c *Config) Validate() error {
	var problems []string

	switch c.LLM.Provider {
	case "", "openai", "anthropic", "ollama", "gemini":
	default:
		problems = append(problems, fmt.Sprintf("llm.provider %q not supported (supported: openai, anthropic, ollama, gemini)", c.LLM.Provider))
	}
	if c.LLM.BaseURL == "" {
		problems = append(problems, "llm.base-url is required (set in YAML or DEEPSEEK_BASE_URL)")
	} else {
		u, err := url.Parse(c.LLM.BaseURL)
		if err != nil {
			problems = append(problems, fmt.Sprintf("llm.base-url parse: %v", err))
		} else if u.Scheme != "http" && u.Scheme != "https" {
			problems = append(problems, fmt.Sprintf("llm.base-url scheme must be http(s), got %q", u.Scheme))
		}
	}
	if c.LLM.Model == "" {
		problems = append(problems, "llm.model is required")
	}
	if c.LLM.MaxTokens < 0 {
		problems = append(problems, "llm.max-tokens must be >= 0")
	}
	if c.Agent.MaxRounds < 1 || c.Agent.MaxRounds > 64 {
		problems = append(problems, fmt.Sprintf("agent.max-rounds must be in [1, 64], got %d", c.Agent.MaxRounds))
	}
	if strings.TrimSpace(c.Agent.WorkspaceRoot) == "" {
		problems = append(problems, "agent.workspace must not be empty")
	}
	switch c.Obs.Provider {
	case "", "noop", "otel":
	default:
		problems = append(problems, fmt.Sprintf("obs.provider %q not supported (supported: noop, otel)", c.Obs.Provider))
	}
	if c.Obs.SampleRatio < 0 || c.Obs.SampleRatio > 1 {
		problems = append(problems, fmt.Sprintf("obs.otel.sample-ratio must be in [0, 1], got %v", c.Obs.SampleRatio))
	}
	switch c.Compaction.Strategy {
	case "", "truncate", "llm-summary":
	default:
		problems = append(problems, fmt.Sprintf("compaction.strategy %q not supported (supported: truncate, llm-summary)", c.Compaction.Strategy))
	}
	if c.Compaction.TriggerTokens < 0 {
		problems = append(problems, fmt.Sprintf("compaction.trigger-tokens must be >= 0, got %d", c.Compaction.TriggerTokens))
	}
	if c.Compaction.KeepRecent < 0 {
		problems = append(problems, fmt.Sprintf("compaction.keep-recent must be >= 0, got %d", c.Compaction.KeepRecent))
	}
	switch c.Sandbox.Provider {
	case "", "noop", "windows_acl", "linux_ns", "auto":
	default:
		problems = append(problems, fmt.Sprintf("sandbox.provider %q not supported (supported: noop, windows_acl, linux_ns, auto)", c.Sandbox.Provider))
	}
	if c.Server.Enabled && strings.TrimSpace(c.Server.AuthToken) == "" {
		problems = append(problems, "server.enabled=true requires server.auth-token to be set")
	}
	if c.Server.Enabled && strings.TrimSpace(c.Server.Listen) == "" {
		problems = append(problems, "server.enabled=true requires server.listen to be set")
	}
	if c.Server.MaxSessions < 1 {
		problems = append(problems, fmt.Sprintf("server.max-concurrent-sessions must be >= 1, got %d", c.Server.MaxSessions))
	}

	if len(problems) > 0 {
		return fmt.Errorf("config: invalid:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}
