#!/usr/bin/env bash
# 在 WSL kali 中启动 RestXtraAI Linux 版（原生 bash，零 PowerShell）
# 自动拉起本机 PostgreSQL（若未运行），并确保最小权限的应用用户与库存在。
set -euo pipefail
cd "$(dirname "$0")"

ADDR="${RESTXTRA_ADDR:-:8787}"
PROXY="${RESTXTRA_PROXY:-:8788}"
BIN="restxtra-linux"

PG_USER="${RESTXTRA_PG_USER:-restxtra}"
PG_PASS="${RESTXTRA_PG_PASS:-}"
PG_DB="${RESTXTRA_PG_DB:-restxtra}"
PG_STARTED_LOCAL=0

# ── 0. 确保 PostgreSQL 可用 ─────────────────────────────────────────────
# 优先用 TCP 探测 5432：若已有实例响应（如 Docker 容器 postgres:16 映射的
# 0.0.0.0:5432），说明数据库就绪，直接跳过本机 cluster 逻辑（本机 cluster
# 会因端口被占而无法启动，反而报 socket 错误）。
pg_ready() {
  command -v pg_isready >/dev/null 2>&1 && pg_isready -q -h 127.0.0.1 -p 5432
}

ensure_postgres() {
  # 已有实例在 TCP 5432 响应 → 视为就绪，跳过
  if pg_ready; then
    echo "[pg] 检测到 127.0.0.1:5432 已有 PostgreSQL 实例（如 Docker 容器），直接使用"
    return 0
  fi
  PG_STARTED_LOCAL=1
  # 否则尝试启动本机 cluster
  if command -v pg_lsclusters >/dev/null 2>&1; then
    if ! pg_lsclusters 2>/dev/null | awk 'NR>1 && $4=="down"' | grep -q .; then
      echo "[pg] 检测到 cluster 全部 online，跳过启动"
    else
      echo "[pg] 检测到 cluster 未运行，正在启动..."
      (command -v service >/dev/null 2>&1 && service postgresql start) \
        || (command -v pg_ctlcluster >/dev/null 2>&1 && pg_ctlcluster 17 main start) \
        || true
    fi
  fi
  for i in $(seq 1 30); do
    if pg_ready; then
      echo "[pg] PostgreSQL 就绪 (127.0.0.1:5432)"
      return 0
    fi
    sleep 1
  done
  echo "[pg] PostgreSQL 未在 30s 内就绪" >&2
  return 1
}

# ── 1. 确保角色与库存在（幂等） ─────────────────────────────────────────
# 仅初始化本脚本刚启动的本地 cluster；已存在的 TCP 实例由其管理者负责。
ensure_role_db() {
  if [ "$PG_STARTED_LOCAL" != "1" ]; then
    echo "[pg] 使用已存在的 PostgreSQL，跳过角色/库初始化"
    return 0
  fi
  if ! [[ "$PG_USER" =~ ^[a-z_][a-z0-9_]*$ && "$PG_DB" =~ ^[a-z_][a-z0-9_]*$ ]]; then
    echo "[pg] 用户名和数据库名只能包含小写字母、数字和下划线" >&2
    return 1
  fi
  if [ -z "$PG_PASS" ]; then
    echo "[pg] 首次初始化需要设置 RESTXTRA_PG_PASS（不提供弱默认密码）" >&2
    return 1
  fi
  echo "[pg] 确保角色 $PG_USER 与数据库 $PG_DB 存在 ..."
  sudo -u postgres psql -v ON_ERROR_STOP=1 -v role="$PG_USER" -v pwd="$PG_PASS" -v db="$PG_DB" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE', :'role', :'pwd')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'role') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'db', :'role')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'db') \gexec
SQL
}

ensure_postgres
ensure_role_db

if [ ! -x "$BIN" ] || [ "${RESTXTRA_REBUILD:-0}" = "1" ]; then
  echo "[build] 未找到 $BIN，正在编译 Linux 二进制..."
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags embedui -o "$BIN" ./cmd/restxtra
fi

echo "[start] $(date) 启动 $BIN -addr $ADDR -proxy $PROXY"
exec "./$BIN" -addr "$ADDR" -proxy "$PROXY"
