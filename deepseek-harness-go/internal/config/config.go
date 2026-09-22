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
	LLM    LLMConfig    `yaml:"llm"    json:"llm"`
	Agent  AgentConfig  `yaml:"agent"  json:"agent"`
	Server ServerConfig `yaml:"server" json:"server"`
}

// ServerConfig 保存 HTTP 服务器设置。
type ServerConfig struct {
	Enabled     bool          `yaml:"enabled"            env:"DSH_SERVER_ENABLED"`
	Listen      string        `yaml:"listen"             env:"DSH_SERVER_LISTEN"`
	AuthToken   string        `yaml:"auth-token"         env:"DSH_SERVER_AUTH_TOKEN"`
	Timeout     time.Duration `yaml:"request-timeout"    env:"DSH_SERVER_TIMEOUT"`
	MaxSessions int           `yaml:"max-concurrent-sessions" env:"DSH_SERVER_MAX_SESSIONS"`
}

// LLMConfig 保存 OpenAI 兼容的上游设置。
type LLMConfig struct {
	BaseURL   string        `yaml:"base-url"   env:"DEEPSEEK_BASE_URL"`
	APIKey    string        `yaml:"api-key"    env:"DEEPSEEK_API_KEY"`
	Model     string        `yaml:"model"      env:"DEEPSEEK_DEFAULT_MODEL"`
	MaxTokens int           `yaml:"max-tokens" env:"DEEPSEEK_MAX_TOKENS"`
	Timeout   time.Duration `yaml:"timeout"    env:"DEEPSEEK_TIMEOUT"`
}

// AgentConfig 保存 agent runner 的循环与工作区设置。
type AgentConfig struct {
	MaxRounds     int      `yaml:"max-rounds"     env:"DSH_AGENT_MAX_ROUNDS"`
	WorkspaceRoot string   `yaml:"workspace"      env:"DSH_AGENT_WORKSPACE"`
	Temperature   *float64 `yaml:"temperature"    env:"DSH_AGENT_TEMPERATURE"`
	SystemPrompt  string   `yaml:"system-prompt"  env:"DSH_AGENT_SYSTEM_PROMPT"`
	Debug         bool     `yaml:"debug"          env:"DSH_AGENT_DEBUG"`
}

// 当 YAML 和环境变量都未提供值时应用的默认值。
func defaults() Config {
	temp := 0.2
	maxTokens := 8192
	return Config{
		LLM: LLMConfig{
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
