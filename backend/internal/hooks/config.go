package hooks

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadConfigFile(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{Enabled: false}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Enabled: false}, nil
		}
		return Config{}, err
	}
	var wrapper struct {
		Hooks Config
	}
	if err := yaml.Unmarshal(data, &wrapper); err == nil && (wrapper.Hooks.Enabled || len(wrapper.Hooks.Hooks) > 0) {
		return normalizeConfig(wrapper.Hooks), nil
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("load hooks config: %w", err)
	}
	return normalizeConfig(cfg), nil
}

func NewEngineFromConfig(cfg Config) (*Engine, error) {
	cfg = normalizeConfig(cfg)
	if !cfg.Enabled || len(cfg.Hooks) == 0 {
		return NewEngine(cfg), nil
	}
	if err := Validate(cfg.Hooks, cfg.WebhookAllowedHosts...); err != nil {
		return nil, err
	}
	return NewEngine(cfg), nil
}
