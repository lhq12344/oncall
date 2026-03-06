# Kubernetes 部署文档

本目录包含 Prometheus 和 Milvus 的 Kubernetes 部署配置。

## 目录结构

```
k8s/
├── deploy.sh                    # 部署管理脚本
├── prometheus/
│   ├── configmap.yaml          # Prometheus 配置和告警规则
│   └── deployment.yaml         # Prometheus 部署、服务、RBAC
└── milvus/
    ├── configmap.yaml          # Milvus 配置
    ├── etcd.yaml               # etcd 部署
    ├── minio.yaml              # MinIO 对象存储
    └── milvus.yaml             # Milvus 向量数据库
```

## 快速开始

### 部署所有服务

```bash
cd /home/lihaoqian/project/oncall/manifest/k8s
./deploy.sh start
```

### 查看服务状态

```bash
./deploy.sh status
```

### 停止所有服务

```bash
./deploy.sh stop
```

### 重启服务

```bash
./deploy.sh restart
```

### 清理所有资源（包括数据）

```bash
./deploy.sh cleanup
```

## 服务访问

部署完成后，可以通过以下地址访问服务：

| 服务 | 地址 | 说明 |
|------|------|------|
| Prometheus Web UI | `http://<node-ip>:30090` | 监控查询界面 |
| Milvus gRPC | `<node-ip>:31953` | 向量数据库 gRPC 端口 |
| Milvus Metrics | `http://<node-ip>:30091/metrics` | Prometheus 指标 |
| MinIO Console | `http://<node-ip>:30901` | 对象存储管理界面 (minioadmin/minioadmin) |

## Prometheus 配置

### 监控目标

Prometheus 自动监控以下目标：

1. **Kubernetes Pods** - 自动发现带 `prometheus.io/scrape: "true"` 注解的 Pod
2. **Nacos** - `nacos.infra.svc:8848`
3. **MySQL** - `mysql-svc.infra.svc:3306`
4. **Milvus** - `milvus.infra.svc:9091`

### 告警规则

内置告警规则包括：

- `ServiceDown` - 服务下线告警
- `PodRestartingTooOften` - Pod 频繁重启
- `HighErrorRate` - 接口错误率过高
- `MilvusConnectionFailed` - Milvus 连接失败
- `NacosServiceDown` - Nacos 服务异常

### 修改配置

编辑 `prometheus/configmap.yaml` 后，重新应用配置：

```bash
kubectl apply -f prometheus/configmap.yaml
kubectl rollout restart deployment/prometheus -n infra
```

## Milvus 配置

### 依赖服务

Milvus 依赖以下服务：

- **etcd** - 元数据存储
- **MinIO** - 对象存储（存储向量数据）

### 连接信息

在应用中连接 Milvus：

```go
// 集群内访问
address := "milvus.infra.svc:19530"

// 集群外访问
address := "<node-ip>:31953"
```

### 数据持久化

所有服务使用 PVC 持久化数据：

- `prometheus-data` - 10Gi
- `milvus-data` - 20Gi
- `minio-data` - 20Gi
- `etcd-data` - 5Gi

## 资源配置

### Prometheus

- CPU: 200m (request) / 1000m (limit)
- Memory: 512Mi (request) / 2Gi (limit)
- Storage: 10Gi (保留 7 天数据)

### Milvus

- CPU: 500m (request) / 2000m (limit)
- Memory: 1Gi (request) / 4Gi (limit)
- Storage: 20Gi

### MinIO

- CPU: 200m (request) / 1000m (limit)
- Memory: 512Mi (request) / 2Gi (limit)
- Storage: 20Gi

### etcd

- CPU: 100m (request) / 500m (limit)
- Memory: 256Mi (request) / 1Gi (limit)
- Storage: 5Gi

## 故障排查

### 查看 Pod 日志

```bash
# Prometheus
kubectl logs -n infra -l app=prometheus -f

# Milvus
kubectl logs -n infra -l app=milvus -f

# etcd
kubectl logs -n infra -l app=etcd -f

# MinIO
kubectl logs -n infra -l app=minio -f
```

### 查看 Pod 详情

```bash
kubectl describe pod -n infra <pod-name>
```

### 常见问题

1. **Milvus 启动失败**
   - 检查 etcd 和 MinIO 是否正常运行
   - 查看 Milvus 日志确认错误信息

2. **Prometheus 无法抓取指标**
   - 检查目标服务是否暴露 `/metrics` 端点
   - 验证网络连通性和 RBAC 权限

3. **PVC 无法绑定**
   - 确认 `local-path` StorageClass 存在
   - 检查节点存储空间是否充足

## 与 oncall agent 集成

部署完成后，更新 oncall 配置文件 `manifest/config/config.yaml`：

```yaml
prometheus:
  url: "http://prometheus.infra.svc:9090"

milvus:
  address: "milvus.infra.svc:19530"
```

重启 oncall agent 使配置生效。
