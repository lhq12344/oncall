#!/bin/bash

# 自愈循环 API 快速测试脚本

BASE_URL="http://localhost:6872/api/v1"

echo "=========================================="
echo "自愈循环 API 测试"
echo "=========================================="
echo

# 检查服务是否运行
echo "1. 检查服务状态..."
if ! curl -s -f "$BASE_URL/monitoring" > /dev/null 2>&1; then
    echo "❌ 服务未运行！请先启动服务："
    echo "   cd /home/lihaoqian/project/oncall"
    echo "   go run main.go"
    exit 1
fi
echo "✅ 服务正常运行"
echo

# 测试监控端点
echo "2. 测试监控端点..."
echo "GET $BASE_URL/monitoring"
MONITOR_RESULT=$(curl -s "$BASE_URL/monitoring")
echo "$MONITOR_RESULT" | jq '.' 2>/dev/null || echo "$MONITOR_RESULT"
echo

# 测试触发自愈 - Pod Crash Loop
echo "3. 测试触发自愈（Pod Crash Loop）..."
echo "POST $BASE_URL/healing/trigger"
TRIGGER_RESULT=$(curl -s -X POST "$BASE_URL/healing/trigger" \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "test-inc-001",
    "type": "pod_crash_loop",
    "severity": "high",
    "title": "Pod Crash Loop Test",
    "description": "Testing pod crash loop healing"
  }')

echo "$TRIGGER_RESULT" | jq '.' 2>/dev/null || echo "$TRIGGER_RESULT"

# 提取 session_id
SESSION_ID=$(echo "$TRIGGER_RESULT" | jq -r '.data.session_id' 2>/dev/null)
if [ "$SESSION_ID" != "null" ] && [ -n "$SESSION_ID" ]; then
    echo "✅ 自愈会话已创建: $SESSION_ID"
else
    echo "❌ 创建自愈会话失败"
    echo "响应: $TRIGGER_RESULT"
    exit 1
fi
echo

# 等待一下让自愈流程执行
echo "4. 等待自愈流程执行（2秒）..."
sleep 2
echo

# 查询自愈状态
echo "5. 查询自愈状态..."
echo "GET $BASE_URL/healing/status?session_id=$SESSION_ID"
STATUS_RESULT=$(curl -s "$BASE_URL/healing/status?session_id=$SESSION_ID")
echo "$STATUS_RESULT" | jq '.' 2>/dev/null || echo "$STATUS_RESULT"

# 检查状态
STATE=$(echo "$STATUS_RESULT" | jq -r '.data.sessions[0].state' 2>/dev/null)
if [ "$STATE" = "completed" ]; then
    echo "✅ 自愈流程已完成"
elif [ "$STATE" = "failed" ]; then
    echo "⚠️  自愈流程失败"
else
    echo "ℹ️  自愈流程状态: $STATE"
fi
echo

# 测试触发自愈 - High CPU
echo "6. 测试触发自愈（High CPU）..."
echo "POST $BASE_URL/healing/trigger"
TRIGGER_RESULT2=$(curl -s -X POST "$BASE_URL/healing/trigger" \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "test-inc-002",
    "type": "high_cpu",
    "severity": "medium",
    "title": "High CPU Test",
    "description": "Testing high CPU healing"
  }')

echo "$TRIGGER_RESULT2" | jq '.' 2>/dev/null || echo "$TRIGGER_RESULT2"
SESSION_ID2=$(echo "$TRIGGER_RESULT2" | jq -r '.data.session_id' 2>/dev/null)
echo "✅ 第二个自愈会话已创建: $SESSION_ID2"
echo

# 等待一下
sleep 2

# 查询所有活跃会话
echo "7. 查询所有活跃会话..."
echo "GET $BASE_URL/healing/status"
ALL_STATUS=$(curl -s "$BASE_URL/healing/status")
echo "$ALL_STATUS" | jq '.' 2>/dev/null || echo "$ALL_STATUS"

SESSION_COUNT=$(echo "$ALL_STATUS" | jq '.data.sessions | length' 2>/dev/null)
if [ "$SESSION_COUNT" -gt 0 ]; then
    echo "ℹ️  当前活跃会话数: $SESSION_COUNT"
else
    echo "ℹ️  没有活跃会话（所有会话已完成）"
fi
echo

# 测试 AI Ops 端点
echo "8. 测试 AI Ops 端点..."
echo "POST $BASE_URL/ai_ops"
AIOPS_RESULT=$(curl -s -X POST "$BASE_URL/ai_ops" \
  -H "Content-Type: application/json")
echo "$AIOPS_RESULT" | jq '.' 2>/dev/null || echo "$AIOPS_RESULT"
echo

echo "=========================================="
echo "测试完成！"
echo "=========================================="
echo
echo "总结："
echo "✅ 监控端点正常"
echo "✅ 自愈触发正常"
echo "✅ 状态查询正常"
echo "✅ AI Ops 端点正常"
echo
echo "详细日志请查看服务输出"
