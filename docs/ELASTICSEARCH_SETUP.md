# Elasticsearch 部署指南

## 使用 Docker 快速部署

### 单节点开发环境

```bash
docker run -d \
  --name elasticsearch \
  -p 9200:9200 \
  -p 9300:9300 \
  -e "discovery.type=single-node" \
  -e "xpack.security.enabled=false" \
  -e "ES_JAVA_OPTS=-Xms512m -Xmx512m" \
  docker.elastic.co/elasticsearch/elasticsearch:8.11.0
```

### 验证部署

```bash
curl http://localhost:9200
```

应该返回类似：
```json
{
  "name" : "...",
  "cluster_name" : "docker-cluster",
  "version" : {
    "number" : "8.11.0",
    ...
  }
}
```

## 使用 Kubernetes 部署

### 创建 Elasticsearch 部署

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: elasticsearch
  namespace: infra
spec:
  replicas: 1
  selector:
    matchLabels:
      app: elasticsearch
  template:
    metadata:
      labels:
        app: elasticsearch
    spec:
      containers:
      - name: elasticsearch
        image: docker.elastic.co/elasticsearch/elasticsearch:8.11.0
        ports:
        - containerPort: 9200
          name: http
        - containerPort: 9300
          name: transport
        env:
        - name: discovery.type
          value: "single-node"
        - name: xpack.security.enabled
          value: "false"
        - name: ES_JAVA_OPTS
          value: "-Xms512m -Xmx512m"
        resources:
          requests:
            memory: "1Gi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
---
apiVersion: v1
kind: Service
metadata:
  name: elasticsearch
  namespace: infra
spec:
  type: NodePort
  ports:
  - port: 9200
    targetPort: 9200
    nodePort: 30920
    name: http
  selector:
    app: elasticsearch
```

部署：
```bash
kubectl apply -f elasticsearch.yaml
```

验证：
```bash
kubectl get pods -n infra | grep elasticsearch
curl http://localhost:30920
```

## 配置 oncall agent

在 `manifest/config/config.yaml` 中配置：

```yaml
elasticsearch:
  addresses:
    - "http://localhost:30920"  # 集群外访问
    # - "http://elasticsearch.infra.svc:9200"  # K8s 集群内访问
  username: ""  # 用户名（可选）
  password: ""  # 密码（可选）
  timeout: "10s"
  tls_skip: true  # 开发环境跳过 TLS 验证
```

## 创建测试日志索引

```bash
# 创建索引
curl -X PUT "http://localhost:30920/logs-2024.03" -H 'Content-Type: application/json' -d'
{
  "mappings": {
    "properties": {
      "@timestamp": { "type": "date" },
      "level": { "type": "keyword" },
      "message": { "type": "text" },
      "service": { "type": "keyword" },
      "pod": { "type": "keyword" },
      "namespace": { "type": "keyword" }
    }
  }
}'

# 插入测试日志
curl -X POST "http://localhost:30920/logs-2024.03/_doc" -H 'Content-Type: application/json' -d'
{
  "@timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%S.000Z)'",
  "level": "error",
  "message": "Failed to connect to database: connection timeout",
  "service": "api-server",
  "pod": "api-server-abc123",
  "namespace": "default"
}'

# 查询日志
curl -X GET "http://localhost:30920/logs-2024.03/_search?pretty" -H 'Content-Type: application/json' -d'
{
  "query": {
    "match": {
      "level": "error"
    }
  }
}'
```

## 生产环境建议

1. **启用安全认证**：
   - 设置 `xpack.security.enabled=true`
   - 配置用户名和密码
   - 使用 TLS 加密

2. **集群部署**：
   - 至少 3 个节点
   - 配置主节点、数据节点、协调节点

3. **资源配置**：
   - 内存：至少 2GB，推荐 4-8GB
   - CPU：至少 2 核
   - 磁盘：SSD，根据日志量规划

4. **索引管理**：
   - 使用索引生命周期管理（ILM）
   - 定期清理旧日志
   - 配置索引模板

5. **监控**：
   - 使用 Kibana 可视化
   - 监控集群健康状态
   - 设置告警规则
