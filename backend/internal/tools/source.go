package toolregistry

func riskForTool(name string) RiskLevel {
	switch name {
	case "execute_step", "rollback", "bash_execute_with_approval":
		return RiskHigh
	case "update_knowledge", "prune_knowledge":
		return RiskMedium
	default:
		return RiskLow
	}
}

func capabilityForTool(name string) Capability {
	switch name {
	case "k8s_monitor", "build_dependency_graph":
		return Capability("kubernetes.read")
	case "metrics_collector":
		return Capability("metrics.read")
	case "es_log_query":
		return Capability("logs.read")
	case "knowledge_retrieve", "ops_case_retrieve":
		return Capability("rag.read")
	case "execute_step", "rollback", "bash_execute_with_approval":
		return Capability("execution.mutation")
	default:
		return Capability("agent.tool")
	}
}
