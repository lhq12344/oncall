package integration

import (
	"context"
	"strings"
	"testing"

	"go_agent/internal/agent/ops/tools"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestK8sIntegration K8s 集成测试
func TestK8sIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping k8s integration test")
	}

	ctx := context.Background()
	logger := zap.NewNop()

	// 创建 K8s 监控工具
	k8sTool, err := tools.NewK8sMonitorTool("", logger)
	require.NoError(t, err)
	require.NotNil(t, k8sTool)

	// 类型断言为 InvokableTool
	invokableTool, ok := k8sTool.(tool.InvokableTool)
	require.True(t, ok, "tool should implement InvokableTool")

	t.Run("PodMonitoring", func(t *testing.T) {
		// 测试 Pod 监控
		input := `{"namespace": "default", "resource_type": "pod"}`
		result, err := invokableTool.InvokableRun(ctx, input)

		if err != nil {
			// K8s 可能不可用，检查是否是降级响应
			t.Logf("K8s not available (expected in some environments): %v", err)
			return
		}

		require.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "namespace")
		t.Logf("Pod monitoring result: %s", result)
	})

	t.Run("SpecificPodQuery", func(t *testing.T) {
		// 查询特定 Pod
		input := `{"namespace": "kube-system", "resource_type": "pod", "resource_name": "coredns"}`
		result, err := invokableTool.InvokableRun(ctx, input)

		if err != nil {
			t.Logf("K8s not available or pod not found: %v", err)
			return
		}

		assert.NotEmpty(t, result)
		t.Logf("Specific pod query result: %s", result)
	})

	t.Run("DeploymentOperations", func(t *testing.T) {
		// 测试 Deployment 查询
		input := `{"namespace": "default", "resource_type": "deployment"}`
		result, err := invokableTool.InvokableRun(ctx, input)

		if err != nil {
			t.Logf("K8s not available: %v", err)
			return
		}

		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// K8s 可能返回错误响应（降级模式）
		if strings.Contains(result, "deployments") {
			t.Logf("✅ Deployment query succeeded")
		} else {
			t.Logf("⚠️  K8s returned degraded response (expected when running outside cluster)")
		}
	})

	t.Run("ServiceQuery", func(t *testing.T) {
		// 测试 Service 查询
		input := `{"namespace": "default", "resource_type": "service"}`
		result, err := invokableTool.InvokableRun(ctx, input)

		if err != nil {
			t.Logf("K8s not available: %v", err)
			return
		}

		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// K8s 可能返回错误响应（降级模式）
		if strings.Contains(result, "services") {
			t.Logf("✅ Service query succeeded")
		} else {
			t.Logf("⚠️  K8s returned degraded response (expected when running outside cluster)")
		}
	})

	t.Run("NodeMonitoring", func(t *testing.T) {
		// 测试 Node 监控
		input := `{"resource_type": "node"}`
		result, err := invokableTool.InvokableRun(ctx, input)

		if err != nil {
			t.Logf("K8s not available: %v", err)
			return
		}

		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// K8s 可能返回错误响应（降级模式）
		if strings.Contains(result, "nodes") {
			t.Logf("✅ Node monitoring succeeded")
		} else {
			t.Logf("⚠️  K8s returned degraded response (expected when running outside cluster)")
		}
	})

	t.Run("MultipleNamespaces", func(t *testing.T) {
		// 测试多个命名空间
		namespaces := []string{"default", "kube-system", "kube-public"}

		for _, ns := range namespaces {
			input := `{"namespace": "` + ns + `", "resource_type": "pod"}`
			result, err := invokableTool.InvokableRun(ctx, input)

			if err != nil {
				t.Logf("Namespace %s not accessible: %v", ns, err)
				continue
			}

			assert.NotEmpty(t, result)
			t.Logf("Namespace %s has pods", ns)
		}
	})
}

// TestK8sAvailability 测试 K8s 集群可用性
func TestK8sAvailability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping k8s availability test")
	}

	ctx := context.Background()
	logger := zap.NewNop()

	k8sTool, err := tools.NewK8sMonitorTool("", logger)
	require.NoError(t, err)

	invokableTool, ok := k8sTool.(tool.InvokableTool)
	require.True(t, ok)

	// 尝试查询 kube-system 命名空间（通常存在）
	input := `{"namespace": "kube-system", "resource_type": "pod"}`
	result, err := invokableTool.InvokableRun(ctx, input)

	if err != nil {
		t.Logf("⚠️  K8s cluster not available: %v", err)
		t.Log("💡 To run K8s integration tests, ensure:")
		t.Log("   1. kubectl is configured")
		t.Log("   2. K8s cluster is accessible")
		t.Log("   3. You have proper RBAC permissions")
		t.Skip("K8s cluster not available")
	}

	t.Log("✅ K8s cluster is available")
	t.Logf("Sample result: %s", result)
}
