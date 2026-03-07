#!/bin/bash

# Prometheus & Milvus 部署管理脚本
# 用法: ./deploy.sh [start|stop|restart|status]

set -e

NAMESPACE="infra"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROMETHEUS_DIR="${SCRIPT_DIR}/prometheus"
MILVUS_DIR="${SCRIPT_DIR}/milvus"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查 kubectl 是否可用
check_kubectl() {
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl 未安装或不在 PATH 中"
        exit 1
    fi
}

# 确保命名空间存在
ensure_namespace() {
    if ! kubectl get namespace ${NAMESPACE} &> /dev/null; then
        log_info "创建命名空间 ${NAMESPACE}"
        kubectl create namespace ${NAMESPACE}
    fi
}

# 部署 Prometheus
deploy_prometheus() {
    log_info "部署 Prometheus..."
    kubectl apply -f ${PROMETHEUS_DIR}/configmap.yaml
    kubectl apply -f ${PROMETHEUS_DIR}/deployment.yaml
    log_info "Prometheus 部署完成"
}

# 部署 Milvus 及其依赖
deploy_milvus() {
    log_info "部署 Milvus 依赖 (etcd)..."
    kubectl apply -f ${MILVUS_DIR}/etcd.yaml

    log_info "等待 etcd 就绪..."
    kubectl wait --for=condition=ready pod -l app=etcd -n ${NAMESPACE} --timeout=120s || log_warn "etcd 启动超时"

    log_info "创建 MinIO Service（指向现有 MinIO）..."
    kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: minio
  namespace: ${NAMESPACE}
spec:
  type: ClusterIP
  ports:
    - port: 9000
      targetPort: 9000
      name: api
    - port: 9001
      targetPort: 9001
      name: console
  selector:
    app: minio
EOF

    log_info "部署 Milvus..."
    kubectl apply -f ${MILVUS_DIR}/configmap.yaml
    kubectl apply -f ${MILVUS_DIR}/milvus.yaml

    log_info "部署 Milvus Attu (WebUI)..."
    kubectl apply -f ${MILVUS_DIR}/attu.yaml
    log_info "Milvus 部署完成"
}

# 启动所有服务
start_all() {
    log_info "开始部署 Prometheus 和 Milvus 到命名空间 ${NAMESPACE}"
    check_kubectl
    ensure_namespace

    deploy_prometheus
    deploy_milvus

    log_info "所有服务部署完成！"
    log_info "等待 Pod 就绪..."
    sleep 5
    show_status

    log_info ""
    log_info "访问地址:"
    log_info "  Prometheus: http://<node-ip>:30090"
    log_info "  Milvus gRPC: <node-ip>:31953"
    log_info "  Milvus Metrics: http://<node-ip>:30091/metrics"
    log_info "  MinIO Console: http://<node-ip>:30901 (minioadmin/minioadmin)"
    log_info "  Milvus Attu WebUI: http://<node-ip>:30900"
}

# 停止所有服务
stop_all() {
    log_info "停止 Prometheus 和 Milvus..."
    check_kubectl

    log_info "删除 Prometheus..."
    kubectl delete -f ${PROMETHEUS_DIR}/deployment.yaml --ignore-not-found=true
    kubectl delete -f ${PROMETHEUS_DIR}/configmap.yaml --ignore-not-found=true

    log_info "删除 Milvus..."
    kubectl delete -f ${MILVUS_DIR}/milvus.yaml --ignore-not-found=true
    kubectl delete -f ${MILVUS_DIR}/configmap.yaml --ignore-not-found=true
    kubectl delete -f ${MILVUS_DIR}/etcd.yaml --ignore-not-found=true

    log_info "删除 MinIO Service..."
    log_info "删除 Milvus Attu..."
    kubectl delete -f ${MILVUS_DIR}/attu.yaml --ignore-not-found=true
    kubectl delete svc minio -n ${NAMESPACE} --ignore-not-found=true

    log_info "所有服务已停止"
}

# 重启所有服务
restart_all() {
    log_info "重启 Prometheus 和 Milvus..."
    stop_all
    sleep 5
    start_all
}

# 显示服务状态
show_status() {
    log_info "服务状态:"
    echo ""
    kubectl get pods -n ${NAMESPACE} -l 'app in (prometheus,milvus,etcd)' -o wide
    echo ""
    log_info "服务端口:"
    kubectl get svc -n ${NAMESPACE} -l 'app in (prometheus,milvus)'
    echo ""
    log_info "MinIO (复用现有):"
    kubectl get pods -n ${NAMESPACE} -l app=minio
    kubectl get svc -n ${NAMESPACE} minio-nodeport
}

# 清理所有资源（包括 PVC）
cleanup_all() {
    log_warn "警告: 此操作将删除所有数据（包括 PVC）！"
    read -p "确认删除? (yes/no): " confirm
    if [ "$confirm" != "yes" ]; then
        log_info "取消操作"
        exit 0
    fi

    stop_all

    log_info "删除 PVC..."
    kubectl delete pvc -n ${NAMESPACE} prometheus-data milvus-data etcd-data --ignore-not-found=true

    log_info "清理完成（MinIO 数据保留）"
}

# 主函数
main() {
    case "${1:-}" in
        start)
            start_all
            ;;
        stop)
            stop_all
            ;;
        restart)
            restart_all
            ;;
        status)
            show_status
            ;;
        cleanup)
            cleanup_all
            ;;
        *)
            echo "用法: $0 {start|stop|restart|status|cleanup}"
            echo ""
            echo "命令说明:"
            echo "  start   - 部署 Prometheus 和 Milvus"
            echo "  stop    - 停止所有服务（保留数据）"
            echo "  restart - 重启所有服务"
            echo "  status  - 查看服务状态"
            echo "  cleanup - 删除所有资源（包括数据）"
            exit 1
            ;;
    esac
}

main "$@"
