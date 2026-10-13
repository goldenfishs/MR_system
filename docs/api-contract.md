# API 契约

所有接口位于 `/api`。除文件下载和预览外，成功响应为 JSON；失败响应为 `{error:string}`，限流时增加 `retryAfterSeconds` 和 `Retry-After`。认证使用 HttpOnly、SameSite=Lax 会话 Cookie；修改请求在包含 `Origin` 时校验来源。日期使用带时区的 RFC 3339，ID 为字符串。通用 JSON 请求上限 1 MiB，注册及资料更新上限 4 MiB；上传请求上限 26 MiB，单个文件上限 25 MiB。

## 身份与资料

```ts
type Role = 'admin' | 'mentor' | 'interviewer' | 'student';
type Competition = 'RoboMaster' | 'RoboCon'; // 历史输入 ROBOCON 仍兼容
type Direction = '机械' | '电控' | '视觉算法' | '硬件' | '运营';
interface User {
  id: string; name: string; email: string; role: Role;
  team: string; direction: string; joinedAt: string; emailVerifiedAt?: string;
}
interface ProfileFields {
  gender: '男' | '女' | '其他'; university: string; major: string;
  className: string; studentNumber: string; phone: string; hobbies: string;
}
interface Profile extends ProfileFields {
  userId: string; name: string; photoFileId: string; updatedAt: string; complete: boolean;
}
interface PhotoInput { name: string; contentBase64: string }
```

- `POST /auth/send-verification-code`：`{email}` → `{message,cooldownSeconds:60,expiresInSeconds:600}`。验证码为 6 位数字，10 分钟有效，60 秒内不能重发；每邮箱每小时最多 5 次、每来源 IP 每 15 分钟最多 10 次，错误校验最多 5 次。新验证码使旧码失效。接口不返回验证码，也不披露邮箱是否已注册。
- `POST /auth/register`：`{name,email,password,verificationCode,profile:ProfileFields,photo:PhotoInput}` → `{user:User}`。姓名、邮箱、密码、验证码、性别、学校、专业、班级、学号、电话、照片必填，兴趣爱好可以为空。照片为不带 data URL 前缀的 base64 JPEG/PNG，原文件不超过 2 MiB。仅创建 student 身份，并自动登录。身份、Profile、照片元数据、验证码消费和会话同一事务提交；失败时删除新照片文件。
- `POST /auth/login`：`{email,password}` → `{user:User}`。`POST /auth/logout` → `{ok:true}`。
- `GET /auth/me` → `{user:User,demoMode:boolean}`；匿名访问返回 401。
- `GET /profile` → `{profile:Profile}`。仅当前用户；旧账号没有资料时返回姓名和 `complete:false`，不创建虚假资料。
- `PATCH /profile`：`{name,...ProfileFields,photo?:PhotoInput}` → `{profile:Profile,user:User}`。完整提交资料字段；已有照片可省略 `photo` 保留原照片，首次补全必须附照片。
- `GET /users` → `{items:(User & {profile:Profile})[]}`，仅管理员，供审核、档案分配和邮件选人。
- `GET /public/config` → `{organization,season,demoMode,applicationOpen,applicationDeadline,emailVerificationRequired:true,registrationEnabled:boolean,brandName,logoUrl,teamName,secondaryTeamName,competitions:Competition[]}`，无需登录。`registrationEnabled` 表示已配置 SMTP，不保证外部邮件服务可用。

新注册记录 `emailVerifiedAt`。已有、引导和管理员创建的账号不会被追溯标记为已验证。密码最少 10 字节、最多 72 字节；工作人员初始密码最少 12 字节。

## 文件库

```ts
interface FileAsset {
  id: string; ownerId: string; name: string; mime: string; size: number; createdAt: string;
}
```

| 接口 | 请求／响应 | 权限与约束 |
| --- | --- | --- |
| `POST /files` | multipart/form-data，唯一文件字段 `file` → 201 `{item:FileAsset}` | 必须登录；单文件 25 MiB，个人累计 250 MiB，不接受空文件 |
| `GET /files` | `{items:FileAsset[]}` | 默认仅本人；管理员 `?all=true` 可查询全部 |
| `GET /files/:id` | `{item:FileAsset}` | 同下载权限，方便附件显示名称和大小 |
| `GET /files/:id/download` | 二进制下载 | 本人、管理员、个人档案指定收件人，或有任务材料／成果访问权限者 |
| `GET /files/:id/preview` | JPEG/PNG、PDF、MP4/WebM | 同下载权限；图片须清洗，视频支持 Range 请求 |

文件存储于 `UPLOAD_DIR`（默认 `DATA_DIR/uploads`），API 不暴露磁盘路径。支持 PDF、Office 文档、文字、ZIP/7z/RAR、常用源代码、图片、PCB/原理图/Gerber、CAD/STEP、SolidWorks、MP4/WebM 等扩展名。具体白名单见 `backend/platform_core.go` 的 `allowedExtension`。PDF 检查 `%PDF-` 文件头；JPEG/PNG 限制尺寸和像素数并重新编码，去除原始元数据及附加内容。PDF 和 MP4/WebM 支持鉴权预览；其他文件仅保存，不解压、不执行；SVG 仅附件下载，不作为页面渲染。下载带 `Content-Disposition: attachment`、`nosniff` 和限制性 CSP。文件内容不经过杀毒扫描。

文件引用每次最多 20 个，不允许重复；普通用户只能引用自己上传的文件，管理员可引用已上传文件。任务材料权限根据当前团队成员关系和方向即时判断，录取撤销后不再具有团队材料权限。个人档案附件通过明确的用户授权记录共享。当前没有公开删除文件接口。

## 团队与报名

```ts
interface Team {
  id: string; name: string; description: string; competition: Competition; competitions: Competition[];
  directions: Direction[]; status: 'open' | 'closed'; deadline: string;
  createdAt: string; createdBy: string; memberCount: number; myApplication?: TeamApplication;
}
interface TeamApplication {
  id: string; teamId: string; userId: string; userName: string; direction: Direction;
  resumeFileId: string; motivation: string; status: 'pending' | 'accepted' | 'rejected';
  feedback: string; createdAt: string; updatedAt: string; profile?: Profile;
}
interface Membership { teamId: string; userId: string; direction: Direction; joinedAt: string }
```

- `GET /teams` → `{items:Team[]}`，已登录用户。包含人数及当前用户自己的报名状态。
- `POST /teams`：`{name,description,competitions:Competition[],directions,status?,deadline?}` → 201 `{item:Team}`，仅管理员；默认状态 open。`competitions` 支持同时选择 RoboCon / RoboMaster；旧 `competition` 字段保留为首项。默认沧溟 / MOVE 是同一支战队的两个名称，不是两支战队。
- `PATCH /teams/:id`：上述字段的部分更新 → `{item:Team}`，仅管理员，可关闭或重新开放报名。
- `POST /teams/:id/applications`：`{direction,resumeFileId,motivation}` → 201 `{item:TeamApplication}`，仅学员。必须完成资料、上传本人 PDF 简历、选择开放方向，且团队开放并未到截止时间。每人每团队一份申请，pending/rejected 可更新重新提交，accepted 不允许自改。
- `GET /teams/:id/applications` → `{items:TeamApplication[]}`。管理员可读本团队全部申请及 `profile`；其他用户仅本人申请，不附加他人资料。
- `PATCH /team-applications/:id`：`{status:'accepted'|'rejected',feedback}` → `{item:TeamApplication}`，仅管理员。录取与成员关系写入原子提交；重复录取不产生重复成员，撤销录取删除相应成员关系。
- `GET /memberships` → `{items:Membership[]}`，普通用户自己的，管理员全部。

`User.team` 是旧面试流程的赛事字段；新团队的实体 ID 为 `Team.id`／`Task.teamId`。一个用户可以加入多个团队，不用覆盖旧字段表达多团队关系。

## 荣誉中心与个人档案

```ts
interface Record {
  id: string; userId: string; userName: string; title: string;
  kind: 'certificate' | 'award' | 'notice'; description: string; fileIds: string[];
  issuedAt: string; createdAt: string; createdBy: string;
}
```

- `GET /records` → `{items:Record[]}`，管理员全部，其他角色仅本人。
- `POST /records`：`{userId,title,kind,description?,fileIds?,issuedAt?}` → 201 `{item:Record}`，仅管理员。接收人必须是已存在用户，默认发放时间为当前时间；档案和附件访问授权同一事务写入。档案通知不会自动发送邮件，邮件由管理员在批量邮件页面另行发出。

## 任务、成果与评分

```ts
interface JudgeCase { input: string; expectedOutput: string; hidden: boolean }
interface JudgeConfig {
  enabled: boolean; testCases: JudgeCase[]; languageIds: number[];
  timeLimitSeconds: number; totalCases: number;
}
interface Task {
  id: string; title: string; description: string; direction: string; team: string; teamId?: string;
  category: 'assignment' | 'exam' | 'problem'; tags: string[]; locked: boolean;
  acceptedCount: number; attemptCount: number;
  kind: 'code' | 'document' | 'hardware' | 'quiz'; difficulty: '入门' | '进阶' | '挑战';
  startsAt?: string; dueAt: string; resourcesUrl: string; fileIds: string[];
  submissionType?: 'files' | 'project' | 'code'; judge?: JudgeConfig;
  maxScore: number; required: boolean; createdBy: string; createdAt: string;
  submissionCount: number; mySubmission: Submission | null;
}
interface Submission {
  id: string; taskId: string; userId: string; userName: string; content: string;
  repoUrl: string; artifactUrl: string; fileIds: string[]; judgeRunId?: string; version: number;
  status: 'pending' | 'approved' | 'changes_requested'; score: number | null;
  feedback: string; reviewedBy: string; createdAt: string; reviewedAt: string;
}
```

- `GET /tasks` → `{items:Task[]}`。学员只能看到所属团队与方向的任务；工作人员保留全队管理视图。学员响应隐藏 `hidden:true` 的输入和预期输出，保留 `totalCases`。
- `POST /tasks`：Task 的非服务器管理字段 → 201 `{item:Task}`，管理员或导师。`fileIds` 为教材或题目附件；`submissionType` 区分一般文件、工程项目和单文件代码。`teamId` 指定发布团队，`team` 为兼容旧数据的赛事字段。
- `GET /tasks/:id/submissions` → `{items:Submission[]}`，学员仅自己，管理员／导师全部。
- `POST /tasks/:id/submissions`：`{content,repoUrl?,artifactUrl?,fileIds?,judgeRunId?}` → 201 `{item:Submission}`。必须已加入对应团队／方向，在开始与截止时间内；每次创建新版本。声明提交形式的任务必须上传文件，启用判题时必须引用本任务、本人、完成状态的判题记录以及对应代码文件；源代码哈希须一致。
- `PATCH /submissions/:id`：`{status:'approved'|'changes_requested',score,feedback}` → `{item:Submission}`，管理员／导师，只可审核最新版本，分值范围为 0 至 task.maxScore。

旧任务不含 `teamId` 时，沿用旧报名录取及赛事／方向校验。机械说明书、工程 ZIP、PCB、运营图片统一使用附件；这些文件由人工评分，工程 ZIP 不自动编译。

## 在线编译测试

- `GET /judge/config` → `{configured:boolean,languages:{id:number,name:string}[]}`。服务未配置时返回 false 和空列表；配置后由独立 Judge0 兼容 API 读取语言，服务不可用返回 503。
- `POST /tasks/:id/judge`：`{sourceFileId,languageId}` → 202 `{item:JudgeRun}`。仅对应已入队学员，在允许的时间段、语言及启用判题的任务下发起。源代码必须本人上传，UTF-8 文本、1 字节至 64 KiB。每用户每分钟最多 5 次，进程最多同时处理 2 个请求。
- `GET /judge-runs/:id` → `{item:JudgeRun}`，本人或管理员。前端轮询状态；不会因查询而重复编译。

```ts
interface JudgeRun {
  id: string; taskId: string; userId: string; sourceFileId: string; sourceHash: string;
  languageId: number; status: 'queued' | 'running' | 'completed' | 'error';
  passed: number; total: number; score: number; createdAt: string; finishedAt?: string; error?: string;
  results: {
    index: number; status: string; statusId: number; hidden: boolean;
    stdout?: string; stderr?: string; compileOutput?: string; time?: string; memory?: number;
  }[];
}
```

创建判题任务需要 1–10 个用例和允许语言 ID；每用例输入、输出最多各 16,000 字节，CPU 时限 0.1–5 秒，默认 2 秒。编程考试必须设置开始和截止时间。结果分数为通过用例比例 × 任务满分，导师最终评分仍由审核接口完成。非管理员无法获取隐藏用例的 stdout/stderr/编译输出/耗时/内存。Judge0 运行请求禁用网络、限制内存和进程数；沙箱必须独立部署并正确配置，Web 进程不运行提交代码。

## 批量邮件

所有接口仅管理员可用，SMTP 未配置仍可保存草稿，但不能发送。收件人只能选择系统已有用户，不接受任意邮箱列表。

- `GET /mail-campaigns` → `{items:MailCampaign[],configured:boolean}`，最新 100 项。
- `POST /mail-campaigns`：`{subject,body,userIds:string[]}` → 201 `{item:MailCampaign}`。1–200 位收件人，正文为纯文本；仅创建草稿，不发送。
- `GET /mail-campaigns/:id` → `{item:MailCampaign}`。
- `POST /mail-campaigns/:id/send` → 202 `{item:MailCampaign}`，开始后台逐人发送；重复发送请求返回现有状态，不重复投递已完成批次。
- `POST /mail-campaigns/:id/retry` → 202 `{item:MailCampaign}`，仅重新发送明确 failed 的收件人；unknown 需人工核对，不自动重发。

```ts
interface MailCampaign {
  id: string; subject: string; body: string; status: 'draft' | 'sending' | 'completed' | 'partial';
  createdBy: string; createdAt: string; updatedAt: string;
  recipients: {
    userId: string; name: string; email: string;
    status: 'pending' | 'sending' | 'sent' | 'failed' | 'unknown'; error?: string; sentAt?: string;
  }[];
}
```

每封邮件只有一位收件人，不泄露完整名单。SMTP 结果表示上游接受投递，不保证进入收件箱；发送中断且无法确定是否成功时记录 unknown。重启恢复将未尝试的收件人标记为 failed，可由管理员重试。

## 设置、统计与兼容接口

- `GET /health` → `{status:'ok'}`，验证数据库连接。
- `GET /workspace-summary` → `{stats:{openTeams,applications,pendingApplications,members,records,files,activeTasks,pendingReviews},pipeline:{status,label,count}[],profileComplete}`。新团队统计；非管理员报名和个人档案限本人，文件数始终仅本人。
- `GET /settings` → `{settings:{organization,season,applicationOpen,applicationDeadline,hydroUrl,announcement,brandName,logoUrl,teamName,secondaryTeamName,competitions}}`。
- `PATCH /settings`：上述字段部分更新 → `{settings}`，仅管理员。
- `POST /settings/logo`：`{name,contentBase64}` → `{settings}`，仅管理员；上传 PNG/JPEG，最大 2 MiB、单边 6000 像素且总像素不超过 2000 万，服务端清洗重编码并更新 logoUrl。
- `GET /public/logo/:id`：匿名读取当前使用的品牌标志；更换标志后旧地址撤销公开，个人图片不通过此入口暴露。
- `GET /settings/email` → `{configured,from,security,supportsBulk}`，仅管理员，不返回 SMTP 密钥或主机配置。
- `GET /members` → `{items:(User & {teams:{id,name,competition,competitions,direction}[]})[]}`，工作人员可见，包含工作人员及已入队用户，并附加新团队归属列表。
- `POST /members`：`{name,email,password,role:'mentor'|'interviewer',team,direction}` → 201 `{item:User}`，仅管理员，不可用此入口创建管理员。

保留原有 `/dashboard`、`GET/POST /applicants`、`PATCH /applicants/:id` 以访问历史面试数据；它们不等同新团队报名。历史报名每用户一份，学员仅自己的、面试官仅分配记录、管理员／导师全部；内部评分和备注对学员隐藏。原有最终录取仍更新 User 的赛事／方向。新功能应使用 `/teams`、`/team-applications`、`/memberships`。

同源部署，不开放通配 CORS。生产设置 `SESSION_SECURE=true`，SMTP 与判题服务密钥通过环境注入。数据库迁移 v4 新增知识库与讨论表，并迁移默认品牌及团队多赛事字段；部署路径和实际边界见[架构说明](architecture.md)。


## 分组学习、题库与定时考试

`GET /tasks` 支持 `category`、`direction`、`tag`、`search` 参数。未分类历史任务归入 assignment。机械和运营不接受代码提交或自动判题；题库 problem 仅支持电控、视觉算法、硬件，必须配置代码测试用例。题库 AC / 尝试统计来自实际完成的 JudgeRun，不含排队、运行中或服务错误记录。

考试 category=exam 必须同时提供 startsAt / dueAt，开始时间早于截止时间。开考前普通用户可见标题和时间，但 locked=true、description/resourcesUrl 为空、fileIds 为空、judge 被移除；文件详情、下载和预览同样校验开考时间。开考前和截止后不可提交或发起判题。管理员／导师可提前管理题目。

- `PATCH /tasks/:id` → `{item:Task}`，支持任务可编辑字段。管理员可改全部，导师仅自己发布的任务；已有提交或判题记录时返回 409，须另发新版本。
- `DELETE /tasks/:id` → `{deleted:true}`，同上述权限；已有提交或判题时拒绝删除。

## 知识库

```ts
interface KnowledgeResource {
  id: string; title: string; description: string; direction: string;
  kind: 'file' | 'pdf' | 'video'; fileIds: string[]; url: string;
  createdBy: string; createdAt: string; updatedAt: string;
}
```

- `GET /knowledge-base?direction=&search=` → `{resources:KnowledgeResource[]}`，已登录用户；按方向筛选时也包含 direction 为空的共享资料。
- `POST /knowledge-base`：`{title,description?,direction?,kind,fileIds?,url?}` → 201 `{resource}`，管理员／导师；至少一个附件或 HTTP(S) 资料链接。PDF 类型的附件必须为 PDF。
- `PATCH /knowledge-base/:id` → `{resource}`；`DELETE /knowledge-base/:id` → `{deleted:true}`。管理员全部可维护，导师仅自己发布的资料。

知识库面向全体已登录用户，组别用于内容分类，不是成员访问限制。附件授权依据仍存在的知识库引用动态判断，移除引用后立即撤销该来源授权；原上传者、管理员或仍有其他合法引用者保留访问权。

## 讨论区

```ts
interface DiscussionReply { id: string; content: string; userId: string; userName: string; createdAt: string }
interface Discussion {
  id: string; title: string; content: string; direction: string;
  status: 'open' | 'resolved'; published: boolean; createdBy: string; authorName: string;
  createdAt: string; updatedAt: string; replies: DiscussionReply[];
}
```

- `GET /discussions?direction=&status=&published=true&search=` → `{discussions}`，只返回当前用户有权查看的问题。
- `POST /discussions`：`{title,content,direction?}` → 201 `{discussion}`；默认私问，仅本人、管理员和导师可见。
- `POST /discussions/:id/replies`：`{content}` → 201 `{discussion}`；仅作者、管理员、导师可回复，最多 200 条；公开精选在取消公开前禁止新增回复。
- `PATCH /discussions/:id`：`{status?,published?}` → `{discussion}`。管理员／导师可标记解答（必须已有回复）；仅管理员可公开已解答的问题。公开指所有已登录用户可查阅，匿名访问仍需登录。

## 品牌配置

默认 brandName=QUT机器人战队、organization=青岛理工大学机器人战队、teamName=沧溟、secondaryTeamName=MOVE、competitions=[RoboCon,RoboMaster]、logoUrl=/mark.svg。队名为同一战队的名称。管理员可通过设置更新这些字段；标志支持 HTTP(S) URL、本站静态资源路径及专用 /api/public/logo/:id 地址，不能使用私有附件 API 地址。公开配置不包含 SMTP/Judge 密钥或成员资料。首次 v4 迁移仅替换已知旧默认名称，保留自定义品牌与既有业务记录。
