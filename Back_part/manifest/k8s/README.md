# Kubernetes 清单迁移说明

`oncall` 的 K8s YAML 已迁移到统一目录：

```bash
/home/lihaoqian/project/k8s
```

## 当前入口

- 统一部署/启停脚本：`/home/lihaoqian/project/k8s/bin/k8s-stack.sh`
- 当前目录仅保留状态查看脚本：`manifest/k8s/deploy.sh`
- 统一目录按组件名拆分，不再按 `oncall/clouddisk_v2` 建目录

## 常用命令

```bash
/home/lihaoqian/project/k8s/bin/k8s-stack.sh deploy prometheus
/home/lihaoqian/project/k8s/bin/k8s-stack.sh deploy milvus
/home/lihaoqian/project/k8s/bin/k8s-stack.sh start elasticsearch
/home/lihaoqian/project/k8s/bin/k8s-stack.sh status oncall
```

## 说明

- 统一清单是唯一 YAML 来源，原仓库不再保存可执行副本
- `oncall` 只是脚本里的兼容性组件集合别名，不对应目录层级
- `stop` 只关闭工作负载，不删除 PVC、ConfigMap、Secret、Namespace
- `deploy.sh status` 仍可查看 `oncall` 相关资源状态
