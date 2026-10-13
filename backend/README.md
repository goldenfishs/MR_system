# Backend

原创 Go HTTP API，使用 HttpOnly 会话 Cookie、bcrypt 密码哈希和持久化数据库。所有业务接口见 [`../docs/api-contract.md`](../docs/api-contract.md)。默认使用纯 Go SQLite；设置 `DATABASE_URL` 后使用 PostgreSQL。前端与 API 应同源部署。

## 本地运行

需要 Go 1.24 或更新版本。在 `backend/` 中运行：

```sh
go mod download
DEMO_MODE=true go run .
```

默认监听 `127.0.0.1:8080`。前端开发服务器代理 `/api` 到该地址，并保留原始 Host（`changeOrigin: false`）。生产前端构建放在 `../frontend/dist` 后，后端会直接提供页面和静态文件。

演示模式仅在空数据库首次初始化时写入虚构账号、报名、任务和评阅记录，日期相对首次启动时间生成；重启不会清空数据。四个演示账号：

| 角色 | 邮箱 | 密码 |
| --- | --- | --- |
| 管理员 | `admin@demo.local` | `DemoAdmin!2026` |
| 导师 | `mentor@demo.local` | `DemoMentor!2026` |
| 已录取学员 | `student@demo.local` | `DemoStudent!2026` |
| 面试官 | `interviewer@demo.local` | `DemoInterview!2026` |

所有演示身份与成果均为虚构。生产模式会拒绝打开曾写入公开演示凭据的数据库；请使用独立数据目录或独立 PostgreSQL 数据库。

## 生产配置

| 环境变量 | 默认值与作用 |
| --- | --- |
| `ADDR` | `127.0.0.1:8080`；容器内设置 `0.0.0.0:8080` |
| `DATA_DIR` | `./data`，SQLite 数据目录 |
| `DATABASE_URL` | 空；有值时使用 PostgreSQL 连接串 |
| `WEB_DIR` | `../frontend/dist`，前端静态资源目录 |
| `DEMO_MODE` | `false`；仅字面值 `true` 启用演示数据 |
| `ADMIN_EMAIL` | 新生产数据库首次启动必填 |
| `ADMIN_PASSWORD` | 新生产数据库首次启动必填，12–72 字节 |
| `ADMIN_NAME` | `管理员` |
| `SESSION_SECURE` | `false`；HTTPS 部署必须设置 `true` |
| `PUBLIC_ORIGIN` | 可选，反向代理改变 Host 时填写完整源，例如 `https://robots.example.org`，不含尾斜线 |

管理员可在成员页面创建导师、面试官账号。新账号使用手动设置的初始密码；MVP 尚未提供找回密码、改密或邮件投递，应仅向可信内部人员提供工作人员账号。普通注册仅创建学生，无法自行获取导师、面试官或管理员权限。建议通过运维密钥管理注入初始密码，初始化完成后移除 bootstrap 环境变量。已有管理员时不会被环境变量重置。

会话有效期 24 小时，服务端只保存 Cookie 令牌的 SHA-256 摘要。登录/注册对来源 IP 与邮箱组合设有进程内速率限制，成功后清除该组合的计数；反向代理场景不信任任意 `X-Forwarded-For`，因此建议在入口另外设置限速。

## 实现边界

- MVP 使用单实例部署。写请求在进程内串行化，保证同一实例的版本生成和“只评最新提交”约束；**不要运行共享数据库的多个副本**。PostgreSQL 17 的核心业务与重连持久化集成测试已在 GitHub Actions 通过，详见[验收记录](../docs/verification.md)。
- 数据采用标准 SQL 表＋JSON 文档，已有主键、邮箱唯一约束、每人一份报名表、提交版本唯一约束及版本化迁移记录。启动时自动创建 v1 表结构；后续结构变更应追加迁移。
- 文件成果以 HTTP(S) 链接提交，尚无文件上传、病毒扫描或对象存储实现。
- Hydro 为设置中的外部入口，尚无账号映射、单点登录、判题或成绩同步。`quiz` 任务以说明/链接提交后人工审核，不是自动判分测验。
- 站内活动记录已持久化；未接入短信、邮件或其他外部消息渠道。
- 导师可查看全部报名但不可修改招新决定；仅管理员可录取或取消录取。面试官只能查看、评分被分配的候选人。学员只接收自身报名和提交记录，面试内部评语和分数不会返回给学员。
- 历次任务提交保留为不可变版本。审核只作用于最新版本；提交截止和赛队/方向条件均由服务端检查。
- 静态文件使用 Go `os.OpenRoot` 限制访问范围，路径跳转及符号链接不能访问 `WEB_DIR` 外的文件。

## 验证

```sh
go test ./...
go test -race ./...
go vet ./...
go build .
```

API 集成测试使用独立临时 SQLite，覆盖登录、Cookie、跨源请求、注册提权、报名资料隔离、面试分配、提交版本与评分边界、截止日期、演示环境隔离及静态资源目录限制。

可选 PostgreSQL 集成测试通过 `TEST_DATABASE_URL` 启用，未设置时自动跳过：

```sh
TEST_DATABASE_URL='postgres://test_user:test_password@127.0.0.1:5432/mr_system_test?sslmode=disable' \
  go test -run '^TestPostgreSQLPersistence$' -count=1 -v ./...
```

请使用专用测试数据库，连接用户需要创建 schema 的权限。测试创建随机独立 schema，通过连接的 `search_path` 隔离所有业务表；结束时仅删除该随机 schema，不清空已有表。测试覆盖事务化演示初始化、重复初始化、登录及持久会话、报名新增和更新、录取事务、任务发布、提交版本、仅审核最新版本、数据库重连后的数据持久化与学生隐私。CI 使用独立 PostgreSQL 17 服务执行该测试。
