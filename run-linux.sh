#!/usr/bin/env bash
# 在 WSL kali 中启动 RestXtraAI Linux 版（原生 bash，零 PowerShell）
set -e
cd "$(dirname "$0")"

ADDR="${RESTXTRA_ADDR:-:8787}"
PROXY="${RESTXTRA_PROXY:-:8788}"
BIN="restxtra-linux"

if [ ! -x "$BIN" ]; then
  echo "缺少 $BIN，请先在 Windows 上执行: GOOS=linux CGO_ENABLED=0 go build -tags embedui -o restxtra-linux ./cmd/restxtra"
  exit 1
fi

echo "[start] $(date) 启动 $BIN -addr $ADDR -proxy $PROXY"
exec "./$BIN" -addr "$ADDR" -proxy "$PROXY"
