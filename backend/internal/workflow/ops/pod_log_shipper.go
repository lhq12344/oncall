package ops

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	es "go_agent/utility/elasticsearch"

	"github.com/elastic/go-elasticsearch/v8"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultPodLogSyncInterval = 30 * time.Second
	defaultPodLogTailLines    = int64(200)
	defaultPodLogIndexPrefix  = "logs-k8s"
	podLogScannerBufferSize   = 1024 * 1024
	podLogBulkBatchSize       = 200
)

// PodLogShipperConfig 定义 Pod 日志写入 Elasticsearch 的配置。
// 输入：K8s 连接、命名空间、同步周期、首轮抓取行数、索引前缀、日志对象。
// 输出：供 NewPodLogShipper 使用的初始化配置。
type PodLogShipperConfig struct {
	KubeConfig  string
	Namespaces  []string
	Interval    time.Duration
	TailLines   int64
	IndexPrefix string
	Logger      *zap.Logger
}

// PodLogShipper 负责周期性抓取 K8s Pod 日志并写入 Elasticsearch。
type PodLogShipper struct {
	client      *kubernetes.Clientset
	logger      *zap.Logger
	namespaces  []string
	interval    time.Duration
	tailLines   int64
	indexPrefix string

	mu            sync.Mutex
	lastCollected map[string]time.Time
}

type podLogDocument struct {
	Index string
	ID    string
	Body  map[string]interface{}
}

type podLogContainer struct {
	Name string
	Type string
}

type podLogMetadata struct {
	App       string
	Workload  string
	OwnerKind string
	OwnerName string
}

// NewPodLogShipper 创建 Pod 日志同步器。
// 输入：PodLogShipperConfig。
// 输出：同步器实例和初始化错误。
func NewPodLogShipper(cfg *PodLogShipperConfig) (*PodLogShipper, error) {
	if cfg == nil {
		return nil, fmt.Errorf("pod log shipper config is required")
	}

	client, err := newKubernetesClientset(cfg.KubeConfig)
	if err != nil {
		return nil, err
	}

	namespaces := normalizeNamespaces(cfg.Namespaces)
	interval := cfg.Interval
	if interval <= 0 {
		interval = defaultPodLogSyncInterval
	}

	tailLines := cfg.TailLines
	if tailLines <= 0 {
		tailLines = defaultPodLogTailLines
	}

	indexPrefix := strings.TrimSpace(cfg.IndexPrefix)
	if indexPrefix == "" {
		indexPrefix = defaultPodLogIndexPrefix
	}

	return &PodLogShipper{
		client:        client,
		logger:        cfg.Logger,
		namespaces:    namespaces,
		interval:      interval,
		tailLines:     tailLines,
		indexPrefix:   indexPrefix,
		lastCollected: make(map[string]time.Time),
	}, nil
}

// Start 启动后台日志同步循环。
// 输入：ctx。
// 输出：无；内部周期性执行 CollectOnce。
func (s *PodLogShipper) Start(ctx context.Context) {
	if s == nil {
		return
	}

	s.collectAndLog(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.collectAndLog(ctx)
		}
	}
}

// CollectOnce 执行一次 Pod 日志采集并写入 Elasticsearch。
// 输入：ctx。
// 输出：本轮错误；局部 Pod 失败会记录日志并继续其他 Pod。
func (s *PodLogShipper) CollectOnce(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("pod log shipper is nil")
	}

	esClient := es.GetElasticsearch()
	if esClient == nil {
		return fmt.Errorf("elasticsearch client not initialized")
	}

	namespaces, err := s.resolveNamespaces(ctx)
	if err != nil {
		return err
	}

	totalDocs := 0
	totalPods := 0

	for _, namespace := range namespaces {
		pods, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("list pods in namespace %s failed: %w", namespace, err)
		}

		for i := range pods.Items {
			totalPods++
			count, collectErr := s.collectPodLogs(ctx, esClient, &pods.Items[i])
			if collectErr != nil {
				if s.logger != nil {
					s.logger.Warn("collect pod logs failed",
						zap.String("namespace", namespace),
						zap.String("pod", pods.Items[i].Name),
						zap.Error(collectErr))
				}
				continue
			}
			totalDocs += count
		}
	}

	if s.logger != nil {
		s.logger.Info("pod logs shipped to elasticsearch",
			zap.Int("namespaces", len(namespaces)),
			zap.Int("pods", totalPods),
			zap.Int("documents", totalDocs))
	}

	return nil
}

// collectAndLog 执行一次采集并输出错误日志。
// 输入：ctx。
// 输出：无。
func (s *PodLogShipper) collectAndLog(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, s.interval)
	defer cancel()

	if err := s.CollectOnce(runCtx); err != nil && s.logger != nil {
		s.logger.Warn("pod log shipping skipped",
			zap.Error(err))
	}
}

// resolveNamespaces 解析需要同步的命名空间集合。
// 输入：ctx。
// 输出：命名空间列表和错误。
func (s *PodLogShipper) resolveNamespaces(ctx context.Context) ([]string, error) {
	if len(s.namespaces) == 1 && s.namespaces[0] == "*" {
		list, err := s.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces failed: %w", err)
		}
		namespaces := make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			namespaces = append(namespaces, item.Name)
		}
		return namespaces, nil
	}

	return append([]string(nil), s.namespaces...), nil
}

// collectPodLogs 采集单个 Pod 下所有容器日志并写入 Elasticsearch。
// 输入：ctx、ES 客户端、Pod 对象。
// 输出：写入文档数量和错误。
func (s *PodLogShipper) collectPodLogs(ctx context.Context, esClient *elasticsearch.Client, pod *corev1.Pod) (int, error) {
	if pod == nil {
		return 0, nil
	}

	metadata, err := s.resolvePodLogMetadata(ctx, pod)
	if err != nil && s.logger != nil {
		s.logger.Debug("resolve pod log metadata failed",
			zap.String("namespace", pod.Namespace),
			zap.String("pod", pod.Name),
			zap.Error(err))
	}

	total := 0
	for _, container := range podContainersForLogging(pod) {
		count, err := s.collectContainerLogs(ctx, esClient, pod, container, metadata)
		if err != nil {
			if shouldIgnorePodLogError(err) {
				continue
			}
			return total, err
		}
		total += count
	}

	return total, nil
}

// collectContainerLogs 采集单个容器日志并写入 Elasticsearch。
// 输入：ctx、ES 客户端、Pod、容器描述。
// 输出：写入文档数量和错误。
func (s *PodLogShipper) collectContainerLogs(ctx context.Context, esClient *elasticsearch.Client, pod *corev1.Pod, container podLogContainer, metadata podLogMetadata) (int, error) {
	key := buildPodLogStateKey(pod, container)
	logOptions := &corev1.PodLogOptions{
		Container:  container.Name,
		Timestamps: true,
	}

	if lastSeen, ok := s.getLastCollected(key); ok {
		logOptions.SinceTime = &metav1.Time{Time: lastSeen}
	} else {
		tailLines := s.tailLines
		logOptions.TailLines = &tailLines
	}

	stream, err := s.client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions).Stream(ctx)
	if err != nil {
		return 0, err
	}
	defer stream.Close()

	documents, maxTimestamp, err := s.readPodLogDocuments(stream, pod, container, metadata)
	if err != nil {
		return 0, err
	}
	if len(documents) == 0 {
		return 0, nil
	}

	if err := s.bulkIndexDocuments(ctx, esClient, documents); err != nil {
		return 0, err
	}

	if !maxTimestamp.IsZero() {
		s.setLastCollected(key, maxTimestamp)
	}

	return len(documents), nil
}

// readPodLogDocuments 解析日志流并构造待写入 Elasticsearch 的文档集合。
// 输入：日志流、Pod、容器描述。
// 输出：文档列表、最大时间戳、错误。
func (s *PodLogShipper) readPodLogDocuments(reader io.Reader, pod *corev1.Pod, container podLogContainer, metadata podLogMetadata) ([]podLogDocument, time.Time, error) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, podLogScannerBufferSize)

	documents := make([]podLogDocument, 0, 32)
	maxTimestamp := time.Time{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		timestamp, message, level := parsePodLogLine(line, time.Now().UTC())
		if timestamp.After(maxTimestamp) {
			maxTimestamp = timestamp
		}

		document := buildPodLogDocument(s.indexPrefix, pod, container, metadata, timestamp, message, level, line)
		documents = append(documents, document)
	}

	if err := scanner.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("scan log stream failed: %w", err)
	}

	return documents, maxTimestamp, nil
}

// bulkIndexDocuments 批量写入 Elasticsearch。
// 输入：ctx、ES 客户端、文档列表。
// 输出：错误。
func (s *PodLogShipper) bulkIndexDocuments(ctx context.Context, esClient *elasticsearch.Client, documents []podLogDocument) error {
	for start := 0; start < len(documents); start += podLogBulkBatchSize {
		end := start + podLogBulkBatchSize
		if end > len(documents) {
			end = len(documents)
		}
		if err := s.bulkIndexBatch(ctx, esClient, documents[start:end]); err != nil {
			return err
		}
	}

	return nil
}

// bulkIndexBatch 写入一批 Elasticsearch 文档。
// 输入：ctx、ES 客户端、文档批次。
// 输出：错误。
func (s *PodLogShipper) bulkIndexBatch(ctx context.Context, esClient *elasticsearch.Client, documents []podLogDocument) error {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)

	for _, document := range documents {
		meta := map[string]map[string]string{
			"index": {
				"_index": document.Index,
				"_id":    document.ID,
			},
		}
		if err := encoder.Encode(meta); err != nil {
			return fmt.Errorf("encode bulk meta failed: %w", err)
		}
		if err := encoder.Encode(document.Body); err != nil {
			return fmt.Errorf("encode bulk document failed: %w", err)
		}
	}

	res, err := esClient.Bulk(
		bytes.NewReader(body.Bytes()),
		esClient.Bulk.WithContext(ctx),
		esClient.Bulk.WithRefresh("false"),
	)
	if err != nil {
		return fmt.Errorf("bulk index request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("bulk index returned error: %s", res.String())
	}

	var response struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int `json:"status"`
			Error  struct {
				Reason string `json:"reason"`
				Type   string `json:"type"`
			} `json:"error"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return fmt.Errorf("decode bulk response failed: %w", err)
	}

	if !response.Errors {
		return nil
	}

	for _, item := range response.Items {
		for _, result := range item {
			if result.Status < 300 {
				continue
			}
			return fmt.Errorf("bulk item failed: %s (%s)", strings.TrimSpace(result.Error.Reason), strings.TrimSpace(result.Error.Type))
		}
	}

	return fmt.Errorf("bulk index failed with unknown item error")
}

// getLastCollected 读取容器最后一次采集到的时间戳。
// 输入：状态键。
// 输出：时间戳和是否存在。
func (s *PodLogShipper) getLastCollected(key string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, ok := s.lastCollected[key]
	return value, ok
}

// setLastCollected 更新容器最后一次采集时间。
// 输入：状态键、时间戳。
// 输出：无。
func (s *PodLogShipper) setLastCollected(key string, timestamp time.Time) {
	if key == "" || timestamp.IsZero() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.lastCollected[key]; ok && existing.After(timestamp) {
		return
	}
	s.lastCollected[key] = timestamp.UTC()
}

// normalizeNamespaces 规范化配置中的命名空间列表。
// 输入：原始命名空间数组。
// 输出：去重后的命名空间数组。
func normalizeNamespaces(namespaces []string) []string {
	if len(namespaces) == 0 {
		return []string{"infra"}
	}

	seen := make(map[string]struct{}, len(namespaces))
	result := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if namespace == "*" {
			return []string{"*"}
		}
		if _, ok := seen[namespace]; ok {
			continue
		}
		seen[namespace] = struct{}{}
		result = append(result, namespace)
	}

	if len(result) == 0 {
		return []string{"infra"}
	}
	return result
}

// newKubernetesClientset 创建 Kubernetes ClientSet。
// 输入：kubeconfig 路径。
// 输出：ClientSet 和错误。
func newKubernetesClientset(kubeconfig string) (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error

	if strings.TrimSpace(kubeconfig) != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			config, err = rest.InClusterConfig()
			if err != nil {
				return nil, fmt.Errorf("build k8s config failed: %w", err)
			}
		}
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("build in-cluster k8s config failed: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create k8s client failed: %w", err)
	}
	return clientset, nil
}

// podContainersForLogging 返回需要采集日志的容器集合。
// 输入：Pod 对象。
// 输出：容器描述列表。
func podContainersForLogging(pod *corev1.Pod) []podLogContainer {
	if pod == nil {
		return nil
	}

	containers := make([]podLogContainer, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	for _, container := range pod.Spec.InitContainers {
		containers = append(containers, podLogContainer{
			Name: container.Name,
			Type: "init",
		})
	}
	for _, container := range pod.Spec.Containers {
		containers = append(containers, podLogContainer{
			Name: container.Name,
			Type: "app",
		})
	}
	return containers
}

// buildPodLogStateKey 构造容器日志采集状态键。
// 输入：Pod、容器描述。
// 输出：状态键。
func buildPodLogStateKey(pod *corev1.Pod, container podLogContainer) string {
	if pod == nil {
		return ""
	}

	return fmt.Sprintf("%s/%s/%s/%s/%s", pod.Namespace, pod.Name, pod.UID, container.Type, container.Name)
}

// parsePodLogLine 解析带时间戳的 K8s 日志行。
// 输入：原始日志行、兜底时间。
// 输出：日志时间、日志消息、日志级别。
func parsePodLogLine(line string, fallback time.Time) (time.Time, string, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback.UTC(), "", ""
	}

	timestamp := fallback.UTC()
	message := line

	if index := strings.IndexByte(line, ' '); index > 0 {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(line[:index])); err == nil {
			timestamp = parsed.UTC()
			message = strings.TrimSpace(line[index+1:])
		}
	}

	jsonMessage, jsonLevel := parseStructuredLogMessage(message)
	if strings.TrimSpace(jsonMessage) != "" {
		message = jsonMessage
	}

	level := normalizeLogLevel(jsonLevel)
	if level == "" {
		level = detectLogLevel(message)
	}

	return timestamp, message, level
}

// parseStructuredLogMessage 解析 JSON 日志行中的 message 和 level 字段。
// 输入：日志文本。
// 输出：日志消息和日志级别。
func parseStructuredLogMessage(message string) (string, string) {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "{") || !strings.HasSuffix(message, "}") {
		return message, ""
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(message), &payload); err != nil {
		return message, ""
	}

	level := normalizeLogLevel(firstNonEmptyLogText(
		toTrimmedString(payload["level"]),
		toTrimmedString(payload["severity"]),
		toTrimmedString(payload["log.level"]),
	))

	text := firstNonEmptyLogText(
		toTrimmedString(payload["message"]),
		toTrimmedString(payload["msg"]),
		toTrimmedString(payload["error"]),
	)
	if text == "" {
		text = message
	}

	return text, level
}

// detectLogLevel 根据文本内容推断日志级别。
// 输入：日志文本。
// 输出：标准化后的日志级别。
func detectLogLevel(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(lower, "fatal"), strings.HasPrefix(lower, "fatal"), strings.Contains(lower, "\"fatal\""):
		return "fatal"
	case strings.Contains(lower, "error"), strings.HasPrefix(lower, "error"), strings.Contains(lower, "\"error\""), strings.Contains(lower, "exception"):
		return "error"
	case strings.Contains(lower, "warn"), strings.HasPrefix(lower, "warn"), strings.Contains(lower, "\"warn\""), strings.Contains(lower, "warning"):
		return "warn"
	case strings.Contains(lower, "debug"), strings.HasPrefix(lower, "debug"), strings.Contains(lower, "\"debug\""):
		return "debug"
	case strings.Contains(lower, "info"), strings.HasPrefix(lower, "info"), strings.Contains(lower, "\"info\""):
		return "info"
	default:
		return ""
	}
}

// normalizeLogLevel 将日志级别归一化为 ES 查询友好的形式。
// 输入：原始级别字符串。
// 输出：归一化后的级别。
func normalizeLogLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "warning":
		return "warn"
	case "err":
		return "error"
	default:
		return level
	}
}

// buildPodLogDocument 构造 Elasticsearch 文档。
// 输入：索引前缀、Pod、容器描述、时间、消息、级别、原始日志。
// 输出：待写入的 ES 文档对象。
func buildPodLogDocument(indexPrefix string, pod *corev1.Pod, container podLogContainer, metadata podLogMetadata, timestamp time.Time, message, level, raw string) podLogDocument {
	index := buildPodLogIndexName(indexPrefix, timestamp)
	body := map[string]interface{}{
		"@timestamp": timestamp.UTC().Format(time.RFC3339Nano),
		"message":    strings.TrimSpace(message),
		"raw":        strings.TrimSpace(raw),
		"level":      strings.TrimSpace(level),
		"source":     "k8s_pod_logs",
		"app":        strings.TrimSpace(metadata.App),
		"workload":   strings.TrimSpace(metadata.Workload),
		"owner_kind": strings.TrimSpace(metadata.OwnerKind),
		"owner_name": strings.TrimSpace(metadata.OwnerName),
		"namespace":  pod.Namespace,
		"pod":        pod.Name,
		"container":  container.Name,
		"node_name":  pod.Spec.NodeName,
		"pod_uid":    string(pod.UID),
		"kubernetes": map[string]interface{}{
			"namespace":      pod.Namespace,
			"namespace_name": pod.Namespace,
			"pod_name":       pod.Name,
			"container_name": container.Name,
			"container_type": container.Type,
			"node_name":      pod.Spec.NodeName,
			"pod_uid":        string(pod.UID),
			"app":            strings.TrimSpace(metadata.App),
			"workload":       strings.TrimSpace(metadata.Workload),
			"owner_kind":     strings.TrimSpace(metadata.OwnerKind),
			"owner_name":     strings.TrimSpace(metadata.OwnerName),
		},
	}

	return podLogDocument{
		Index: index,
		ID:    buildPodLogDocumentID(pod, container, timestamp, raw),
		Body:  body,
	}
}

// buildPodLogIndexName 生成日志索引名称。
// 输入：索引前缀、时间戳。
// 输出：按天分桶的索引名。
func buildPodLogIndexName(indexPrefix string, timestamp time.Time) string {
	prefix := strings.TrimSpace(indexPrefix)
	if prefix == "" {
		prefix = defaultPodLogIndexPrefix
	}
	return fmt.Sprintf("%s-%s", prefix, timestamp.UTC().Format("2006.01.02"))
}

// buildPodLogDocumentID 构造 Elasticsearch 文档 ID。
// 输入：Pod、容器描述、时间戳、原始日志文本。
// 输出：稳定的文档 ID。
func buildPodLogDocumentID(pod *corev1.Pod, container podLogContainer, timestamp time.Time, raw string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, pod.Namespace)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, pod.Name)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, string(pod.UID))
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, container.Type)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, container.Name)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, timestamp.UTC().Format(time.RFC3339Nano))
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, strings.TrimSpace(raw))
	return hex.EncodeToString(h.Sum(nil))
}

// resolvePodLogMetadata 解析 Pod 的应用和工作负载元数据。
// 输入：ctx、Pod 对象。
// 输出：日志元数据和错误。
func (s *PodLogShipper) resolvePodLogMetadata(ctx context.Context, pod *corev1.Pod) (podLogMetadata, error) {
	metadata := podLogMetadata{
		App: extractPodAppName(pod),
	}

	ownerKind, ownerName, err := s.resolvePodOwner(ctx, pod)
	if err != nil {
		metadata.Workload = firstNonEmptyLogText(metadata.App, pod.Name)
		metadata.OwnerKind = ""
		metadata.OwnerName = ""
		return metadata, err
	}

	metadata.OwnerKind = ownerKind
	metadata.OwnerName = ownerName
	metadata.Workload = firstNonEmptyLogText(ownerName, metadata.App, pod.Name)
	return metadata, nil
}

// extractPodAppName 从常见标签中提取应用名。
// 输入：Pod 对象。
// 输出：应用名。
func extractPodAppName(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}

	candidateKeys := []string{
		"app",
		"app.kubernetes.io/name",
		"app.kubernetes.io/instance",
		"app.kubernetes.io/component",
		"k8s-app",
		"component",
	}
	for _, key := range candidateKeys {
		if value, ok := pod.Labels[key]; ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

// resolvePodOwner 解析 Pod 所属的逻辑工作负载。
// 输入：ctx、Pod 对象。
// 输出：owner kind、owner name、错误。
func (s *PodLogShipper) resolvePodOwner(ctx context.Context, pod *corev1.Pod) (string, string, error) {
	if pod == nil {
		return "", "", nil
	}

	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return "", "", nil
	}

	switch owner.Kind {
	case "ReplicaSet":
		if s == nil || s.client == nil {
			return owner.Kind, owner.Name, nil
		}
		replicaSet, err := s.client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
		if err != nil {
			return owner.Kind, owner.Name, err
		}
		parent := metav1.GetControllerOf(replicaSet)
		if parent != nil && strings.TrimSpace(parent.Name) != "" {
			return parent.Kind, parent.Name, nil
		}
		return owner.Kind, owner.Name, nil
	default:
		return owner.Kind, owner.Name, nil
	}
}

// shouldIgnorePodLogError 判断某些当前阶段不可读日志错误是否应跳过。
// 输入：错误对象。
// 输出：true 表示跳过该错误。
func shouldIgnorePodLogError(err error) bool {
	if err == nil {
		return false
	}

	text := strings.ToLower(err.Error())
	return strings.Contains(text, "containercreating") ||
		strings.Contains(text, "pod initializing") ||
		strings.Contains(text, "waiting to start") ||
		strings.Contains(text, "is terminated")
}

// toTrimmedString 将任意值转换为去空白字符串。
// 输入：任意值。
// 输出：字符串。
func toTrimmedString(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

// firstNonEmptyLogText 返回首个非空字符串。
// 输入：候选字符串列表。
// 输出：首个非空字符串。
func firstNonEmptyLogText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
