# Docker Compose 环境启动指南

本目录包含了完整的开发环境配置，包括 Milvus、Prometheus 等服务。

## 服务列表

- **Prometheus**: 监控和告警系统 (端口: 9090)
- **Milvus**: 向量数据库 (端口: 19530)
- **Attu**: Milvus 管理界面 (端口: 8000)
- **MinIO**: 对象存储服务 (端口: 9000, 9001)
- **etcd**: 键值存储服务 (端口: 2379)

## 快速启动

### 方法1: 使用启动脚本
```bash
./start_with_prometheus.sh
```

### 方法2: 手动启动
```bash
# 启动所有服务
docker-compose up -d

# 查看服务状态
docker-compose ps

# 查看日志
docker-compose logs -f prometheus
```

## 访问地址

启动成功后，可以通过以下地址访问各项服务：

- **Prometheus Web UI**: http://192.168.149.128:9090
  - 主页面：http://192.168.149.128:9090/graph
  - 告警页面：http://192.168.149.128:9090/alerts
  - 配置页面：http://192.168.149.128:9090/config
  - 目标状态：http://192.168.149.128:9090/targets

### 监控目标
- Prometheus 自身监控
## Prometheus Web UI 功能

### 主要页面
1. **Graph (查询页面)**: 执行PromQL查询和图表展示
2. **Alerts (告警页面)**: 查看当前触发的告警
3. **Config (配置页面)**: 查看Prometheus配置
4. **Targets (目标页面)**: 查看监控目标的状态

### 常用查询示例
```promql
# 查看所有告警
ALERTS
# 查看服务状态
up
# 查看HTTP请求率
rate(http_requests_total[5m])
```

- `volumes/milvus/`: Milvus 数据
- `volumes/etcd/`: etcd 数据
- `volumes/minio/`: MinIO 数据

## 常用命令

```bash
# 停止所有服务
docker-compose down

# 重启特定服务
docker-compose restart prometheus

# 查看特定服务日志
docker-compose logs -f prometheus

# 更新服务配置后重新加载
docker-compose up -d --force-recreate prometheus

# 清理所有数据(谨慎使用)
docker-compose down -v
sudo rm -rf volumes/
```

## 故障排除

### Prometheus 无法启动
1. 检查配置文件语法: `docker run --rm -v $(pwd)/prometheus.yml:/prometheus.yml prom/prometheus promtool check config /prometheus.yml`
2. 检查告警规则语法: `docker run --rm -v $(pwd)/alert_rules.yml:/alert_rules.yml prom/prometheus promtool check rules /alert_rules.yml`

### 端口冲突
如果端口被占用，可以修改 docker-compose.yml 中的端口映射。

### 权限问题
确保数据目录有正确的权限:
```bash
sudo chmod -R 755 volumes/
sudo chown -R $USER:$USER volumes/
```

## 自定义配置

### 添加新的监控目标
编辑 `prometheus.yml` 文件，在 `scrape_configs` 部分添加新的 job。

### 添加新的告警规则
编辑 `alert_rules.yml` 文件，添加新的告警规则。

### 修改端口
1. **权限问题** (最常见):
   ```bash
   # 修复Prometheus数据目录权限
   sudo mkdir -p volumes/prometheus/data
   sudo chmod -R 777 volumes/prometheus/
   docker-compose restart prometheus
   ```

2. **配置文件语法检查**:
   ```bash
   docker run --rm -v $(pwd)/prometheus.yml:/prometheus.yml prom/prometheus promtool check config /prometheus.yml
   ```

3. **告警规则语法检查**:
   ```bash
   docker run --rm -v $(pwd)/alert_rules.yml:/alert_rules.yml prom/prometheus promtool check rules /alert_rules.yml
   ```

### Prometheus UI 无法访问
1. 确认容器正在运行: `docker-compose ps prometheus`
2. 检查容器日志: `docker-compose logs prometheus`
3. 验证端口: `curl http://192.168.149.128:9090`
4. 检查防火墙设置
