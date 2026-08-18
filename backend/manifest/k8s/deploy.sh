#!/bin/bash

set -euo pipefail

UNIFIED_SCRIPT="/home/lihaoqian/project/k8s/bin/k8s-stack.sh"

show_help() {
    cat <<EOF
oncall 的 K8s YAML 已迁移到统一目录：
  /home/lihaoqian/project/k8s

统一目录按组件名组织，不再按项目建目录。

当前脚本只保留状态查看入口：
  $0 status

统一入口示例：
  ${UNIFIED_SCRIPT} deploy prometheus
  ${UNIFIED_SCRIPT} deploy milvus
  ${UNIFIED_SCRIPT} start elasticsearch
  ${UNIFIED_SCRIPT} status oncall
EOF
}

ensure_unified_script() {
    if [ ! -x "${UNIFIED_SCRIPT}" ]; then
        echo "未找到统一脚本: ${UNIFIED_SCRIPT}" >&2
        exit 1
    fi
}

main() {
    case "${1:-help}" in
        status)
            ensure_unified_script
            exec "${UNIFIED_SCRIPT}" status oncall
            ;;
        start|stop|restart|cleanup)
            echo "oncall 的 Pod 生命周期管理已迁移到统一脚本：${UNIFIED_SCRIPT}" >&2
            echo "请改用 ${UNIFIED_SCRIPT} deploy|start|stop <组件名>（oncall 仍可作为兼容别名）" >&2
            exit 1
            ;;
        help|--help|-h)
            show_help
            ;;
        *)
            show_help
            exit 1
            ;;
    esac
}

main "$@"
