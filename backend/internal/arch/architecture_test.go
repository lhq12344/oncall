package arch

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhaseZeroArchitectureArtifactsExist(t *testing.T) {
	backendRoot := findBackendRoot(t)
	repoRoot := filepath.Dir(backendRoot)

	for _, rel := range []string{
		"docs/architecture/CONTEXT.md",
		"docs/architecture/target-modules.md",
		"docs/architecture/domain-glossary.md",
		"docs/architecture/phase-completion-ledger.json",
		"docs/adr/0001-eino-runtime-kernel.md",
		"docs/adr/0002-workflow-agent-hybrid.md",
		"docs/adr/0003-event-trace-audit-separation.md",
		"docs/adr/0004-storage-and-adapter-policy.md",
		"docs/adr/0005-data-flywheel-governance.md",
	} {
		assertNonEmptyFile(t, filepath.Join(repoRoot, filepath.FromSlash(rel)), rel)
	}
}

func TestTargetModulesDescribeCutoverState(t *testing.T) {
	backendRoot := findBackendRoot(t)
	repoRoot := filepath.Dir(backendRoot)
	path := filepath.Join(repoRoot, "docs", "architecture", "target-modules.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target modules: %v", err)
	}
	content := string(b)
	for _, forbidden := range []string{
		"will own",
		"will become",
		"will adapt",
		"will group",
		"Phase 0 compatibility module",
		"Legacy modules can remain during migration",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("target module source of truth still uses migration-era language %q", forbidden)
		}
	}
	for _, required := range []string{
		"These seams are the source of truth",
		"Legacy implementation modules are not allowed after Phase 13",
		"`workflow/agentteams` must not exist",
		"root `internal/slash` must not exist",
		"root `internal/permissions` must not exist",
		"root `internal/compact` must not exist",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("target module cutover policy missing %q", required)
		}
	}
}

func TestPhaseZeroBaselineDatasetsAreVersioned(t *testing.T) {
	backendRoot := findBackendRoot(t)

	jsonlFiles := []string{
		"testdata/golden_traces/phase0_golden_traces.jsonl",
		"testdata/intent/phase0_intent_seed.jsonl",
		"testdata/replay/workflow/phase0_incident_replay.jsonl",
		"testdata/security/phase0_security_seed.jsonl",
	}
	for _, rel := range jsonlFiles {
		assertJSONLHasVersion(t, filepath.Join(backendRoot, filepath.FromSlash(rel)), rel)
	}

	jsonFiles := []string{
		"testdata/baselines/phase0_rag_baseline.json",
		"testdata/baselines/phase0_sse_baseline.json",
	}
	for _, rel := range jsonFiles {
		assertJSONHasVersion(t, filepath.Join(backendRoot, filepath.FromSlash(rel)), rel)
	}
}

func TestPhaseThirteenDatasetGateSizes(t *testing.T) {
	backendRoot := findBackendRoot(t)
	repoRoot := filepath.Dir(backendRoot)
	assertJSONLCountAtLeast(t, filepath.Join(backendRoot, "testdata", "rag_eval_gold.jsonl"), "RAG gold", 100)
	assertJSONLCountAtLeast(t, filepath.Join(backendRoot, "testdata", "rag_eval_gold_corpus.jsonl"), "RAG corpus", 100)
	assertJSONLCountAtLeast(t, filepath.Join(backendRoot, "testdata", "intent", "phase13_intent_gold.jsonl"), "Intent gold", 200)
	assertJSONLCountAtLeast(t, filepath.Join(backendRoot, "testdata", "security", "phase13_security_gold.jsonl"), "Security gold", 30)
	assertJSONLCountAtLeast(t, filepath.Join(backendRoot, "testdata", "replay", "incident", "phase10_incident_replay.jsonl"), "Incident replay", 20)
	assertNonEmptyFile(t, filepath.Join(repoRoot, "docs", "architecture", "phase13-verification-report.md"), "Phase 13 verification report")
	assertNonEmptyFile(t, filepath.Join(repoRoot, "docs", "architecture", "phase13-verification-checklist.json"), "Phase 13 verification checklist")
	assertNonEmptyFile(t, filepath.Join(repoRoot, "docs", "architecture", "phase13-live-verification-runbook.md"), "Phase 13 live verification runbook")
	assertNonEmptyFile(t, filepath.Join(repoRoot, "docs", "architecture", "phase13-production-evidence-template.json"), "Phase 13 production evidence template")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-verify.ps1"), "Phase 13 verification script")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-verify.cmd"), "Phase 13 Windows verification entrypoint")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-race-evidence.ps1"), "Phase 13 race evidence script")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-race-evidence.cmd"), "Phase 13 race evidence Windows entrypoint")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-live-verify.ps1"), "Phase 13 live verification script")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-live-verify.cmd"), "Phase 13 Windows live verification entrypoint")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-production-evidence-init.ps1"), "Phase 13 production evidence init script")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-production-evidence-init.cmd"), "Phase 13 production evidence init Windows entrypoint")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-production-evidence-update.ps1"), "Phase 13 production evidence update script")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-production-evidence-update.cmd"), "Phase 13 production evidence update Windows entrypoint")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-production-evidence-verify.ps1"), "Phase 13 production evidence verification script")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-production-evidence-verify.cmd"), "Phase 13 Windows production evidence entrypoint")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-status.ps1"), "Phase 13 status summary script")
	assertNonEmptyFile(t, filepath.Join(backendRoot, "hack", "phase13-status.cmd"), "Phase 13 Windows status summary entrypoint")
	assertNonEmptyFile(t, filepath.Join(repoRoot, ".github", "workflows", "phase13-verification.yml"), "Phase 13 CI workflow")
}

func TestPhaseThirteenVerifySupportsWslRaceFallback(t *testing.T) {
	backendRoot := findBackendRoot(t)
	path := filepath.Join(backendRoot, "hack", "phase13-verify.ps1")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read phase13 verify script: %v", err)
	}
	content := string(b)
	for _, required := range []string{
		"SkipWslRace",
		"ExternalRaceEvidencePath",
		"ConvertTo-WslPath",
		"Invoke-WslRaceGate",
		"wsl.exe -d $wslDistro -- bash -lc",
		"Test-ExternalRaceEvidence",
		"GOCACHE=/tmp/oncall-race-cache",
		"GOFLAGS=-p=2 CGO_ENABLED=1 go test -race ./...",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("phase13 verification script missing WSL race fallback marker %q", required)
		}
	}
}

func TestPhaseThirteenVerificationChecklistTracksExternalGates(t *testing.T) {
	backendRoot := findBackendRoot(t)
	repoRoot := filepath.Dir(backendRoot)
	path := filepath.Join(repoRoot, "docs", "architecture", "phase13-verification-checklist.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checklist: %v", err)
	}
	var checklist struct {
		Version    string `json:"version"`
		Status     string `json:"status"`
		LocalGates []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"local_gates"`
		ExternalGates []struct {
			ID             string `json:"id"`
			Status         string `json:"status"`
			Evidence       string `json:"evidence"`
			EvidenceNeeded string `json:"evidence_needed"`
		} `json:"external_gates"`
	}
	if err := json.Unmarshal(b, &checklist); err != nil {
		t.Fatalf("decode checklist: %v", err)
	}
	if checklist.Version != "phase13.verification/v1" || checklist.Status != "external_required" {
		t.Fatalf("unexpected checklist header: %+v", checklist)
	}
	wantLocal := map[string]bool{
		"gofmt":                                  false,
		"go_test":                                false,
		"go_vet":                                 false,
		"frontend_lint":                          false,
		"frontend_test":                          false,
		"frontend_build":                         false,
		"incident_replay_20":                     false,
		"rag_eval_100":                           false,
		"rag_inspect_smoke":                      false,
		"intent_200":                             false,
		"security_30":                            false,
		"legacy_cutover_scan":                    false,
		"changed_workspace_hygiene":              false,
		"frontend_run_event_only":                false,
		"sse_reconnect_unit":                     false,
		"git_diff_check":                         false,
		"approval_snapshot_unit":                 false,
		"data_flywheel_unit":                     false,
		"telemetry_redaction_unit":               false,
		"prompt_cutover_naming":                  false,
		"policy_legacy_removed":                  false,
		"rag_legacy_embedding_removed":           false,
		"incident_old_plan_bridge_removed":       false,
		"incident_execution_stage_alias_removed": false,
		"compact_placeholder_removed":            false,
		"skill_middleware_compat_probe_removed":  false,
		"controller_run_event_streams_only":      false,
		"integration_harness":                    false,
	}
	for _, gate := range checklist.LocalGates {
		if gate.ID == "" || gate.Status != "complete" {
			t.Fatalf("local gate must be complete and named: %+v", gate)
		}
		seen, ok := wantLocal[gate.ID]
		if !ok {
			t.Fatalf("unexpected local gate %s", gate.ID)
		}
		if seen {
			t.Fatalf("duplicate local gate %s", gate.ID)
		}
		wantLocal[gate.ID] = true
	}
	for id, seen := range wantLocal {
		if !seen {
			t.Fatalf("missing local gate %s", id)
		}
	}
	wantExternal := map[string]bool{
		"race":              false,
		"live_sse_pressure": false,
		"live_optional_dependency_fault_injection": false,
		"live_cozeloop_trace":                      false,
		"live_mutation_approval":                   false,
		"live_data_flywheel_publish":               false,
	}
	for _, gate := range checklist.ExternalGates {
		if _, ok := wantExternal[gate.ID]; ok {
			wantExternal[gate.ID] = true
		}
		switch gate.Status {
		case "complete":
			if strings.TrimSpace(gate.Evidence) == "" {
				t.Fatalf("completed external gate must record evidence: %+v", gate)
			}
		case "external_required":
			if strings.TrimSpace(gate.EvidenceNeeded) == "" {
				t.Fatalf("external gate must require evidence: %+v", gate)
			}
		default:
			t.Fatalf("external gate must be complete or require evidence: %+v", gate)
		}
	}
	for id, seen := range wantExternal {
		if !seen {
			t.Fatalf("missing external gate %s", id)
		}
	}
}

func TestPhaseCompletionLedgerTracksEveryPhase(t *testing.T) {
	backendRoot := findBackendRoot(t)
	repoRoot := filepath.Dir(backendRoot)
	path := filepath.Join(repoRoot, "docs", "architecture", "phase-completion-ledger.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read phase ledger: %v", err)
	}
	var ledger struct {
		Version    string `json:"version"`
		SourcePlan string `json:"source_plan"`
		Status     string `json:"status"`
		Phases     []struct {
			ID                     string   `json:"id"`
			Status                 string   `json:"status"`
			ImplementationEvidence []string `json:"implementation_evidence"`
			VerificationCommands   []string `json:"verification_commands"`
			ReviewEvidence         string   `json:"review_evidence"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(b, &ledger); err != nil {
		t.Fatalf("decode phase ledger: %v", err)
	}
	if ledger.Version != "phase-completion-ledger/v1" {
		t.Fatalf("unexpected ledger version %q", ledger.Version)
	}
	if ledger.SourcePlan != ".omx/plans/2026-09-02-oncall-complete-architecture-refactor.md" {
		t.Fatalf("ledger must point at the active plan, got %q", ledger.SourcePlan)
	}
	if ledger.Status != "external_required" {
		t.Fatalf("ledger must preserve external verification requirement, got %q", ledger.Status)
	}
	wantPhases := map[string]bool{}
	for i := 0; i <= 13; i++ {
		wantPhases["phase_"+itoa(i)] = false
	}
	for _, phase := range ledger.Phases {
		seen, ok := wantPhases[phase.ID]
		if !ok {
			t.Fatalf("unexpected phase id %q", phase.ID)
		}
		if seen {
			t.Fatalf("duplicate phase id %q", phase.ID)
		}
		wantPhases[phase.ID] = true
		if strings.TrimSpace(phase.Status) == "" {
			t.Fatalf("phase %s missing status", phase.ID)
		}
		if len(phase.ImplementationEvidence) == 0 {
			t.Fatalf("phase %s missing implementation evidence", phase.ID)
		}
		for _, rel := range phase.ImplementationEvidence {
			if strings.TrimSpace(rel) == "" {
				t.Fatalf("phase %s has blank implementation evidence", phase.ID)
			}
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(rel))); err != nil {
				t.Fatalf("phase %s implementation evidence missing: %s: %v", phase.ID, rel, err)
			}
		}
		if len(phase.VerificationCommands) == 0 {
			t.Fatalf("phase %s missing verification commands", phase.ID)
		}
		for _, cmd := range phase.VerificationCommands {
			if strings.TrimSpace(cmd) == "" {
				t.Fatalf("phase %s has blank verification command", phase.ID)
			}
		}
		if strings.TrimSpace(phase.ReviewEvidence) == "" {
			t.Fatalf("phase %s missing code-review evidence", phase.ID)
		}
	}
	for id, seen := range wantPhases {
		if !seen {
			t.Fatalf("missing phase ledger entry %s", id)
		}
	}
}

func TestPhaseThirteenVerificationScriptsFailClosed(t *testing.T) {
	backendRoot := findBackendRoot(t)
	localScript := filepath.Join(backendRoot, "hack", "phase13-verify.ps1")
	liveScript := filepath.Join(backendRoot, "hack", "phase13-live-verify.ps1")
	productionInitScript := filepath.Join(backendRoot, "hack", "phase13-production-evidence-init.ps1")
	productionUpdateScript := filepath.Join(backendRoot, "hack", "phase13-production-evidence-update.ps1")
	productionScript := filepath.Join(backendRoot, "hack", "phase13-production-evidence-verify.ps1")
	statusScript := filepath.Join(backendRoot, "hack", "phase13-status.ps1")

	for _, script := range []string{localScript, liveScript, productionScript, statusScript} {
		assertFileContains(t, script, "trap {", "verification scripts must emit evidence before failing")
		assertFileContains(t, script, "failed_gate", "verification scripts must record the failed gate")
		assertFileContains(t, script, "error", "verification scripts must record the failure message")
	}

	assertFileContains(t, localScript, "$unformatted = @(gofmt -l .)", "local verification must fail when gofmt reports unformatted files")
	assertFileContains(t, localScript, "throw \"gofmt found unformatted files\"", "local verification must throw on unformatted Go files")
	assertFileContains(t, localScript, "$matches = @(rg -n $legacyPatterns", "local verification must capture ripgrep legacy scan matches")
	assertFileContains(t, localScript, "throw \"legacy cutover scan found forbidden patterns\"", "local verification must fail when legacy cutover patterns are found")
	assertFileContains(t, localScript, "[Parameter(Mandatory = $true)][string]$GateID", "local verification failed_gate ids must use stable evidence gate ids")
	assertFileContains(t, localScript, "git ls-files -z --modified --others --exclude-standard", "local verification must scan changed and untracked files with NUL-safe Git output")
	assertFileContains(t, localScript, "':!:backend/.gocache/**'", "local verification must exclude generated local Go cache from hygiene scans")

	assertFileContains(t, liveScript, "status = \"missing_environment\"", "live verification must distinguish missing environment from generic failure")
	assertFileContains(t, liveScript, "id = \"env_check\"", "live verification failed_gate must identify environment validation failures")
	assertFileContains(t, liveScript, "failed_gates", "live verification must preserve all failed live gates")
	assertFileContains(t, liveScript, "LiveGateFailures", "live verification must continue collecting granular evidence after a live gate fails")
	for _, gate := range []string{
		"live_sse_endpoint",
		"live_sse_pressure",
		"live_dependency_redis",
		"live_dependency_elasticsearch",
		"live_dependency_milvus",
		"live_dependency_kubernetes",
		"live_dependency_cozeloop",
	} {
		assertFileContains(t, liveScript, gate, "live verification must record granular live gate evidence")
	}
	for _, gate := range []string{
		"live_optional_dependency_fault_injection",
		"live_cozeloop_trace",
		"live_mutation_approval",
		"live_data_flywheel_publish",
	} {
		assertFileContains(t, productionInitScript, gate, "production evidence init must scaffold remaining external gates")
		assertFileContains(t, productionUpdateScript, gate, "production evidence update must address remaining external gates")
		assertFileContains(t, productionScript, gate, "production evidence verification must cover remaining external gates")
	}
	assertFileContains(t, productionUpdateScript, "ValidateSet(\"dependency\", \"cozeloop_trace\", \"mutation_approval\", \"data_flywheel_publish\")", "production evidence update must restrict supported gate names")
	assertFileContains(t, productionUpdateScript, "Production evidence file not found", "production evidence update must fail closed when the evidence file is missing")
	assertFileContains(t, productionInitScript, "outage drill still required", "production evidence init must not claim fault injection completion")
	assertFileContains(t, productionInitScript, "status = \"external_required\"", "production evidence init must keep production evidence incomplete by default")
	assertFileContains(t, productionInitScript, "observed_degraded", "production evidence init must preserve dependency drill fields")
	assertFileContains(t, productionInitScript, "unrelated_capabilities_available", "production evidence init must preserve dependency drill fields")
	assertFileContains(t, statusScript, "phase13.status/v1", "status summary must use a versioned schema")
	assertFileContains(t, statusScript, "open_count", "status summary must expose the remaining open item count")
	assertFileContains(t, statusScript, "open_items", "status summary must list remaining open items")
}

func TestPromptAssemblerUsesCutoverNaming(t *testing.T) {
	backendRoot := findBackendRoot(t)
	promptRoot := filepath.Join(backendRoot, "internal", "prompt")
	var violations []string
	err := filepath.WalkDir(promptRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, "Compatibility"+"Assembler") || strings.Contains(line, "from"+"Legacy"+"Section") {
				violations = append(violations, rel+":"+itoa(lineNo)+": prompt assembler must use cutover naming, not compatibility-era names: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk prompt: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("prompt compatibility-era names must be removed:\n%s", strings.Join(violations, "\n"))
	}
}

func TestRAGDoesNotUseLegacyEmbeddingFallback(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string
	roots := []string{
		filepath.Join(backendRoot, "internal", "rag"),
		filepath.Join(backendRoot, "internal", "workflow", "dialogue"),
		filepath.Join(backendRoot, "internal", "tools", "dialogue"),
		filepath.Join(backendRoot, "cmd", "ragctl"),
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}
			rel := relativeToBackend(backendRoot, path)
			scanFileLines(t, path, func(lineNo int, line string) {
				for _, marker := range []string{
					"Legacy" + "Retriever",
					"legacy" + "Retriever",
					"legacy" + "Collection",
					"knowledge" + "Legacy",
					"ops" + "Legacy",
					"embedding_" + "legacy",
					"source.embedding_" + "legacy_docs",
					"legacy_" + "knowledge",
					"legacy_" + "ops_cases",
				} {
					if strings.Contains(line, marker) {
						violations = append(violations, rel+":"+itoa(lineNo)+": RAG must not use old embedding fallback after Phase 13: "+strings.TrimSpace(line))
					}
				}
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("RAG legacy embedding fallback must be removed:\n%s", strings.Join(violations, "\n"))
	}
}

func TestIncidentRemediationProposalRejectsOldCommandPlanBridge(t *testing.T) {
	backendRoot := findBackendRoot(t)
	parserPath := filepath.Join(backendRoot, "internal", "workflow", "ops", "incident_parser.go")
	var violations []string
	scanFileLines(t, parserPath, func(lineNo int, line string) {
		for _, marker := range []string{
			"legacy" + "Command",
			"legacy" + "Plan",
			"findLatestJSONObject(messages, \"commands\")",
		} {
			if strings.Contains(line, marker) {
				violations = append(violations, "internal/workflow/ops/incident_parser.go:"+itoa(lineNo)+": remediation proposal parsing must accept current actions contract only after Phase 13: "+strings.TrimSpace(line))
			}
		}
	})
	if len(violations) > 0 {
		t.Fatalf("old command-plan remediation bridge must be removed:\n%s", strings.Join(violations, "\n"))
	}
}

func TestIncidentStateBridgeUsesCutoverExecutionStagesOnly(t *testing.T) {
	backendRoot := findBackendRoot(t)
	stateBridgePath := filepath.Join(backendRoot, "internal", "workflow", "ops", "state_bridge.go")
	var violations []string
	scanFileLines(t, stateBridgePath, func(lineNo int, line string) {
		for _, marker := range []string{
			"case " + "\"execution\"",
			"\"" + "execution" + "\", \"" + "execute_plan" + "\"",
			"compatibility " + "alias",
		} {
			if strings.Contains(line, marker) {
				violations = append(violations, "internal/workflow/ops/state_bridge.go:"+itoa(lineNo)+": incident state bridge must use cutover execution stages only: "+strings.TrimSpace(line))
			}
		}
	})
	if len(violations) > 0 {
		t.Fatalf("old execution stage alias must be removed:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCompactCompatibilityPlaceholderRemoved(t *testing.T) {
	backendRoot := findBackendRoot(t)
	path := filepath.Join(backendRoot, "internal", "context", "compact", "eino_middleware.go")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		t.Fatalf("internal/context/compact/eino_middleware.go was a migration placeholder and must stay removed after Phase 13")
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat compact placeholder: %v", err)
	}
}

func TestSkillMiddlewareProbeRemoved(t *testing.T) {
	backendRoot := findBackendRoot(t)
	path := filepath.Join(backendRoot, "internal", "skills", "eino_middleware.go")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		t.Fatalf("internal/skills/eino_middleware.go exposes an unused %s probe and must stay removed after Phase 13", "Middleware"+"Compatibility")
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat skills middleware probe: %v", err)
	}
}

func TestControllerStreamsEmitRunEventsOnly(t *testing.T) {
	backendRoot := findBackendRoot(t)
	path := filepath.Join(backendRoot, "internal", "controller", "chat", "chat_v1.go")
	var violations []string
	scanFileLines(t, path, func(lineNo int, line string) {
		for _, marker := range []string{
			"writeSSEJSON(",
			"writeSSEJSON(r, map[string]any{\"type\"",
			"writeSSEData(r, fmt.Sprintf(\"{\\\"type\\\"",
			"writeSSEData(r, \"{\\\"type\\\"",
			"writeSSEJSON(r, withSSEWorkflow(map[string]any{\"type\"",
		} {
			if strings.Contains(line, marker) {
				violations = append(violations, "internal/controller/chat/chat_v1.go:"+itoa(lineNo)+": stream responses must emit versioned RunEvent frames only: "+strings.TrimSpace(line))
			}
		}
	})
	if len(violations) > 0 {
		t.Fatalf("controller stream legacy frame writers must be removed:\n%s", strings.Join(violations, "\n"))
	}
}

func TestMySQLIsNotAProjectDependency(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	forbiddenImportFragments := []string{
		`"go_agent/utility/mysql"`,
		`"gorm.io/`,
		`"github.com/go-sql-driver/mysql"`,
	}

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			if strings.EqualFold(filepath.ToSlash(path), filepath.ToSlash(filepath.Join(backendRoot, "utility", "mysql"))) {
				violations = append(violations, relativeToBackend(backendRoot, path)+": MySQL utility package must be deleted")
				return filepath.SkipDir
			}
			return nil
		}

		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") {
			return nil
		}
		switch {
		case strings.HasSuffix(name, ".go"):
			scanFileLines(t, path, func(lineNo int, line string) {
				for _, fragment := range forbiddenImportFragments {
					if strings.Contains(line, fragment) {
						violations = append(violations, rel+":"+itoa(lineNo)+": forbidden MySQL/GORM import: "+strings.TrimSpace(line))
					}
				}
			})
		case name == "go.mod":
			scanFileLines(t, path, func(lineNo int, line string) {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "gorm.io/") || strings.HasPrefix(trimmed, "github.com/go-sql-driver/mysql ") {
					violations = append(violations, rel+":"+itoa(lineNo)+": forbidden MySQL/GORM module dependency: "+trimmed)
				}
			})
		case strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml"):
			scanFileLines(t, path, func(lineNo int, line string) {
				trimmed := strings.TrimSpace(line)
				if trimmed == "mysql:" || strings.Contains(trimmed, "mysql:") || strings.Contains(trimmed, "MYSQL_DSN") {
					violations = append(violations, rel+":"+itoa(lineNo)+": forbidden MySQL config: "+trimmed)
				}
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("MySQL must not be a project dependency:\n%s", strings.Join(violations, "\n"))
	}
}

func TestRedisSDKIsAdapterOnly(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}

		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") || strings.HasPrefix(rel, "internal/adapters/redis/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, `"github.com/redis/go-redis/v9"`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": Redis SDK import must stay behind internal/adapters/redis: "+strings.TrimSpace(line))
			}
			if strings.Contains(line, `"go_agent/utility/mem"`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": legacy Redis memory utility must not be imported: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("Redis SDK must be isolated behind adapter package:\n%s", strings.Join(violations, "\n"))
	}
}

func TestElasticsearchSDKIsAdapterOnly(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}

		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") || strings.HasPrefix(rel, "internal/adapters/elasticsearch/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, `"github.com/elastic/go-elasticsearch/v8"`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": Elasticsearch SDK import must stay behind internal/adapters/elasticsearch: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("Elasticsearch SDK must be isolated behind adapter package:\n%s", strings.Join(violations, "\n"))
	}
}

func TestPrometheusSDKIsAdapterOnly(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}

		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") || strings.HasPrefix(rel, "internal/adapters/prometheus/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, `"github.com/prometheus/client_golang`) || strings.Contains(line, `"github.com/prometheus/common`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": Prometheus SDK import must stay behind internal/adapters/prometheus: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("Prometheus SDK must be isolated behind adapter package:\n%s", strings.Join(violations, "\n"))
	}
}

func TestKubernetesSDKIsAdapterOnly(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}

		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") || strings.HasPrefix(rel, "internal/adapters/kubernetes/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, `"k8s.io/`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": Kubernetes SDK import must stay behind internal/adapters/kubernetes: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("Kubernetes SDK must be isolated behind adapter package:\n%s", strings.Join(violations, "\n"))
	}
}

func TestMilvusSDKIsAdapterOnly(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}

		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") || strings.HasPrefix(rel, "internal/adapters/milvus/") {
			return nil
		}
		// Transitional Phase 0 compatibility seams. These are removed in the RAG
		// indexing/retrieval migration once Milvus moves behind internal/adapters.
		if strings.HasPrefix(rel, "internal/ai/indexer/") || strings.HasPrefix(rel, "internal/ai/retriever/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, `"github.com/milvus-io/milvus-sdk-go/v2`) || strings.Contains(line, `"github.com/cloudwego/eino-ext/components/indexer/milvus`) || strings.Contains(line, `"github.com/cloudwego/eino-ext/components/retriever/milvus`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": Milvus SDK import must stay behind internal/adapters/milvus or documented Phase 0 compatibility seams: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("Milvus SDK must be isolated behind adapter package:\n%s", strings.Join(violations, "\n"))
	}
}

func TestControllerDoesNotConstructRuntimeInternals(t *testing.T) {
	backendRoot := findBackendRoot(t)
	controllerRoot := filepath.Join(backendRoot, "internal", "controller")
	var violations []string

	err := filepath.WalkDir(controllerRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		scanFileLines(t, path, func(lineNo int, line string) {
			trimmed := strings.TrimSpace(line)
			for _, forbidden := range []string{
				"NewDialogueAgent(",
				"NewOpsAgent(",
				"NewIncidentWorkflowAgent(",
				"NewExecutionAgent(",
				"NewRCAAgent(",
				"NewStrategyAgent(",
				"NewPlanAgent(",
				"NewChatModel(",
				"NewPolicyEngine(",
				"NewCheckpointStore(",
			} {
				if strings.Contains(trimmed, forbidden) {
					violations = append(violations, rel+":"+itoa(lineNo)+": controller must receive runtime internals through bootstrap seams, not construct "+forbidden)
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk controller: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("controllers must not construct runtime internals:\n%s", strings.Join(violations, "\n"))
	}
}

func TestWorkflowDoesNotImportTransportTypes(t *testing.T) {
	backendRoot := findBackendRoot(t)
	workflowRoot := filepath.Join(backendRoot, "internal", "workflow")
	var violations []string

	err := filepath.WalkDir(workflowRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		scanFileLines(t, path, func(lineNo int, line string) {
			for _, forbidden := range []string{
				`"go_agent/internal/controller`,
				`"go_agent/internal/server/http`,
				`"go_agent/internal/server/sse`,
				`"go_agent/api/chat`,
			} {
				if strings.Contains(line, forbidden) {
					violations = append(violations, rel+":"+itoa(lineNo)+": workflow must not import transport/frontend type: "+strings.TrimSpace(line))
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk workflow: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("workflow packages must stay transport-agnostic:\n%s", strings.Join(violations, "\n"))
	}
}

func TestRAGCoreDoesNotReadGlobalConfig(t *testing.T) {
	backendRoot := findBackendRoot(t)
	ragRoot := filepath.Join(backendRoot, "internal", "rag")
	var violations []string

	err := filepath.WalkDir(ragRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		scanFileLines(t, path, func(lineNo int, line string) {
			for _, forbidden := range []string{
				`"go_agent/internal/config`,
				`"go_agent/utility/common`,
				"common.Load",
				"config.Load",
				"config.Build(",
			} {
				if strings.Contains(line, forbidden) {
					violations = append(violations, rel+":"+itoa(lineNo)+": RAG core must receive config values through its Config interface: "+strings.TrimSpace(line))
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk rag: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("RAG core must not read global config directly:\n%s", strings.Join(violations, "\n"))
	}
}

func TestAgentTeamsPackageIsMarkedForPhaseThirteenRemoval(t *testing.T) {
	backendRoot := findBackendRoot(t)
	agentTeamsDir := filepath.Join(backendRoot, "internal", "workflow", "agent"+"teams")
	if info, err := os.Stat(agentTeamsDir); err == nil && info.IsDir() {
		t.Fatalf("workflow/agent" + "teams compatibility package must be renamed or removed in Phase 13")
	}
}

func TestToolRegistryUsesPluralToolsPackage(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	legacyToolDir := filepath.Join(backendRoot, "internal", "tool")
	if info, err := os.Stat(legacyToolDir); err == nil && info.IsDir() {
		violations = append(violations, "internal/tool: singular tool package must be migrated to internal/tools")
	}

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, `"go_agent/internal/tool"`) || strings.Contains(line, `"go_agent/internal/tool/`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": import internal/tools instead of internal/tool: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("tool registry must use the plural tools package like the reference project:\n%s", strings.Join(violations, "\n"))
	}
}

func TestToolRuntimeDoesNotDeclareSecondRegistry(t *testing.T) {
	backendRoot := findBackendRoot(t)
	toolsRoot := filepath.Join(backendRoot, "internal", "tools")
	var violations []string

	err := filepath.WalkDir(toolsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		if rel == "internal/tools/registry.go" {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "type "+"Registry struct") || strings.Contains(trimmed, "func New"+"Registry(") {
				violations = append(violations, rel+":"+itoa(lineNo)+": only internal/tools/registry.go may declare the tool registry: "+trimmed)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk tools: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("tool runtime must not declare a second registry:\n%s", strings.Join(violations, "\n"))
	}
}

func TestAgentConstructorsUseToolRegistrySeam(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	agentConstructorFiles := map[string]struct{}{
		"internal/workflow/dialogue/agent.go": {},
		"internal/workflow/ops/agent.go":      {},
		"internal/workflow/ops/plan_agent.go": {},
		"internal/execution/agent.go":         {},
		"internal/agent/rca/agent.go":         {},
		"internal/agent/strategy/agent.go":    {},
	}

	for rel := range agentConstructorFiles {
		path := filepath.Join(backendRoot, filepath.FromSlash(rel))
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, `"go_agent/internal/`+`toolkit"`) || strings.Contains(line, `"go_agent/internal/tools/policy/`+`legacy"`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": agent constructors must get executable tools from internal/tools registry seam: "+strings.TrimSpace(line))
			}
		})
	}

	if len(violations) > 0 {
		t.Fatalf("agent constructors should not assemble tool gateway internals directly:\n%s", strings.Join(violations, "\n"))
	}
}

func TestNoLegacyPolicyPackage(t *testing.T) {
	backendRoot := findBackendRoot(t)
	legacyPolicyDir := filepath.Join(backendRoot, "internal", "tools", "policy", "legacy")
	if info, err := os.Stat(legacyPolicyDir); err == nil && info.IsDir() {
		t.Fatalf("internal/tools/policy/" + "legacy must be migrated to a named policy seam package")
	}
	var violations []string
	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, `"go_agent/internal/tools/policy/`+`legacy"`) {
				violations = append(violations, rel+":"+itoa(lineNo)+": policy legacy import is forbidden after Phase 13: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("legacy policy package references must be removed:\n%s", strings.Join(violations, "\n"))
	}
}

func TestExternalClientUtilitiesLiveInAdapters(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string

	legacyDirs := []string{
		filepath.Join(backendRoot, "utility", "elasticsearch"),
		filepath.Join(backendRoot, "utility", "kubernetes"),
	}
	for _, dir := range legacyDirs {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			violations = append(violations, relativeToBackend(backendRoot, dir)+": external client utility package must live under internal/adapters")
		}
	}

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			for _, oldImport := range []string{`"go_agent/utility/elasticsearch"`, `"go_agent/utility/kubernetes"`} {
				if strings.Contains(line, oldImport) {
					violations = append(violations, rel+":"+itoa(lineNo)+": import internal/adapters instead of utility client package: "+strings.TrimSpace(line))
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("external client utilities must be adapter packages:\n%s", strings.Join(violations, "\n"))
	}
}

func TestPhaseOneRuntimePackagesExist(t *testing.T) {
	backendRoot := findBackendRoot(t)
	for _, rel := range []string{
		"internal/config/config.go",
		"internal/config/model.go",
		"internal/config/storage.go",
		"internal/config/observability.go",
		"internal/config/validation.go",
		"internal/model/catalog.go",
		"internal/events/run_event.go",
		"internal/events/run_event_test.go",
		"internal/telemetry/recorder.go",
		"internal/app/application.go",
		"internal/app/runtime.go",
		"internal/app/capabilities.go",
		"internal/adapters/cozeloop/recorder.go",
		"internal/bootstrap/runtime.go",
		"cmd/oncall/main.go",
	} {
		if _, err := os.Stat(filepath.Join(backendRoot, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("phase 1 runtime artifact missing: %s: %v", rel, err)
		}
	}
}

func TestConfigReadsRuntimeSourcesInConfigModuleOnly(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string
	checkedPrefixes := []string{
		"main.go",
		"cmd/oncall/",
		"internal/bootstrap/",
		"internal/app/",
		"internal/model/",
		"internal/events/",
		"internal/telemetry/",
	}

	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		if !hasAnyPrefix(rel, checkedPrefixes) {
			return nil
		}
		if strings.HasPrefix(rel, "internal/config/") || strings.HasPrefix(rel, "internal/adapters/") || strings.HasPrefix(rel, "utility/common/") || strings.HasPrefix(rel, "internal/arch/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, "g.Cfg().") || strings.Contains(line, "os.Getenv(") {
				violations = append(violations, rel+":"+itoa(lineNo)+": runtime config source reads must stay in internal/config or adapter compatibility seams: "+strings.TrimSpace(line))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("runtime config reads must be centralized:\n%s", strings.Join(violations, "\n"))
	}
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func TestRunEventHasSingleVersionedSchemaPackage(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var definitions []string
	err := filepath.WalkDir(backendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel := relativeToBackend(backendRoot, path)
		if strings.HasPrefix(rel, "internal/arch/") {
			return nil
		}
		scanFileLines(t, path, func(lineNo int, line string) {
			if strings.Contains(line, "type "+"RunEvent struct") {
				definitions = append(definitions, rel+":"+itoa(lineNo))
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend: %v", err)
	}
	if len(definitions) != 1 || !strings.HasPrefix(definitions[0], "internal/events/run_event.go:") {
		t.Fatalf("RunEvent must have exactly one schema definition in internal/events: %v", definitions)
	}
}

func TestNoLegacySSESentinels(t *testing.T) {
	backendRoot := findBackendRoot(t)
	repoRoot := filepath.Dir(backendRoot)
	var violations []string
	for _, root := range []string{filepath.Join(backendRoot, "internal", "controller"), filepath.Join(repoRoot, "frontend", "src")} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), ".ts") && !strings.HasSuffix(d.Name(), ".tsx") {
				return nil
			}
			rel, _ := filepath.Rel(repoRoot, path)
			scanFileLines(t, path, func(lineNo int, line string) {
				if strings.Contains(line, "["+"DONE"+"]") || strings.Contains(line, "["+"ERROR"+"]") || strings.Contains(line, "Go "+"map") || strings.Contains(line, "map"+"[command") {
					violations = append(violations, filepath.ToSlash(rel)+":"+itoa(lineNo)+": legacy SSE/parser sentinel is forbidden: "+strings.TrimSpace(line))
				}
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("legacy SSE sentinels must be removed:\n%s", strings.Join(violations, "\n"))
	}
}

func TestFrontendConsumesOnlyVersionedRunEvents(t *testing.T) {
	backendRoot := findBackendRoot(t)
	repoRoot := filepath.Dir(backendRoot)
	frontendRoot := filepath.Join(repoRoot, "frontend", "src")
	var violations []string

	forbidden := []string{
		"LegacyStreamEvent",
		"legacyToRunEvent",
		"reduceLegacyStreamEvent",
		"handleLegacyEvent",
		"legacy-run",
		"parseBashRequestFromText",
		"Stringer text from compatibility endpoints",
	}
	err := filepath.WalkDir(frontendRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".ts") && !strings.HasSuffix(d.Name(), ".tsx") {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		scanFileLines(t, path, func(lineNo int, line string) {
			for _, marker := range forbidden {
				if strings.Contains(line, marker) {
					violations = append(violations, filepath.ToSlash(rel)+":"+itoa(lineNo)+": frontend must consume only versioned RunEvent frames: "+strings.TrimSpace(line))
				}
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk frontend: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("frontend legacy event adapters must be removed:\n%s", strings.Join(violations, "\n"))
	}
}

func TestNoGlobalAdapterSingletons(t *testing.T) {
	backendRoot := findBackendRoot(t)
	var violations []string
	for _, root := range []string{filepath.Join(backendRoot, "internal", "adapters"), filepath.Join(backendRoot, "utility")} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			rel := relativeToBackend(backendRoot, path)
			scanFileLines(t, path, func(lineNo int, line string) {
				if strings.Contains(line, "Global"+"ES") || strings.Contains(line, "var "+"Global") {
					violations = append(violations, rel+":"+itoa(lineNo)+": global adapter singleton is forbidden: "+strings.TrimSpace(line))
				}
			})
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("global adapter singletons must be removed:\n%s", strings.Join(violations, "\n"))
	}
}
func findBackendRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find backend go.mod from %s", wd)
		}
	}
}

func scanFileLines(t *testing.T, path string, visit func(lineNo int, line string)) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		visit(lineNo, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
}

func relativeToBackend(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func assertNonEmptyFile(t *testing.T, path, label string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("required architecture artifact missing: %s: %v", label, err)
	}
	if info.Size() == 0 {
		t.Fatalf("required architecture artifact is empty: %s", label)
	}
}

func assertFileContains(t *testing.T, path, needle, message string) {
	t.Helper()
	found := false
	scanFileLines(t, path, func(_ int, line string) {
		if strings.Contains(line, needle) {
			found = true
		}
	})
	if !found {
		t.Fatalf("%s", message)
	}
}

func assertJSONLHasVersion(t *testing.T, path, label string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", label, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("%s:%d invalid JSONL: %v", label, lineNo, err)
		}
		version, ok := record["version"].(string)
		if !ok || strings.TrimSpace(version) == "" {
			t.Fatalf("%s:%d missing non-empty version field", label, lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", label, err)
	}
	if lineNo == 0 {
		t.Fatalf("%s must contain at least one versioned fixture", label)
	}
}

func assertJSONHasVersion(t *testing.T, path, label string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", label, err)
	}
	var record map[string]any
	if err := json.Unmarshal(b, &record); err != nil {
		t.Fatalf("%s invalid JSON: %v", label, err)
	}
	version, ok := record["version"].(string)
	if !ok || strings.TrimSpace(version) == "" {
		t.Fatalf("%s missing non-empty version field", label)
	}
}

func assertJSONLCountAtLeast(t *testing.T, path, label string, want int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", label, err)
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", label, err)
	}
	if count < want {
		t.Fatalf("%s count=%d, want >=%d", label, count, want)
	}
}
