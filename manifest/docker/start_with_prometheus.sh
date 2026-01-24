#!/bin/bash

echo "=========================================="
echo "启动 Docker Compose 服务 (包含 Prometheus)"
echo "=========================================="

# 进入正确的目录
cd "$(dirname "$0")"

# 检查docker-compose文件是否存在
if [ ! -f "docker-compose.yml" ]; then
    echo "错误: 找不到 docker-compose.yml 文件"
    exit 1
fi

# 停止已有的服务
echo "1. 停止现有服务..."
docker-compose down

# 创建数据目录
echo "2. 创建数据目录..."
mkdir -p volumes/prometheus/data
mkdir -p volumes/milvus
mkdir -p volumes/etcd
mkdir -p volumes/minio

# 设置权限
echo "3. 设置目录权限..."
sudo chmod -R 777 volumes/prometheus/ || chmod -R 755 volumes/prometheus/ || true
chmod -R 755 volumes/ || true

# 启动所有服务
echo "4. 启动所有服务..."
docker-compose up -d

# 等待服务启动
echo "5. 等待服务启动..."
sleep 10

# 检查服务状态
echo "6. 检查服务状态..."
docker-compose ps

echo ""
echo "=========================================="
echo "服务启动完成!"
echo "=========================================="
echo ""
echo "📊 Prometheus:  http://192.168.149.128:9090"
echo "🗃️  Milvus Attu: http://192.168.149.128:8000"
echo "📁 MinIO:       http://192.168.149.128:9001"
echo "🔧 Milvus:      192.168.149.128:19530"
echo ""
echo "使用以下命令查看日志:"
echo "  docker-compose logs -f prometheus"
echo "  docker-compose logs -f standalone"
echo ""
