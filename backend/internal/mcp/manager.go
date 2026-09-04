package mcp

import "strings"

type Manager struct {
	servers map[string]ServerConfig
	health  map[string]Health
}

func NewManager(configs []ServerConfig) *Manager {
	m := &Manager{servers: map[string]ServerConfig{}, health: map[string]Health{}}
	for _, cfg := range configs {
		cfg = cfg.Normalize()
		name := strings.TrimSpace(cfg.Name)
		if name == "" {
			continue
		}
		m.servers[name] = cfg
		m.health[name] = Health{Server: name, Available: false, Reason: "not connected"}
	}
	return m
}

func (m *Manager) Health(server string) Health {
	if m == nil || m.health == nil {
		return Health{Server: server, Available: false, Reason: "mcp manager unavailable"}
	}
	if h, ok := m.health[server]; ok {
		return h
	}
	return Health{Server: server, Available: false, Reason: "server not configured"}
}
