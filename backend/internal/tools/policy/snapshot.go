package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type ApprovalSnapshot struct {
	ToolID       string         `json:"tool_id"`
	ToolVersion  string         `json:"tool_version"`
	ArgsHash     string         `json:"args_hash"`
	Cluster      string         `json:"cluster,omitempty"`
	Namespace    string         `json:"namespace,omitempty"`
	Resource     string         `json:"resource,omitempty"`
	PlanID       string         `json:"plan_id,omitempty"`
	Revision     int            `json:"revision,omitempty"`
	SnapshotHash string         `json:"snapshot_hash,omitempty"`
	Scope        string         `json:"scope"`
	ExpiresUnix  int64          `json:"expires_unix,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

func HashArgs(args map[string]any) (string, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func NewApprovalSnapshot(toolID, version string, args map[string]any) (ApprovalSnapshot, error) {
	hash, err := HashArgs(args)
	if err != nil {
		return ApprovalSnapshot{}, fmt.Errorf("hash tool args: %w", err)
	}
	return ApprovalSnapshot{ToolID: toolID, ToolVersion: version, ArgsHash: hash, Scope: "one_shot"}, nil
}

func (s ApprovalSnapshot) SameApprovalTarget(other ApprovalSnapshot) bool {
	return s.ToolID == other.ToolID && s.ToolVersion == other.ToolVersion && s.ArgsHash == other.ArgsHash && s.Cluster == other.Cluster && s.Namespace == other.Namespace && s.Resource == other.Resource && s.PlanID == other.PlanID && s.Revision == other.Revision && s.SnapshotHash == other.SnapshotHash && s.Scope == other.Scope
}
