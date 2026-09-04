package policy

import (
	"fmt"
	"strconv"
	"strings"
)

func BindApproval(req Request) (ApprovalSnapshot, error) {
	snapshot, err := NewApprovalSnapshot(req.ToolID, req.ToolVersion, req.Args)
	if err != nil {
		return ApprovalSnapshot{}, err
	}
	snapshot.Cluster = stringArg(req.Args, "cluster")
	snapshot.Namespace = stringArg(req.Args, "namespace")
	snapshot.Resource = firstStringArg(req.Args, "resource", "target")
	snapshot.PlanID = firstStringArg(req.Args, "plan_id", "plan")
	snapshot.Revision = intArg(req.Args, "revision")
	snapshot.SnapshotHash = firstStringArg(req.Args, "snapshot_hash", "hash")
	return snapshot, nil
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstStringArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringArg(args, key); value != "" {
			return value
		}
	}
	return ""
}

func intArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case jsonNumber:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

type jsonNumber interface {
	String() string
}
