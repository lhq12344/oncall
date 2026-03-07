#!/bin/bash

# 自愈循环 API 测试脚本

BASE_URL="http://localhost:6872/api/v1"

echo "=== 自愈循环 API 测试 ==="
echo

# 1. 触发自愈
echo "1. 触发自愈流程"
echo "POST $BASE_URL/healing/trigger"
SESSION_ID=$(curl -s -X POST "$BASE_URL/healing/trigger" \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "inc-001",
    "type": "pod_crash_loop",
    "severity": "high",
    "title": "Pod Crash Loop Detected",
    "description": "Pod nginx-deployment-xxx is in CrashLoopBackOff state"
  }' | jq -r '.session_id')

echo "Session ID: $SESSION_ID"
echo

# 2. 查询自愈状态
echo "2. 查询自愈状态"
echo "GET $BASE_URL/healing/status?session_id=$SESSION_ID"
curl -s "$BASE_URL/healing/status?session_id=$SESSION_ID" | jq '.'
echo

# 3. 查询所有活跃会话
echo "3. 查询所有活跃会话"
echo "GET $BASE_URL/healing/status"
curl -s "$BASE_URL/healing/status" | jq '.'
echo

# 4. 触发另一个自愈流程（高 CPU）
echo "4. 触发另一个自愈流程（高 CPU）"
echo "POST $BASE_URL/healing/trigger"
curl -s -X POST "$BASE_URL/healing/trigger" \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "inc-002",
    "type": "high_cpu",
    "severity": "medium",
    "title": "High CPU Usage",
    "description": "CPU usage exceeds 80% for 10 minutes"
  }' | jq '.'
echo

# 5. 再次查询所有活跃会话
echo "5. 再次查询所有活跃会话"
echo "GET $BASE_URL/healing/status"
curl -s "$BASE_URL/healing/status" | jq '.'
echo

# 6. 查询监控数据（包含熔断器状态）
echo "6. 查询监控数据"
echo "GET $BASE_URL/monitoring"
curl -s "$BASE_URL/monitoring" | jq '.'
echo

echo "=== 测试完成 ==="
