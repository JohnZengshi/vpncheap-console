#!/bin/bash
# 启动vpncheap-console，附带前置检查：VPNCheap是否在跑、Clash API是否可达。
# make run 也能跑，这个脚本只是多了失败时给出清楚原因，而不是让浏览器空转。
set -euo pipefail

cd "$(dirname "$0")"

ADDR="${ADDR:-127.0.0.1:18090}"
CLASH="${CLASH:-http://127.0.0.1:9090}"

echo "检查VPNCheap是否在运行..."
if ! pgrep -x VPNCheap > /dev/null 2>&1; then
    echo "✗ VPNCheap.app 未运行。请先打开VPNCheap（菜单栏图标即可，不需要打开主窗口）。" >&2
    exit 1
fi
echo "✓ VPNCheap 正在运行"

echo "检查Clash API ($CLASH)..."
if ! curl -s --max-time 2 "$CLASH/version" > /dev/null 2>&1; then
    echo "✗ 连不上 $CLASH。VPNCheap内核可能还没就绪，或端口不是9090。" >&2
    exit 1
fi
echo "✓ Clash API 可达"

echo "构建..."
go build -o vpncheap-console ./cmd/vpncheap-console

echo ""
echo "启动: http://$ADDR/"
exec ./vpncheap-console -addr "$ADDR" -clash "$CLASH"
