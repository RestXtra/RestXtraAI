#!/usr/bin/env bash
# 在 WSL kali 中启动 RestXtraAI Linux 版（原生 bash，零 PowerShell）
# 自动拉起本机 PostgreSQL（若未运行），并确保 root/restxtra_ai 用户与库存在。
set -e
cd "$(dirname "$0")"

ADDR="${RESTXTRA_ADDR:-:8787}"
PROXY="${RESTXTRA_PROXY:-:8788}"
BIN="restxtra-linux"

PG_USER="${RESTXTRA_PG_USER:-root}"
PG_PASS="${RESTXTRA_PG_PASS:-123456}"
PG_DB="${RESTXTRA_PG_DB:-restxtra_ai}"

# ── 0. 确保 PostgreSQL 运行 ──────────────────────────────────────────────
ensure_postgres() {
  if command -v pg_lsclusters >/dev/null 2>&1; then
    # 任一 cluster 的 status 不是 online 就尝试启动
    if ! pg_lsclusters 2>/dev/null | awk 'NR>1 && $4=="down"' | grep -q .; then
      echo "[pg] 检测到 cluster 全部 online，跳过启动"
    else
      echo "[pg] 检测到 cluster 未运行，正在启动..."
      (command -v service >/dev/null 2>&1 && service postgresql start) \
        || (command -v pg_ctlcluster >/dev/null 2>&1 && pg_ctlcluster 17 main start) \
        || true
    fi
  fi
  # 等就绪
  for i in $(seq 1 30); do
    if command -v pg_isready >/dev/null 2>&1 && pg_isready -q -h 127.0.0.1 -p 5432; then
      echo "[pg] PostgreSQL 就绪 (127.0.0.1:5432)"
      return 0
    fi
    sleep 1
  done
  echo "[pg] 警告：PostgreSQL 未在 30s 内就绪，继续启动（可能失败）" >&2
}

# ── 1. 确保角色与库存在（幂等） ─────────────────────────────────────────
ensure_role_db() {
  local ok=1
  sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='$PG_USER'" 2>/dev/null | grep -q 1 || ok=0
  if [ "$ok" = "0" ]; then
    echo "[pg] 创建角色 $PG_USER ..."
    sudo -u postgres psql -c "CREATE USER \"$PG_USER\" WITH PASSWORD '$PG_PASS' SUPERUSER CREATEDB;"
  fi
  if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='$PG_DB'" 2>/dev/null | grep -q 1; then
    echo "[pg] 创建数据库 $PG_DB ..."
    sudo -u postgres psql -c "CREATE DATABASE \"$PG_DB\" OWNER \"$PG_USER\";"
  fi
}

ensure_postgres
ensure_role_db

if [ ! -x "$BIN" ]; then
  echo "[build] 未找到 $BIN，正在编译 Linux 二进制..."
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags embedui -o "$BIN" ./cmd/restxtra
fi

echo "[start] $(date) 启动 $BIN -addr $ADDR -proxy $PROXY"
exec "./$BIN" -addr "$ADDR" -proxy "$PROXY"
