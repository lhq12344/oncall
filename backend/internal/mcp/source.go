package mcp

import (
	"fmt"
	"strings"

	"go_agent/internal/context/notice"
	toolregistry "go_agent/internal/tools"
)

type Tool struct {
	Server      string
	Name        string
	Description string
}

func Namespace(server, tool string) string {
	return "mcp." + strings.TrimSpace(server) + "." + strings.TrimSpace(tool)
}

func Descriptor(server string, tool Tool) toolregistry.ToolDescriptor {
	d := toolregistry.DefaultDescriptor(toolregistry.ToolID(Namespace(server, tool.Name)))
	d.Source = toolregistry.ToolSourceMCP
	d.Exposure = toolregistry.ToolExposureDeferredGateway
	d.Capability = toolregistry.Capability("mcp." + server)
	d.Risk = toolregistry.RiskLow
	return d
}

func InstructionNotice(cfg ServerConfig) notice.Notice {
	return notice.Notice{Kind: notice.KindMCPServer, Trust: notice.TrustUntrustedEvidence, Source: "mcp:" + cfg.Name, Lifecycle: notice.LifecycleSession, Content: cfg.Instruction, Priority: 60, DedupKey: "mcp:" + cfg.Name}
}

func ValidateToolName(server, tool string) error {
	if strings.TrimSpace(server) == "" || strings.TrimSpace(tool) == "" {
		return fmt.Errorf("server and tool are required")
	}
	return nil
}
