package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

// observationCollectorAgent 在 RCA 前采集基础观测快照并写入 Graph State。
type observationCollectorAgent struct {
	name      string
	desc      string
	namespace string
	executor  *IntegratedOpsExecutor
	logger    *zap.Logger
}

// newObservationCollectorAgent 创建观测采集节点。
// 输入：ctx、IncidentWorkflowConfig。
// 输出：可运行的观测采集 Agent（即使底层工具降级也可继续执行）。
func newObservationCollectorAgent(ctx context.Context, cfg *IncidentWorkflowConfig) adk.Agent {
	agent := &observationCollectorAgent{
		name:      "observation_collector",
		desc:      "采集 K8s/Prometheus/Elasticsearch 观测快照并写入 Graph State",
		namespace: "infra",
		logger:    nil,
	}
	if cfg == nil {
		return agent
	}

	agent.logger = cfg.Logger
	executor, err := NewIntegratedOpsExecutor(ctx, &IntegratedOpsConfig{
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        cfg.Logger,
		Timeout:       20 * time.Second,
	})
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to init observation collector executor, degrade to empty snapshot",
				zap.Error(err))
		}
		return agent
	}
	agent.executor = executor
	return agent
}

func (a *observationCollectorAgent) Name(_ context.Context) string {
	return a.name
}

func (a *observationCollectorAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *observationCollectorAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()
		adk.AddSessionValue(ctx, "current_agent", a.name)

		state := getIncidentState(ctx)
		summary := "观测采集降级：未初始化集成执行器。"
		errorTexts := []string{"integrated executor unavailable"}

		if a.executor != nil {
			out, err := a.executor.QueryAllSources(ctx, QueryAllSourcesInput{
				Namespace: a.namespace,
				PromQuery: buildNamespaceCPUHistoryQuery(a.namespace),
				TimeRange: "30m",
				ESIndex:   "logs-*",
				ESQuery:   buildNamespaceESQuery(a.namespace),
				ESLevel:   "error",
				ESSize:    50,
			})
			if err != nil {
				errorTexts = append(errorTexts, err.Error())
				summary = "观测采集失败：查询多数据源时发生错误。"
			} else {
				summary, errorTexts = summarizeObservationSnapshot(out)
			}
		}

		state.ObservationCollected = true
		state.ObservationNamespace = a.namespace
		state.ObservationCollectedAt = time.Now().Format(time.RFC3339)
		state.ObservationTimeRange = "30m"
		state.ObservationSummary = clipText(summary, 1000)
		state.ObservationErrors = errorTexts
		state.ObservationRefreshNeeded = false
		state.ObservationRefreshReason = ""
		state.RuntimeObservationSummary = ""
		state.UpdatedAt = time.Now().Format(time.RFC3339)
		setIncidentState(ctx, state)

		if a.logger != nil {
			a.logger.Info("observation snapshot collected",
				zap.String("namespace", a.namespace),
				zap.Int("error_count", len(errorTexts)))
		}

		generator.Send(assistantEvent(fmt.Sprintf("已完成观测采集（namespace=%s），进入 RCA 根因分析。", a.namespace)))
	}()

	return iterator
}

// summarizeObservationSnapshot 生成简明观测摘要。
// 输入：多数据源查询输出。
// 输出：摘要文本、错误列表。
func summarizeObservationSnapshot(out *QueryAllSourcesOutput) (string, []string) {
	if out == nil {
		return "观测采集结果为空。", []string{"empty output"}
	}

	lines := make([]string, 0, 8)
	errors := make([]string, 0, len(out.Errors))

	if k8sRaw, ok := out.Data["k8s"]; ok {
		lines = append(lines, summarizeK8sPayload(k8sRaw))
	}
	if promRaw, ok := out.Data["prometheus"]; ok {
		lines = append(lines, summarizePromPayload(promRaw))
	}
	if promSourceRaw, ok := out.Data["prometheus_sources"]; ok {
		lines = append(lines, summarizePromSourcePayload(promSourceRaw))
	}
	lines = append(lines, summarizePromHistoryPayloads(out.Data)...)
	if esRaw, ok := out.Data["elasticsearch"]; ok {
		lines = append(lines, summarizeESPayload(esRaw))
	}
	if esIndicesRaw, ok := out.Data["elasticsearch_indices"]; ok {
		lines = append(lines, summarizeESIndexPayload(esIndicesRaw))
	}

	for source, errText := range out.Errors {
		source = strings.TrimSpace(source)
		errText = strings.TrimSpace(errText)
		if source == "" || errText == "" {
			continue
		}
		errors = append(errors, fmt.Sprintf("%s: %s", source, errText))
	}

	if len(lines) == 0 {
		lines = append(lines, "观测结果为空，未读取到有效数据源输出。")
	}
	if len(errors) > 0 {
		lines = append(lines, "采集错误："+strings.Join(errors, "; "))
	}

	return strings.Join(lines, "\n"), errors
}

// summarizeK8sPayload 汇总 K8s 观测结果。
// 输入：k8s_monitor 输出 JSON 字符串。
// 输出：摘要文本。
func summarizeK8sPayload(raw string) string {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "K8s：结果解析失败。"
	}

	namespace := toTrimString(payload["namespace"])
	count := toInt(payload["count"])
	if namespace == "" {
		return fmt.Sprintf("K8s：采集到 %d 个 Pod 结果。", count)
	}
	return fmt.Sprintf("K8s：命名空间 %s 下采集到 %d 个 Pod 结果。", namespace, count)
}

// summarizePromPayload 汇总 Prometheus 观测结果。
// 输入：metrics_collector 输出 JSON 字符串。
// 输出：摘要文本。
func summarizePromPayload(raw string) string {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "Prometheus：结果解析失败。"
	}
	resultType := strings.TrimSpace(toTrimString(payload["type"]))
	count := toInt(payload["count"])
	if resultType == "" {
		resultType = "unknown"
	}
	return fmt.Sprintf("Prometheus：%s 查询返回 %d 条时间序列。", resultType, count)
}

// summarizePromSourcePayload 汇总 Prometheus 指标源发现结果。
// 输入：discover_sources 输出 JSON 字符串。
// 输出：摘要文本。
func summarizePromSourcePayload(raw string) string {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "Prometheus 指标源发现：结果解析失败。"
	}

	active := toInt(payload["active_targets"])
	dropped := toInt(payload["dropped_targets"])
	down := 0
	if health, ok := payload["health_summary"].(map[string]any); ok {
		down = toInt(health["down"])
	}
	return fmt.Sprintf("Prometheus 指标源：active=%d，down=%d，dropped=%d。", active, down, dropped)
}

// summarizePromHistoryPayloads 汇总 Prometheus 历史指标查询结果。
// 输入：聚合数据 map。
// 输出：历史指标摘要列表。
func summarizePromHistoryPayloads(data map[string]string) []string {
	if len(data) == 0 {
		return nil
	}
	items := []struct {
		Key   string
		Label string
	}{
		{Key: "prometheus_cpu", Label: "CPU"},
		{Key: "prometheus_memory", Label: "内存"},
		{Key: "prometheus_restarts", Label: "重启"},
	}

	lines := make([]string, 0, len(items))
	for _, item := range items {
		raw, ok := data[item.Key]
		if !ok {
			continue
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			lines = append(lines, fmt.Sprintf("Prometheus 历史%s：结果解析失败。", item.Label))
			continue
		}
		count := toInt(payload["count"])
		resultType := firstNonEmptyText(toTrimString(payload["type"]), "unknown")
		lines = append(lines, fmt.Sprintf("Prometheus 历史%s：%s 查询返回 %d 条时间序列。", item.Label, resultType, count))
	}
	return lines
}

// summarizeESPayload 汇总 Elasticsearch 观测结果。
// 输入：es_log_query 输出 JSON 字符串。
// 输出：摘要文本。
func summarizeESPayload(raw string) string {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "Elasticsearch：结果解析失败。"
	}

	total := toInt(payload["total_hits"])
	index := toTrimString(payload["index"])
	if index == "" {
		return fmt.Sprintf("Elasticsearch：命中 %d 条日志。", total)
	}
	return fmt.Sprintf("Elasticsearch：索引 %s 命中 %d 条日志。", index, total)
}

// summarizeESIndexPayload 汇总 Elasticsearch 索引发现结果。
// 输入：discover_indices 输出 JSON 字符串。
// 输出：摘要文本。
func summarizeESIndexPayload(raw string) string {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "Elasticsearch 索引发现：结果解析失败。"
	}

	count := toInt(payload["count"])
	pattern := toTrimString(payload["pattern"])
	if pattern == "" {
		pattern = "*"
	}

	names := make([]string, 0, 3)
	if indices, ok := payload["indices"].([]any); ok {
		for _, item := range indices {
			indexEntry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := toTrimString(indexEntry["index"])
			if name == "" {
				continue
			}
			names = append(names, name)
			if len(names) >= 3 {
				break
			}
		}
	}

	if len(names) == 0 {
		return fmt.Sprintf("Elasticsearch 索引链路：模式 %s 下发现 %d 个索引。", pattern, count)
	}
	return fmt.Sprintf("Elasticsearch 索引链路：模式 %s 下发现 %d 个索引，样例：%s。", pattern, count, strings.Join(names, ", "))
}

// toTrimString 将任意值转为去空白字符串。
// 输入：任意值。
// 输出：去空白后的字符串。
func toTrimString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

// toInt 将任意数值转换为 int。
// 输入：任意值。
// 输出：整数，无法识别时返回 0。
func toInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
