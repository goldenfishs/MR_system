# 邮箱验证注册

注册只需填写邮箱、密码、确认密码与邮箱验证码，邮箱即登录账号。成功后创建新生账号并自动登录；确认密码只在页面比较，API 仅提交邮箱、密码及验证码。普通登录继续使用邮箱和密码。发送验证码不会预先创建账号；必须在注册时完成校验。

姓名、性别、学校、专业、班级、学号、联系电话及本人证件照移至「个人中心 → 个人资料」填写，兴趣爱好选填。本人证件照为 2 MiB 以内的 JPEG／PNG，用于报名审核；个人头像是独立的选填图片，仅用于账号展示。未补全必填资料时，进入系统会提示前往填写；可以先浏览系统，提交团队报名仍需完整资料和 PDF 简历。资料完成度以服务端返回的 `Profile.complete` 为准。

交互参考 [Sub2API 的邮箱验证页](https://github.com/Wei-Shaw/sub2api/blob/ab80abc6d1f929a6864c8804fd077b509d21fda2/frontend/src/views/auth/EmailVerifyView.vue)，实现独立编写。密码仅保留在当前页面内存，不写入 localStorage 或 sessionStorage。

原注册页截图 `screenshots/email-registration.jpg` 保留为历史验收材料，不代表当前简化后的注册表单。

## 规则

- 使用密码学随机数生成 6 位数字验证码，有效期 10 分钟，同一邮箱 60 秒后可重发。
- 每个邮箱每小时最多发送 5 次，每个直接连接来源 IP 每 15 分钟最多请求 10 次。
- 每个验证码最多校验 5 次；过期或达到次数上限后需重新发送。新码会替换旧码。
- 验证码与规范化邮箱绑定，数据库仅保存 bcrypt 哈希；不会通过公开 API 或服务器日志返回验证码。
- 成功注册时，在同一事务内消费验证码、创建账号、空资料记录与会话。验证码不可再次使用；此时资料尚未完成，不授予入队学习资格。
- 新注册用户记录 `emailVerifiedAt`。已有账号、管理员引导账号及管理员创建的工作人员账号保持兼容，不会补造已验证记录。
- SMTP 未配置或发送失败时明确返回错误，不允许跳过验证。`applicationOpen` 控制报名表提交，邮箱注册是否可用由发信配置决定。

## 正式邮件服务

在 `.env` 或部署平台的环境变量中填写以下配置，使用邮件服务商提供的 SMTP 授权密码：

```dotenv
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_SECURITY=starttls
SMTP_USERNAME=mailer@example.com
SMTP_PASSWORD=replace-with-provider-smtp-credential
SMTP_FROM=mailer@example.com
SMTP_FROM_NAME=QUT机器人战队
```

上面的地址和密码仅为占位符。`SMTP_FROM` 必须是服务商允许发送的地址。服务商要求隐式 TLS 时改为 `SMTP_PORT=465`、`SMTP_SECURITY=tls`；两种方式均验证服务器证书，不提供忽略证书的开关。使用可信 SMTP 中继且中继不要求认证时可同时留空用户名和密码。

Docker Compose 会将这些变量传给应用，修改后执行 `docker compose up -d app` 重建应用容器。直接运行二进制或 `scripts/dev.sh` 时需将变量导出到进程环境，程序不会自动加载 `.env`。

管理员可在「空间设置 → 注册邮箱验证」查看配置状态和发件邮箱；页面不读取或展示 SMTP 密码。配置状态仅表示参数可用，不保证上游网络、认证或投递成功。实际注册发送成功后，再检查收件箱及垃圾邮件。发件域名的 SPF、DKIM 和 DMARC 由邮件服务商／域名管理员配置。后台品牌名称与 SMTP 发件显示名分别配置，修改页面品牌不会自动改写 `SMTP_FROM_NAME`。

## 本地测试邮箱

可使用 [Mailpit](https://github.com/axllent/mailpit) 在本机捕获测试邮件，不向真实邮箱发送。安装 Mailpit 后启动：

```sh
mailpit --listen 127.0.0.1:8025 --smtp 127.0.0.1:1025
```

另一个终端在仓库根目录运行：

```sh
SMTP_HOST=127.0.0.1 SMTP_PORT=1025 SMTP_SECURITY=none \
SMTP_FROM=no-reply@demo.local SMTP_FROM_NAME=QUT机器人战队 \
  bash scripts/dev.sh
```

在注册页输入虚构邮箱，例如 `learner@example.test`，发送验证码后通过 <http://127.0.0.1:8025> 查看测试邮件，再回到注册页完成校验。`none` 仅在 `DEMO_MODE=true` 且 SMTP 地址为 localhost／回环 IP 时允许；生产模式拒绝明文 SMTP。不要给本地 Mailpit 配置真实邮箱授权密码。

默认开发启动不附带邮件捕获服务。未启动 Mailpit 或真实 SMTP 时，使用现有演示身份登录即可；系统不会生成通用演示验证码。

## 持久化与部署边界

启动时自动追加邮箱验证码表和发送记录表，支持 SQLite 与 PostgreSQL。验证码、邮箱冷却、发送窗口和验证错误次数保存在数据库，重启不会重置；来源 IP 以哈希形式参与持久化发送限流。发送记录保留近一小时，验证码存储不需要额外 Redis。

当前应用仍要求单实例部署。来源 IP 取直接连接地址，不信任任意 `X-Forwarded-For`，因此经过同一反向代理的请求可能共用限额；公网部署应同时设置代理层限流。此功能不包含密码找回、验证码免密登录或自动触发的面试邮件通知；管理员可使用「批量邮件」手动通知已注册成员。真实 SMTP 服务需要另行配置，本地捕获测试不代表真实邮箱投递已经验收。
