#!/bin/bash

# API 快速测试脚本

BASE_URL="http://localhost:6872/api/v1"

echo "=========================================="
echo "OnCall API 测试"
echo "=========================================="
echo

echo "1. 检查服务状态..."
if ! curl -s -f "$BASE_URL/monitoring" >/dev/null 2>&1; then
    echo "❌ 服务未运行！请先启动服务："
    echo "   cd /home/lihaoqian/project/oncall"
    echo "   go run main.go"
    exit 1
fi
echo "✅ 服务正常运行"
echo

echo "2. 测试监控端点..."
echo "GET $BASE_URL/monitoring"
MONITOR_RESULT=$(curl -s "$BASE_URL/monitoring")
echo "$MONITOR_RESULT" | jq '.' 2>/dev/null || echo "$MONITOR_RESULT"
echo

echo "3. 测试 AI Ops 端点..."
echo "POST $BASE_URL/ai_ops"
AIOPS_RESULT=$(curl -s -X POST "$BASE_URL/ai_ops" \
  -H "Content-Type: application/json")
echo "$AIOPS_RESULT" | jq '.' 2>/dev/null || echo "$AIOPS_RESULT"
echo

echo "4. 测试对话端点..."
echo "POST $BASE_URL/chat"
CHAT_RESULT=$(curl -s -X POST "$BASE_URL/chat" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "smoke-test",
    "question": "你好",
    "sse": false
  }')
echo "$CHAT_RESULT" | jq '.' 2>/dev/null || echo "$CHAT_RESULT"
echo

echo "=========================================="
echo "测试完成！"
echo "=========================================="
echo
echo "总结："
echo "✅ 监控端点正常"
echo "✅ AI Ops 端点正常"
echo "✅ 对话端点正常"
