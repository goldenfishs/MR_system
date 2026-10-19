# MR System v0.2.0 安装

本包用于首次自建安装，支持 Linux amd64（x86_64）和 arm64。包含应用与 PostgreSQL 的 Docker Compose 配置；不包含演示账号、生产数据或 Judge0。应用镜像包含生成证书所需的中文字体、LibreOffice 和 PDF 工具。

## 首次安装

安装 Docker Engine 与 Docker Compose 插件（建议 Compose 2.24 或更新版本），至少预留 2 CPU、4 GB 内存及 10 GB 磁盘；实际容量取决于上传文件数量。

从 [GitHub Releases](https://github.com/goldenfishs/MR_system/releases) 下载 `mr-system-v0.2.0-docker.tar.gz` 和 `SHA256SUMS`。

```sh
# Linux；macOS 可改为 shasum -a 256。
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf mr-system-v0.2.0-docker.tar.gz
cd mr-system-v0.2.0
./install.sh --email admin@example.com
```

脚本生成独立随机数据库密码和管理员初始密码，保存在权限为 600 的 `.env`，不会将密码输出到安装日志。安装后在本机打开 `.env` 查看 `ADMIN_PASSWORD`，访问 `http://localhost:8080` 登录。也可以先执行 `./install.sh --email admin@example.com --configure-only`，编辑配置后再运行脚本。

初始管理员仅在空数据库首次启动时创建。后续修改 `.env` 的 `ADMIN_PASSWORD` 不会重置已有账号密码；应在网页“用户管理 → 设置密码”修改。密码至少 6 个字符，没有复杂度要求。

默认端口只绑定 `127.0.0.1:8080`。远程服务器可从自己电脑使用 `ssh -L 8080:127.0.0.1:8080 user@server`，再访问本机地址。正式对外服务应配置域名和 HTTPS 反向代理，并在 `.env` 设置：

```dotenv
PUBLIC_ORIGIN=https://your-domain.example
SESSION_SECURE=true
# 仅信任实际反向代理的 IP，不要填任意客户端或 0.0.0.0/0。
TRUSTED_PROXY_IPS=
```

如果需要局域网 HTTP 测试，可手动将 `APP_BIND` 改为 `0.0.0.0:8080`，并将 `PUBLIC_ORIGIN` 改为实际访问地址。重新执行 `docker compose up -d --wait` 应用修改。

邮件服务可在网站系统设置中配置；配置 SMTP 后才可发送注册及找回密码邮件。在线编译需要单独部署 Judge0 并设置 `JUDGE_URL` / `JUDGE_TOKEN`，不启用时仍可使用常规学习、作业上传和人工批改。

## 镜像地址与下载替代方案

本包固定使用以下不可变镜像摘要，升级不会意外拉取另一个版本：

```
ghcr.io/goldenfishs/mr_system:<版本号>@sha256:<本次发布摘要>
```

如果 GHCR 暂时不可访问，可以在 Release 下载对应架构的 `mr-system-v0.2.0-linux-amd64-image.tar.gz` 或 `...-arm64-image.tar.gz`，校验后导入：

```sh
docker load -i mr-system-v0.2.0-linux-amd64-image.tar.gz
```

执行 `./install.sh --email admin@example.com --configure-only`，将 `.env` 中 `MR_IMAGE` 改为 `ghcr.io/goldenfishs/mr_system:v0.2.0`。先运行 `docker compose pull db` 获取 PostgreSQL 镜像，再运行 `docker compose up -d --pull never --wait`；此模式不要再运行默认安装脚本（它会再次尝试拉取应用镜像）。这不是完整离线安装：PostgreSQL 镜像需要单独下载。

## 备份、升级与回退

数据库与上传文件分别存放于 `postgres_data`、`app_data` 命名卷，普通重新创建容器会保留它们。不要执行 `docker compose down -v`，该命令会删除数据卷。

每次升级前，在原安装目录创建数据库备份并另行备份 `.env` 和整个 `app_data` 卷：

```sh
mkdir -p backups
chmod 700 backups
docker compose exec -T db pg_dump -U mr_system -d mr_system -Fc > backups/database.dump
```

上传文件如需一致性快照，应先暂停提交/停止 app，再通过 Docker 数据卷备份工具备份 `app_data`。`.env` 包含密码，备份目录应只对管理员可读。

下载新安装包，阅读发布说明。保留原安装目录与 `.env`，从新包复制 Compose 配置，并仅将原 `.env` 的 `MR_IMAGE` 更新为新包对应摘要，随后：

```sh
docker compose pull app
docker compose up -d --no-deps --wait app
docker compose ps
docker compose logs --tail=100 app
```

请保持 Compose 项目名 `mr-system` 不变，否则会连接到另一套新数据卷。如果同机部署多套系统，首次安装时通过 `docker compose -p <独立项目名>` 明确区分，并在每次操作中使用相同项目名。

数据库迁移会自动运行。若升级已修改数据库结构，回退前先确认旧版兼容性，必要时从升级前备份恢复数据库和文件；仅切换镜像不保证能回退数据库。

## 自行构建

项目源码：[goldenfishs/MR_system](https://github.com/goldenfishs/MR_system)。仓库根目录执行 `docker build -t mr-system:local .`。根目录的 `compose.yaml` 用于源码构建，本安装包的 `compose.yaml` 使用发布镜像。
