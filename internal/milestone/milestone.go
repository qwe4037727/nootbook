// Package milestone 读取 milestone.yaml，控制各里程碑功能与 Agent 的装配及默认权限。
// 未配置文件时使用内置默认值（M1–M7 可用）；任何 Agent 的默认权限都是 observe。
package milestone

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"novelagent/internal/agentruntime"
)

type agentSetting struct {
	Enabled *bool  `yaml:"enabled"`
	Level   string `yaml:"level"`
}

type m1Setting struct {
	Enabled *bool                   `yaml:"enabled"`
	Agents  map[string]agentSetting `yaml:"agents"`
}

type featureSetting struct {
	Enabled *bool `yaml:"enabled"`
}

type fileConfig struct {
	Milestones struct {
		M1 m1Setting      `yaml:"m1"`
		M2 featureSetting `yaml:"m2"`
		M3 featureSetting `yaml:"m3"`
		M4 featureSetting `yaml:"m4"`
		M5 m1Setting      `yaml:"m5"`
		M6 featureSetting `yaml:"m6"`
		M7 featureSetting `yaml:"m7"`
	} `yaml:"milestones"`
}

// Config 是运行时可消费的里程碑配置快照。
type Config struct {
	M1Enabled bool
	M2Enabled bool
	M3Enabled bool
	M4Enabled bool
	M5Enabled bool
	M6Enabled bool
	M7Enabled bool
	Defaults  map[string]agentruntime.AgentDefaults
	Source    string
}

// knownAgents 覆盖 M1 与 M5 全部插件；未在配置中出现的 Agent 按 observe 启用。
var knownAgents = []string{
	"continuity", "fuse", "memory",
	"plot", "style", "relation", "proofread", "task",
}

// Load 优先读取 dataDir/milestone.yaml；不存在或损坏时回退内置默认并保持 M1 可用。
func Load(dataDir string) Config {
	return load(dataDir, false)
}

func load(dataDir string, strict bool) Config {
	cfg := defaults()
	cfg.Source = "builtin"
	if strings.TrimSpace(dataDir) != "" {
		path := filepath.Join(dataDir, "milestone.yaml")
		if data, err := os.ReadFile(path); err == nil {
			var parsed fileConfig
			if err := yaml.Unmarshal(data, &parsed); err == nil {
				apply(&cfg, parsed)
				cfg.Source = path
			} else if strict {
				cfg.Source = "builtin:invalid:" + path
			}
		}
	}
	return cfg
}

func defaults() Config {
	cfg := Config{
		M1Enabled: true,
		M2Enabled: true,
		M3Enabled: true,
		M4Enabled: true,
		M5Enabled: true,
		M6Enabled: true,
		M7Enabled: true,
		Defaults:  make(map[string]agentruntime.AgentDefaults),
	}
	for _, key := range knownAgents {
		cfg.Defaults[key] = agentruntime.AgentDefaults{Enabled: true, Level: agentruntime.PermissionObserve}
	}
	return cfg
}

func apply(cfg *Config, parsed fileConfig) {
	if parsed.Milestones.M1.Enabled != nil {
		cfg.M1Enabled = *parsed.Milestones.M1.Enabled
	}
	if parsed.Milestones.M2.Enabled != nil {
		cfg.M2Enabled = *parsed.Milestones.M2.Enabled
	}
	if parsed.Milestones.M3.Enabled != nil {
		cfg.M3Enabled = *parsed.Milestones.M3.Enabled
	}
	if parsed.Milestones.M4.Enabled != nil {
		cfg.M4Enabled = *parsed.Milestones.M4.Enabled
	}
	if parsed.Milestones.M5.Enabled != nil {
		cfg.M5Enabled = *parsed.Milestones.M5.Enabled
	}
	if parsed.Milestones.M6.Enabled != nil {
		cfg.M6Enabled = *parsed.Milestones.M6.Enabled
	}
	if parsed.Milestones.M7.Enabled != nil {
		cfg.M7Enabled = *parsed.Milestones.M7.Enabled
	}
	applyAgentSettings(cfg, parsed.Milestones.M1.Agents)
	applyAgentSettings(cfg, parsed.Milestones.M5.Agents)
}

func applyAgentSettings(cfg *Config, settings map[string]agentSetting) {
	for key, setting := range settings {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		current := cfg.Defaults[key]
		if setting.Enabled != nil {
			current.Enabled = *setting.Enabled
		}
		if level := strings.TrimSpace(setting.Level); level != "" {
			current.Level = level
		}
		cfg.Defaults[key] = current
	}
}

// Validate 供启动自检/测试使用：非法级别按 observe 降级并返回说明。
func (c Config) Validate() []error {
	var errs []error
	for key, def := range c.Defaults {
		switch def.Level {
		case agentruntime.PermissionObserve, agentruntime.PermissionPropose, agentruntime.PermissionAutonomous:
		default:
			errs = append(errs, errors.New("milestone: agent "+key+" has invalid level "+def.Level))
		}
	}
	return errs
}
