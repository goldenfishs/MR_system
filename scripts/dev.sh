#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for dependency in node npm go; do
  if ! command -v "$dependency" >/dev/null 2>&1; then
    printf '缺少 %s，请安装 Node.js 22 LTS、npm 和 Go 1.24+ 后重试。\n' "$dependency" >&2
    exit 1
  fi
done

if [[ ! -d "$project_dir/frontend/node_modules" ]]; then
  printf '请先安装前端依赖：npm --prefix "%s/frontend" ci\n' "$project_dir" >&2
  exit 1
fi

if [[ -n "${DATABASE_URL:-}" ]]; then
  printf '本地演示使用独立 SQLite 数据库，请先 unset DATABASE_URL。\n' >&2
  exit 1
fi

backend_pid=""
frontend_pid=""
cleanup() {
  trap - EXIT INT TERM
  [[ -z "$frontend_pid" ]] || kill "$frontend_pid" 2>/dev/null || true
  [[ -z "$backend_pid" ]] || kill "$backend_pid" 2>/dev/null || true
  [[ -z "$frontend_pid" ]] || wait "$frontend_pid" 2>/dev/null || true
  [[ -z "$backend_pid" ]] || wait "$backend_pid" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Build first so the tracked process is the server itself, not a go run wrapper.
mkdir -p "$project_dir/bin"
(
  cd "$project_dir/backend"
  go build -o "$project_dir/bin/mr-system" .
)

DEMO_MODE=true SESSION_SECURE=false PUBLIC_ORIGIN=http://localhost:5173 \
  ADDR=127.0.0.1:8080 DATA_DIR="$project_dir/data/demo" \
  WEB_DIR="$project_dir/frontend/dist" \
  "$project_dir/bin/mr-system" &
backend_pid=$!

node "$project_dir/frontend/node_modules/vite/bin/vite.js" \
  "$project_dir/frontend" --host 127.0.0.1 --port 5173 --strictPort &
frontend_pid=$!

printf '\n本地演示：http://localhost:5173\n后端：127.0.0.1:8080；演示数据：data/demo\n按 Ctrl+C 停止两个进程。\n\n'

# Portable to macOS Bash 3.2 (which does not implement wait -n).
while kill -0 "$backend_pid" 2>/dev/null && kill -0 "$frontend_pid" 2>/dev/null; do
  sleep 1
done
printf '开发服务已退出，请检查上方日志。\n' >&2
exit 1
