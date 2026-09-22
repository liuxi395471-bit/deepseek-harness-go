package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_DefaultsWhenNoFileAndNoEnv(t *testing.T) {
	// 清除可能污染测试的环境变量。
	t.Setenv("DEEPSEEK_BASE_URL", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("DEEPSEEK_DEFAULT_MODEL", "")
	t.Setenv("DEEPSEEK_MAX_TOKENS", "")
	t.Setenv("DEEPSEEK_TIMEOUT", "")
	t.Setenv("DSH_AGENT_MAX_ROUNDS", "")
	t.Setenv("DSH_AGENT_WORKSPACE", "")
	t.Setenv("DSH_AGENT_TEMPERATURE", "")
	t.Setenv("DSH_AGENT_SYSTEM_PROMPT", "")
	t.Setenv("DSH_AGENT_DEBUG", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.LLM.BaseURL != "http://127.0.0.1:8777/v1" {
		t.Errorf("BaseURL default = %q, want %q", cfg.LLM.BaseURL, "http://127.0.0.1:8777/v1")
	}
	if cfg.LLM.Model != "glm-5.3-flash" {
		t.Errorf("Model default = %q, want %q", cfg.LLM.Model, "glm-5.3-flash")
	}
	if cfg.Agent.MaxRounds != 16 {
		t.Errorf("MaxRounds default = %d, want 16", cfg.Agent.MaxRounds)
	}
	if cfg.Agent.WorkspaceRoot != "./.dsh/workspace" {
		t.Errorf("WorkspaceRoot default = %q", cfg.Agent.WorkspaceRoot)
	}
}

func TestLoad_YAMLOverridesDefaults(t *testing.T) {
	t.Setenv("DEEPSEEK_BASE_URL", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.yml")
	yaml := `llm:
  base-url: http://example.invalid/v1
  model: test-model
  max-tokens: 1024
agent:
  max-rounds: 8
  workspace: /tmp/dsh-test-ws
  debug: true
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.BaseURL != "http://example.invalid/v1" {
		t.Errorf("BaseURL = %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "test-model" {
		t.Errorf("Model = %q", cfg.LLM.Model)
	}
	if cfg.LLM.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d", cfg.LLM.MaxTokens)
	}
	if cfg.Agent.MaxRounds != 8 {
		t.Errorf("MaxRounds = %d", cfg.Agent.MaxRounds)
	}
	if cfg.Agent.WorkspaceRoot != "/tmp/dsh-test-ws" {
		t.Errorf("WorkspaceRoot = %q", cfg.Agent.WorkspaceRoot)
	}
	if !cfg.Agent.Debug {
		t.Errorf("Debug = false, want true")
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.yml")
	yaml := `llm:
  base-url: http://yaml.invalid/v1
  model: yaml-model
agent:
  max-rounds: 8
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_BASE_URL", "http://env.invalid/v1")
	t.Setenv("DEEPSEEK_DEFAULT_MODEL", "env-model")
	t.Setenv("DSH_AGENT_MAX_ROUNDS", "32")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.BaseURL != "http://env.invalid/v1" {
		t.Errorf("env should win YAML: BaseURL = %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "env-model" {
		t.Errorf("env should win YAML: Model = %q", cfg.LLM.Model)
	}
	if cfg.Agent.MaxRounds != 32 {
		t.Errorf("env should win YAML: MaxRounds = %d", cfg.Agent.MaxRounds)
	}
}

func TestLoad_ValidateRejectsBadValues(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"empty base-url", func(c *Config) { c.LLM.BaseURL = "" }},
		{"bad scheme", func(c *Config) { c.LLM.BaseURL = "ftp://x" }},
		{"negative max-rounds", func(c *Config) { c.Agent.MaxRounds = 0 }},
		{"max-rounds too big", func(c *Config) { c.Agent.MaxRounds = 999 }},
		{"empty workspace", func(c *Config) { c.Agent.WorkspaceRoot = "  " }},
		{"negative max-tokens", func(c *Config) { c.LLM.MaxTokens = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := defaults()
			tc.mut(&c)
			if err := c.Validate(); err == nil {
				t.Errorf("Validate() error = nil, want non-nil")
			}
		})
	}
}

func TestLoad_AcceptsMissingFile(t *testing.T) {
	t.Setenv("DEEPSEEK_BASE_URL", "")
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.LLM.BaseURL == "" {
		t.Errorf("expected defaults to apply, got empty BaseURL")
	}
}

func TestLoad_PartialYAMLMergesWithDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.yml")
	// 只覆盖 model；其余都应回落到默认值。
	yaml := `llm:
  model: partial-model
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_BASE_URL", "")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Model != "partial-model" {
		t.Errorf("Model = %q", cfg.LLM.Model)
	}
	if cfg.LLM.BaseURL != "http://127.0.0.1:8777/v1" {
		t.Errorf("partial YAML should keep default BaseURL, got %q", cfg.LLM.BaseURL)
	}
	if cfg.Agent.MaxRounds != 16 {
		t.Errorf("partial YAML should keep default MaxRounds, got %d", cfg.Agent.MaxRounds)
	}
}

func TestLoad_TimeoutParsedFromEnv(t *testing.T) {
	t.Setenv("DEEPSEEK_TIMEOUT", "30s")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.LLM.Timeout)
	}
}

// TestLoad_BadEnvValueFailsLoud 覆盖 R5：格式错误的环境变量值必须
// 以错误形式暴露，而不是被默默忽略。
func TestLoad_BadEnvValueFailsLoud(t *testing.T) {
	cases := []struct {
		envVar string
		badVal string
		want   string
	}{
		{"DEEPSEEK_MAX_TOKENS", "abc", "DEEPSEEK_MAX_TOKENS"},
		{"DEEPSEEK_TIMEOUT", "forever", "DEEPSEEK_TIMEOUT"},
		{"DSH_AGENT_MAX_ROUNDS", "many", "DSH_AGENT_MAX_ROUNDS"},
		{"DSH_AGENT_TEMPERATURE", "warm", "DSH_AGENT_TEMPERATURE"},
		{"DSH_AGENT_DEBUG", "maybe", "DSH_AGENT_DEBUG"},
	}
	for _, tc := range cases {
		t.Run(tc.envVar, func(t *testing.T) {
			t.Setenv(tc.envVar, tc.badVal)
			_, err := Load("")
			if err == nil {
				t.Fatalf("Load(%s=%q) error = nil, want non-nil", tc.envVar, tc.badVal)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want to mention %s", err, tc.want)
			}
		})
	}
}
