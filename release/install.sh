#!/bin/sh
set -eu
umask 077
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

usage() {
  echo '用法: ./install.sh --email admin@example.com [--configure-only]'
  echo '首次运行生成随机密码和 .env；已有 .env 时保留配置并启动服务。'
}
email=''
configure_only=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --email) [ "$#" -ge 2 ] || { usage; exit 1; }; email=$2; shift 2 ;;
    --configure-only) configure_only=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 1 ;;
  esac
done
command -v docker >/dev/null 2>&1 || { echo '请先安装 Docker Engine 和 Docker Compose 插件。' >&2; exit 1; }
docker compose version >/dev/null

if [ ! -e .env ]; then
  if [ -z "$email" ]; then
    printf '管理员邮箱: '
    IFS= read -r email
  fi
  # 邮箱会写入 dotenv，只允许常见邮件地址字符，禁止注入换行、变量或注释。
  case "$email" in
    ''|*[!a-zA-Z0-9._+@-]*|@*|*@|*@*@*) echo '请输入有效的管理员邮箱。' >&2; exit 1 ;;
    *@*.*) ;;
    *) echo '请输入有效的管理员邮箱。' >&2; exit 1 ;;
  esac
  if grep -q '@IMAGE_REFERENCE@' .env.example; then
    echo '这是发布模板，请从 GitHub Release 下载已构建的安装包。' >&2
    exit 1
  fi
  random_hex() { od -An -N24 -tx1 /dev/urandom | tr -d ' \n'; }
  db_password=$(random_hex)
  admin_password=$(random_hex)
  [ "${#db_password}" -eq 48 ] && [ "${#admin_password}" -eq 48 ]
  tmp_env=$(mktemp .env.XXXXXX)
  trap 'rm -f "$tmp_env"' EXIT HUP INT TERM
  # 不执行或 source .env 内容；Docker Compose 只将其作为配置读取。
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      POSTGRES_PASSWORD=*) printf 'POSTGRES_PASSWORD=%s\n' "$db_password" ;;
      ADMIN_EMAIL=*) printf 'ADMIN_EMAIL=%s\n' "$email" ;;
      ADMIN_PASSWORD=*) printf 'ADMIN_PASSWORD=%s\n' "$admin_password" ;;
      *) printf '%s\n' "$line" ;;
    esac
  done < .env.example > "$tmp_env"
  # 硬链接提供不覆盖已有配置的原子创建；并发运行时直接报错。
  ln "$tmp_env" .env
  rm -f "$tmp_env"
  trap - EXIT HUP INT TERM
  printf '已创建权限为 600 的 .env。管理员初始密码保存在 ADMIN_PASSWORD 行，请在本机查看。\n'
else
  echo '检测到现有 .env，保留全部配置和密码。'
fi

docker compose config --quiet
if [ "$configure_only" = true ]; then
  echo '配置已生成；检查 .env 后执行 docker compose up -d --wait。'
  exit 0
fi
docker compose pull
docker compose up -d --wait --wait-timeout 180
echo 'MR 已启动。默认本机访问 http://localhost:8080；远程服务器可使用 SSH 隧道或配置 HTTPS 反向代理。'
echo '查看状态：docker compose ps；查看日志：docker compose logs --tail=100 app'
