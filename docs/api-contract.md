# API 契约

所有接口位于 `/api`。除文件下载和预览外，成功响应为 JSON；失败响应为 `{error:string}`，限流时增加 `retryAfterSeconds` 和 `Retry-After`。认证使用 HttpOnly、SameSite=Lax 会话 Cookie；修改请求在包含 `Origin` 时校验来源。日期使用带时区的 RFC 3339，ID 为字符串。通用 JSON 请求（含注册）上限 1 MiB，资料和标志更新上限 4 MiB；上传请求上限 26 MiB，单个文件上限 25 MiB。

## 身份与资料

```ts
type Role = 'admin' | 'group_admin' | 'member' | 'reserve' | 'freshman';
type MemberType = 'formal' | 'reserve' | 'freshman';
type GroupPermission = 'recruitment' | 'assessment' | 'tasks' | 'knowledge' | 'discussions' | 'members' | 'records' | 'mail';
type Competition = 'RoboMaster' | 'RoboCon'; // 历史输入 ROBOCON 仍兼容
type Direction = '机械' | '电控' | '视觉算法' | '硬件' | '运营';
interface GroupGrant { direction: Direction; permissions: GroupPermission[] }
interface User {
  id: string; name: string; email: string; role: Role;
  memberType: MemberType; groupGrants: GroupGrant[];
  assessmentPassedAt?: string; assessmentConfirmedBy?: string; assessmentNote?: string;
  team: string; direction: string; joinedAt: string; emailVerifiedAt?: string;
}
interface ProfileFields {
  gender: '男' | '女' | '其他'; university: string; major: string;
  className: string; studentNumber: string; phone: string; hobbies: string;
}
interface Profile extends ProfileFields {
  userId: string; name: string; avatarFileId?: string; photoFileId: string; updatedAt: string; complete: boolean;
}
interface PhotoInput { name: string; contentBase64: string }
```

- `POST /auth/send-verification-code`：`{email}` → `{message,cooldownSeconds:60,expiresInSeconds:600}`。验证码为 6 位数字，10 分钟有效，60 秒内不能重发；每邮箱每小时最多 5 次、每来源 IP 每 15 分钟最多 10 次，错误校验最多 5 次。新验证码使旧码失效。接口不返回验证码，也不披露邮箱是否已注册。
- `POST /auth/register`：`{email,password,verificationCode}` → `{user:User}`。仅填写邮箱、密码及邮箱验证码；确认密码只在前端比较，不发送至 API。不接受 `name`、`profile`、`photo` 或身份／权限字段，额外字段返回 400。仅创建 freshman 身份，memberType=freshman、groupGrants=[]，并自动登录。`User.name` 初始显示为「未填写姓名」，空 Profile、用户、验证码消费和会话同一事务提交，注册不上传照片。
- `POST /auth/login`：`{email,password}` → `{user:User}`。`POST /auth/logout` → `{ok:true}`。
- `GET /auth/me` → `{user:User,demoMode:boolean}`；匿名访问返回 401。
- `GET /profile` → `{profile:Profile}`。仅当前用户；新注册账号的姓名及资料字段为空，`complete:false`。旧账号没有资料时返回原有姓名和 `complete:false`，不补造学校、学号或照片等资料。
- `PATCH /profile`：`{name,...ProfileFields,avatar?:PhotoInput,photo?:PhotoInput}` → `{profile:Profile,user:User}`。头像选填、用于账号展示；`photo` 是本人证件照，用于报名审核且仍为必填。已有图片可省略对应字段以保留；两张图片可以一起提交。也可用 `PATCH /profile/avatar` 单独保存头像。
- `PATCH /profile/avatar`：`PhotoInput` → `{profile:Profile}`。可在资料尚未补全时单独更新头像，不改变本人证件照或资料完整度。`PATCH /profile` 仅提交 `{avatar:PhotoInput}` 也返回 `{profile:Profile}`。头像预览仅本人及系统管理员可访问，组内报名审核继续读取 `photoFileId`。
- `GET /users?permission=members|recruitment|records|mail` → `{items:(User & {profile:Profile})[]}`，默认用途 members，供审核、档案分配和邮件选人。管理员全部；组管理员必须有所选模块权限，并按接收人当前成员方向筛选。recruitment 也匹配报名方向和有同组报名权限的审核人员；mail 对尚无成员方向者匹配待审核的新报名或未拒绝的历史报名，用于招新联系。普通用户不能读取其他人员资料；不支持其他用途值。

mail／records 选人不会自动授予完整资料访问权：只有该用途权限时，返回姓名、邮箱和所属组，records 另保留学号；照片、电话、专业、班级、兴趣和考核确认字段清空。管理员、本人，或另有目标人员所在组 members／recruitment 权限者保留完整资料。响应仍采用相同 User／Profile 结构，受限字段为空。
- `GET /public/config` → `{organization,season,demoMode,applicationOpen,applicationDeadline,emailVerificationRequired:true,registrationEnabled:boolean,brandName,logoUrl,teamName,secondaryTeamName,competitions:Competition[]}`，无需登录。`registrationEnabled` 表示已配置 SMTP，不保证外部邮件服务可用。

新注册记录 `emailVerifiedAt`。已有、引导和管理员创建的账号不会被追溯标记为已验证。密码最少 10 字节、最多 72 字节；工作人员初始密码最少 12 字节。

注册后在「个人中心 → 个人资料」填写姓名、性别、学校、专业、班级、学号、联系电话及本人证件照；这些项目仍为报名必填，兴趣爱好和个人头像选填。头像与证件照使用不带 data URL 前缀的 base64 JPEG/PNG，单张原文件不超过 2 MiB。登录后的资料提醒以 `Profile.complete` 为准，不以显示姓名或前端本地记录代替服务端校验；资料未完整仍可进入系统，提交团队报名须先补全。

管理员拥有全部权限；group_admin 可兼任多个组，以 groupGrants 的每条 direction/permissions 独立授权。`recruitment` 为报名与历史面试审核，`assessment` 为新生培养阶段与最终考核确认，`tasks` 为作业／题库／考试发布、编辑与评分，`knowledge` 为知识库管理，`discussions` 为私问答疑与精选公开，`members` 为组内成员查看，`records` 为组内荣誉／档案发放，`mail` 为组内批量邮件。空组别的全队管理内容仅管理员可维护。组权限不允许转授权限或修改平台设置；考核权限仅能按完整培养流程晋升新生，不能分配管理员身份。

memberType 表示人员身份，独立于管理职责：formal=正式队员、reserve=梯队队员、freshman=新生。非管理者的 role 分别为 member、reserve、freshman；管理者的 role 为 admin 或 group_admin，仍保留 memberType。通用身份编辑和管理授权由管理员负责；获授权组管理员可通过对应组的最终考核晋升新生。注册不能指定身份或权限。

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
| `GET /files/:id/download` | 二进制下载 | 本人、管理员、个人档案指定收件人，或有任务材料／成果／知识库访问权限者 |
| `GET /files/:id/preview` | JPEG/PNG、PDF、MP4/WebM | 同下载权限；图片须清洗，视频支持 Range 请求 |

文件存储于 `UPLOAD_DIR`（默认 `DATA_DIR/uploads`），API 不暴露磁盘路径。支持 PDF、Office 文档、文字、ZIP/7z/RAR、常用源代码、图片、PCB/原理图/Gerber、CAD/STEP、SolidWorks、MP4/WebM 等扩展名。具体白名单见 `backend/platform_core.go` 的 `allowedExtension`。PDF 检查 `%PDF-` 文件头；JPEG/PNG 限制尺寸和像素数并重新编码，去除原始元数据及附加内容。PDF 和 MP4/WebM 支持鉴权预览；其他文件仅保存，不解压、不执行；SVG 仅附件下载，不作为页面渲染。下载带 `Content-Disposition: attachment`、`nosniff` 和限制性 CSP。文件内容不经过杀毒扫描。

文件引用每次最多 20 个，不允许重复；普通用户只能引用自己上传的文件，管理员可引用已上传文件。任务材料权限根据当前入队状态、团队成员关系和方向即时判断；知识库附件同样要求学习访问权限或该组 knowledge 管理权限。失去入队资格后撤销成员学习来源的访问权，本人上传的文件、个人档案明确授权的附件及仍获授权的管理材料保留访问。当前没有公开删除文件接口。

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
  trainingStatus?: 'training' | 'completed' | 'eliminated'; trainingStageTitle?: string; trainingCycle?: number;
}
interface Membership { teamId: string; userId: string; direction: Direction; joinedAt: string }
```

- `GET /teams` → `{items:Team[]}`，已登录用户。包含人数及当前用户自己的报名状态。
- `POST /teams`：`{name,description,competitions:Competition[],directions,status?,deadline?}` → 201 `{item:Team}`，仅管理员；默认状态 open。`competitions` 支持同时选择 RoboCon / RoboMaster；旧 `competition` 字段保留为首项。默认沧溟 / MOVE 是同一支战队的两个名称，不是两支战队。
- `PATCH /teams/:id`：上述字段的部分更新 → `{item:Team}`，仅管理员，可关闭或重新开放报名。
- `POST /teams/:id/applications`：`{direction,resumeFileId,motivation}` → 201 `{item:TeamApplication}`，普通人员。必须完成资料、上传本人 PDF 简历、选择开放方向，且团队开放并未到截止时间。每人每团队一份申请，pending/rejected 可更新重新提交，accepted 不允许自改。
- `GET /teams/:id/applications` → `{items:TeamApplication[]}`。管理员可读本团队全部申请及 `profile`；有 recruitment 权限的组管理员可读对应方向申请及资料；其余仅本人申请。
- `PATCH /team-applications/:id`：`{status:'accepted'|'rejected',feedback,reopenTraining?:boolean}` → `{item:TeamApplication}`，管理员或对应方向有 recruitment 权限的组管理员。报名通过不改变新生身份；录取、成员关系和新生培养周期写入原子提交，重复录取不产生重复成员或培养周期，撤销录取删除相应成员关系。当前团队／方向已淘汰的人员不能用重复 accepted 恢复本组资格，须明确传 `reopenTraining:true` 并具备该组 assessment 权限；仅 recruitment 权限不能重开。其他团队／方向可以独立录取，不受该淘汰记录影响。
- `GET /memberships` → `{items:Membership[]}`，管理员全部；组管理员本人及有 members 权限的方向；普通用户自己的。
- `GET /learning-access` → `{allowed:boolean,directions:Direction[],appliedDirections:Direction[],enrolledDirections:Direction[]}`，必须登录。`allowed` 表示跨组学习资格；允许时 `directions` 为全部技术组，否则为空。`appliedDirections` 表示有效待审或已录取报名及兼容成员方向，用于侧栏星标；拒绝、已淘汰的对应报名不计入。`enrolledDirections` 为已生效的录取方向。管理员、正式／梯队队员，以及至少有一个有效入队关系或兼容历史 accepted 记录的新生可查看全部组学习内容。培养资格按用户、团队、方向各自最新周期判断，某组淘汰只使该范围失效，其他有效组继续授予学习资格。管理角色不代替成员提交资格；组管理员仍可执行明确授予的管理操作。所有状态从数据库动态计算，审核、淘汰与撤销无需重新登录即可生效。

`User.team` 是旧面试流程的赛事字段；新团队的实体 ID 为 `Team.id`／`Task.teamId`。一个用户可以同时加入多个单方向团队，各组分别报名、审核与考核，不用覆盖旧字段表达多团队关系。

侧栏始终显示机械、电控、硬件、视觉算法、运营五组，已有效报名的组显示 🌟。普通人员通过任一组报名审核后，可以浏览全部组的学习内容，仍保留新生身份；最终考核通过后再晋升梯队／正式队员。未获学习资格时访问 `/learning` 或 `/assessment` 转至团队报名页，并保留所选方向提示。报名、个人资料、个人荣誉和讨论区仍可使用；有跨组学习访问权限不代表可以管理其他组、读取他人成果或提交任意任务，具体团队、方向和时间限制继续生效。

## 新生培养与阶段考核

```ts
interface TrainingStage {
  id: string; title: string; description: string; order: number; final: boolean;
}
interface TrainingPlan {
  id: string; teamId: string; direction: Direction; title: string; stages: TrainingStage[];
  createdBy: string; createdAt: string; updatedBy: string; updatedAt: string;
}
interface TrainingDecision {
  stageId: string; stageTitle: string; status: 'passed' | 'eliminated'; note: string;
  decidedBy: string; decidedByName: string; decidedAt: string; memberType?: 'reserve' | 'formal';
}
interface TrainingProgress {
  id: string; teamId: string; userId: string; userName: string; email: string;
  direction: Direction; cycle: number; planId: string; planTitle: string;
  stages: TrainingStage[]; currentStageId: string; status: 'training' | 'completed' | 'eliminated';
  decisions: TrainingDecision[]; createdBy: string; createdAt: string; updatedAt: string;
}
```

- `GET /training-plans?teamId=&direction=` → `{items:TrainingPlan[]}`，已登录可查看阶段方案，不返回人员考核记录。
- `POST /training-plans`：`{teamId?,direction,title,stages}` → 201 `{item:TrainingPlan}`；`PATCH /training-plans/:id`：`{title?,stages?}` → `{item:TrainingPlan}`。管理员或该组有 assessment 权限的组管理员可维护。同一团队、组别只有一份方案；teamId 为空时为该组通用方案，指定团队须开放该方向。配置 1–12 个阶段，阶段 ID 唯一，名称最多 200 字节，说明最多 5000 字节；顺序按数组保存，只有最后阶段为 final。方案编辑只影响新培养周期。
- `GET /training-progress?teamId=&direction=&status=` → `{items:TrainingProgress[]}`，本人可查看自己全部周期，管理员全部，组管理员只可查看有 assessment 权限组的人员进度与反馈；跨组学习权限不开放他人记录。
- `POST /training-progress/decisions`：`{progressIds:string[],stageId,status:'passed'|'eliminated',note?,targetMemberType?:'reserve'|'formal'}` → `{items:TrainingProgress[]}`，管理员或该组有 assessment 权限者。每批 1–100 个唯一记录，必须均为该用户在相应团队／方向的最新培养周期当前阶段，并仍具备该周期团队／方向的有效录取关系；任一记录越权、录取撤销、过期、已处理或缺少前置通过记录时整批失败。同一批不能混合最终阶段与普通阶段，避免方案版本调整后部分人员意外晋升。组管理员不能确认自己的考核。反馈最多 5000 字节，成员本人可查看。

每个团队／方向首次录取时自动开始独立培养周期，已是正式／梯队队员再加入新组也需参加该组培养；依次选用团队组方案、该组通用方案、默认「基础学习 → 阶段考核 → 最终考核」。周期保存当时的完整阶段快照，后来调整方案不改写既有阶段与结果。同一人员可并行推进多个团队／方向的培养周期，`cycle` 是该用户所有周期共享的递增审计编号，不能用全局最大编号判断其他组周期失效。非最终阶段通过只推进本周期，不改变其他组进度；最终通过将本周期置 completed，按 targetMemberType（默认 reserve）提升账号统一成员身份，已有更高身份不降级，保留管理角色及原有授权。即使已在一组晋升，其他尚未完成周期仍需独立考核。任一阶段淘汰仅将本周期置 eliminated、移除对应团队／方向成员关系并关闭该范围提交资格，其他组的学习资格与考核不受影响；个人历史、荣誉和本人附件保留。

有进行中的培养周期时，不能通过人员身份接口跳过阶段晋升，也不能直接改换方向。对应组的淘汰历史不能通过身份调整或旧录取记录恢复；重新参加该组必须显式重开该团队／方向的新录取周期。`/training` 前端入口保留，未录取或已淘汰人员仍可查看自己的培养状态与历史。

撤销报名录取保留周期与已有决策，但不能继续确认后续阶段；恢复同一团队、方向的 accepted 后可继续当前未淘汰周期。申请响应的 trainingStatus／trainingStageTitle／trainingCycle 为相关培养状态展示，不等同原始报名状态。

## 荣誉中心与个人档案

```ts
interface Record {
  id: string; userId: string; userName: string; title: string;
  kind: 'certificate' | 'award' | 'notice'; description: string; fileIds: string[];
  issuedAt: string; createdAt: string; createdBy: string;
}
```

- `GET /records` → `{items:Record[]}`，管理员全部；有 records 权限的组管理员可读对应组人员档案和本人档案；其余仅本人。
- `POST /records`：`{userId,title,kind,description?,fileIds?,issuedAt?}` → 201 `{item:Record}`，管理员或有接收人所属组 records 权限的组管理员。接收人必须是已存在用户，默认发放时间为当前时间；档案和附件访问授权同一事务写入。档案通知不会自动发送邮件，邮件在批量邮件页面另行发出。

## 任务、成果与评分

```ts
interface JudgeCase { input: string; expectedOutput: string; hidden: boolean }
interface JudgeConfig {
  enabled: boolean; testCases: JudgeCase[]; languageIds: number[];
  timeLimitSeconds: number; totalCases: number;
}
interface MechanicalProblem {
  type: 'single_choice' | 'multiple_choice' | 'fill_blank' | 'drawing';
  options: string[]; answers: string[]; explanation: string; answerFileIds: string[];
  blankCount: number;
}
interface Task {
  id: string; title: string; description: string; direction: string; team: string; teamId?: string;
  category: 'assignment' | 'exam' | 'problem'; tags: string[]; locked: boolean;
  acceptedCount: number; attemptCount: number; myAccepted: boolean; assigned: boolean; canSubmit: boolean;
  kind: 'code' | 'document' | 'hardware' | 'quiz'; difficulty: '入门' | '进阶' | '挑战';
  startsAt?: string; dueAt: string; resourcesUrl: string; fileIds: string[];
  submissionType?: 'files' | 'project' | 'code'; judge?: JudgeConfig;
  mechanicalProblem?: MechanicalProblem;
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

- `GET /tasks` → `{items:Task[]}`。未取得学习访问权限的用户只返回其 tasks 管理授权范围内的任务，无管理授权则为空列表；已取得学习资格的人员可查看各组学习任务。`assigned` 表示任务属于本人的有效录取团队／方向，与开考和截止时间无关；作业／考试据此区分本组必做、选做和跨组参考。`canSubmit` 进一步校验时间窗口，表示当前是否可提交；录取、淘汰、团队和方向条件均由服务端判断。submissionCount 仅为有该任务管理权限者计算，其他人员返回 0，并仅获得自己的 mySubmission。题库 acceptedCount／attemptCount 为公开汇总，myAccepted 为本人是否通过；没有该任务管理权限的响应隐藏 `hidden:true` 的输入和预期输出，保留 `totalCases`。
- `POST /tasks`：Task 的非服务器管理字段 → 201 `{item:Task}`，管理员或对应组有 tasks 权限的组管理员。`fileIds` 为教材或题目附件；`submissionType` 区分一般文件、工程项目和单文件代码；机械题库通过 `mechanicalProblem` 设置选择／填空题或图纸练习。`teamId` 指定发布团队，`team` 为兼容旧数据的赛事字段。
- `GET /tasks/:id/submissions` → `{items:Submission[]}`，普通人员必须仍有该任务的访问权限，且仅返回本人记录；管理员或对应组有 tasks 权限的组管理员可评阅。入队资格撤销后，不再凭历史提交获得任务记录访问权。
- `POST /tasks/:id/submissions`：`{content,repoUrl?,artifactUrl?,fileIds?,judgeRunId?}` → 201 `{item:Submission}`。必须符合下述成员方向与团队规则，在开始与截止时间内；每次创建新版本。声明提交形式的任务必须上传文件，启用判题时必须引用本任务、本人、完成状态的判题记录以及对应代码文件；源代码哈希须一致。机械题库不使用此接口：选择／填空题通过 `/tasks/:id/practice` 作答，图纸题无需提交。
- `PATCH /submissions/:id`：`{status:'approved'|'changes_requested',score,feedback}` → `{item:Submission}`，管理员或对应组有 tasks 权限的组管理员，只可审核最新版本，分值范围为 0 至 task.maxScore。机械题库的自动答题成绩不允许通过此接口人工改分。

任务浏览跨组开放；带 teamId 的任务须有该团队／方向有效成员关系才会分配并允许提交，人员身份或另一组的录取不能绕过本组要求。本组最新周期淘汰后不再分配该任务，其他组任务不受影响。新任务应指定 teamId；不含 teamId 的历史任务按有效成员方向兼容校验。机械说明书、工程 ZIP、PCB、运营图片统一使用附件；这些文件由人工评分，工程 ZIP 不自动编译。

## 在线编译测试

- `GET /judge/config` → `{configured:boolean,languages:{id:number,name:string}[]}`，要求学习访问权限或 tasks 管理权限，否则返回 403。服务未配置时返回 false 和空列表；配置后由独立 Judge0 兼容 API 读取语言，服务不可用返回 503。查询配置不授予成员判题资格。
- `POST /tasks/:id/judge`：`{sourceFileId,languageId}` → 202 `{item:JudgeRun}`。仅对应已入队人员（含通过报名的新生），在允许的时间段、语言及启用判题的任务下发起。源代码必须本人上传，UTF-8 文本、1 字节至 64 KiB。每用户每分钟最多 5 次，进程最多同时处理 2 个请求。
- `GET /judge-runs/:id` → `{item:JudgeRun}`，本人、管理员或对应组有 tasks 权限的组管理员，且必须仍有对应任务的访问权限。前端轮询状态；不会因查询而重复编译。

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

创建判题任务需要 1–10 个用例和允许语言 ID；每用例输入、输出最多各 16,000 字节，CPU 时限 0.1–5 秒，默认 2 秒。编程考试必须设置开始和截止时间。结果分数为通过用例比例 × 任务满分，最终人工评分仍由审核接口完成。隐藏用例的 stdout/stderr/编译输出/耗时/内存仅管理员可见，组管理员可读授权组的判题状态与分数，但这些隐藏输出也被清空。Judge0 运行请求禁用网络、限制内存和进程数；沙箱必须独立部署并正确配置，Web 进程不运行提交代码。

## 批量邮件

接口供管理员及有 mail 权限的组管理员使用；组管理员仅可操作全部收件人仍属于当前授权组的批次。创建和发送时均重新校验收件人范围，后台逐人投递前再次读取实际发起发送者的身份与权限；调整权限后不能继续发送原有越权收件人。SMTP 未配置仍可保存草稿，但不能发送。收件人只能选择系统已有用户，不接受任意邮箱列表。

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
- `GET /workspace-summary` → `{stats:{openTeams,applications,pendingApplications,members,records,files,activeTasks,pendingReviews},pipeline:{status,label,count}[],profileComplete}`。新团队统计按当前管理模块、方向或本人范围计算，文件数始终仅本人。培养任务按成员学习资格或 tasks 管理授权计算；两者均无时任务和待评阅统计为 0。
- `GET /settings` → `{settings:{organization,season,applicationOpen,applicationDeadline,hydroUrl,announcement,brandName,logoUrl,teamName,secondaryTeamName,competitions}}`。
- `PATCH /settings`：上述字段部分更新 → `{settings}`，仅管理员。
- `POST /settings/logo`：`{name,contentBase64}` → `{settings}`，仅管理员；上传 PNG/JPEG，最大 2 MiB、单边 6000 像素且总像素不超过 2000 万，服务端清洗重编码并更新 logoUrl。
- `GET /public/logo/:id`：匿名读取当前使用的品牌标志；更换标志后旧地址撤销公开，个人图片不通过此入口暴露。
- `GET /settings/email` → `{configured,from,security,supportsBulk}`，仅管理员，不返回 SMTP 密钥或主机配置。
- `GET /members` → `{items:(User & {teams:{id,name,competition,competitions,direction}[]})[]}`，管理员可见全部账号，包括未报名／待审核的新生，以便直接分配身份和权限。组管理员名册限于有 members 权限的组内管理者、正式／梯队队员及已通过报名的新生，附加有权查看的新团队归属列表。
- `POST /members`：`{name,email,password,role,memberType?,groupGrants?,team?,direction?}` → 201 `{item:User}`，仅管理员。role 使用本契约的五类身份，后台密码为 12–72 字节。
- `PATCH /members/:id`：`{role?,memberType?,groupGrants?,direction?,assessmentPassed?,assessmentNote?}` → `{item:User}`。角色、组授权和方向分配仅管理员可操作，不能降级最后一位管理员；groupGrants 为完整替换，不能包含重复方向、重复或未知权限，普通角色不能携带管理权限。有培养周期的新生须使用阶段考核接口，不能直接晋升或绕过淘汰；培养进行中改换方向返回 409。没有培养周期的存量新生可由管理员或其所属组有 assessment 权限者明确传 `assessmentPassed:true`，晋升为 reserve／formal，并填写最多 5000 字节的说明；组管理员仅可做该最终身份确认，不能同时修改方向、角色或组授权，也不能确认自己。后台创建既有正式／梯队账号不要求补新生考核。

保留原有 `/dashboard`、`GET/POST /applicants`、`PATCH /applicants/:id` 以访问历史面试数据；它们不等同新团队报名。历史报名每用户一份，普通用户仅自己的，组管理员按 recruitment 权限及申请方向管理，管理员全部；内部评分和备注对普通人员隐藏。新功能应使用 `/teams`、`/team-applications`、`/memberships`。

同源部署，不开放通配 CORS。生产设置 `SESSION_SECURE=true`，SMTP 与判题服务密钥通过环境注入。数据库迁移 v4 新增知识库与讨论表，并迁移默认品牌及团队多赛事字段；v5 持久化五类角色、人员身份与各组授权。旧 mentor 按原方向迁为有 tasks／knowledge／discussions／members 的组管理员，旧 interviewer 按原方向迁为有 recruitment／discussions 的组管理员，旧 student 均迁为新生（含已报名通过账号），不自动认定新生考核通过。v6 增加培养方案、周期及阶段决策持久化；assessment 为独立授权，旧组管理员不会自动获得。部署路径和实际边界见[架构说明](architecture.md)。


## 分组学习、题库与定时考试

`GET /tasks` 支持 `category`、`direction`、`tag`、`search` 参数。未分类历史任务归入 assignment。题库 `category=problem` 支持机械、电控、视觉算法和硬件；运营不提供题库。题库沿用任务的团队、方向、学习访问和提交资格规则，其他组可浏览不代表可以提交。

机械题库必须设置 `mechanicalProblem`，并将 `submissionType` 留空。`single_choice`／`multiple_choice` 设置 2–20 个非空且不重复的 `options`，`answers` 为从 0 开始的选项索引字符串；单选仅一项，多选至少一项、不重复。`fill_blank` 设置 1–10 个按空位顺序排列的标准答案，不设置选项，服务端将 `blankCount` 设为答案个数。每个答案去除首尾空白后不得为空或超过 1000 字节，`explanation` 最多 20000 字节。题目列表对无本题 `tasks` 管理权限者返回 `answers=[]`、`explanation=''` 和 `answerFileIds=[]`，保留作答所需的 `options`、`blankCount`。

- `POST /tasks/:id/practice`：`{answers:string[]}` → 201 `{item:Submission,result:{correct:boolean,score:number,maxScore:number,answers:string[],explanation:string}}`。仅用于机械选择／填空题，要求本题所属团队／方向的有效录取资格，并遵守开始与截止时间。选择题按索引作答，多选答案顺序不限；填空按空位顺序作答，去除首尾空白后区分大小写、精确匹配。全部正确获得满分，否则为 0 分；响应的 `result.answers` 和 `explanation` 返回标准答案及解析。每次作答生成独立版本的 `Submission`，`reviewedBy='system'`，正确为 `approved`，错误为 `changes_requested`。`attemptCount` 统计已自动评阅的答题记录，`acceptedCount` 统计其中正确记录，本人曾答对则 `myAccepted=true`。

`drawing` 图纸题必须通过 `Task.fileIds` 提供至少一份题目图纸，可选 `explanation` 参考说明及 `answerFileIds` 参考答案文件，`options`、`answers` 为空。仅图纸题允许设置参考答案文件。可访问该题的学员可下载图纸和参考答案并自主练习；设置 `startsAt` 时，开始前对无本题管理权限者隐藏题面、机械题型配置和文件引用，文件下载同样受限。`canSubmit=false`，不提交作品、不产生评分或答题次数。机械题库不接受代码提交，不依赖 Judge0 配置。

电控、视觉算法与硬件的题库必须配置代码测试用例，AC／尝试统计来自实际完成的 `JudgeRun`，不含排队、运行中或服务错误记录。运营仍不接受代码提交或自动判题。

考试 category=exam 必须同时提供 startsAt / dueAt，开始时间早于截止时间。开考前普通用户可见标题和时间，但 locked=true、description/resourcesUrl 为空、fileIds 为空、judge 被移除；文件详情、下载和预览同样校验开考时间。开考前和截止后不可提交或发起判题。管理员及对应组有 tasks 权限者可提前管理题目；其他组管理员不能提前查看。

- `PATCH /tasks/:id` → `{item:Task}`，支持任务可编辑字段。管理员可改全部，组管理员仅可管理当前有 tasks 权限的方向；移动方向须同时具备目标方向权限。已有提交或判题记录时返回 409，须另发新版本。
- `DELETE /tasks/:id` → `{deleted:true}`，同上述权限；已有提交或判题时拒绝删除。

## 知识库

```ts
interface KnowledgeResource {
  id: string; title: string; description: string; direction: string;
  kind: 'file' | 'pdf' | 'video'; fileIds: string[]; url: string;
  createdBy: string; createdAt: string; updatedAt: string;
}
```

- `GET /knowledge-base?direction=&search=` → `{resources:KnowledgeResource[]}`，要求学习访问权限，或仅返回当前 knowledge 管理授权范围的资料；未入队且无授权人员返回 403。具备学习资格时按方向筛选也包含 direction 为空的共享资料。
- `POST /knowledge-base`：`{title,description?,direction?,kind,fileIds?,url?}` → 201 `{resource}`，管理员或对应组有 knowledge 权限的组管理员；至少一个附件或 HTTP(S) 资料链接。PDF 类型的附件必须为 PDF。
- `PATCH /knowledge-base/:id` → `{resource}`；`DELETE /knowledge-base/:id` → `{deleted:true}`。管理员全部可维护，组管理员按当前方向和 knowledge 权限维护；共享资料仅管理员维护。

知识库面向全部获得学习资格的人员，组别用于内容分类，浏览所有组不需要该组管理授权。附件授权依据当前学习资格或对应组 knowledge 管理权限及仍存在的资料引用动态判断，失去相关资格或移除引用后立即撤销该来源授权；原上传者、管理员或仍有其他合法引用者保留访问权。

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
- `POST /discussions`：`{title,content,direction?}` → 201 `{discussion}`；默认私问，仅本人、管理员和对应组有 discussions 权限者可见。
- `POST /discussions/:id/replies`：`{content}` → 201 `{discussion}`；仅作者、管理员、对应组有 discussions 权限者可回复，最多 200 条；公开精选在取消公开前禁止新增回复。
- `PATCH /discussions/:id`：`{status?,published?}` → `{discussion}`。管理员或对应组有 discussions 权限者可标记解答（必须已有回复）及公开已解答的问题。公开指所有已登录用户可查阅，匿名访问仍需登录。

## 品牌配置

默认 brandName=QUT机器人战队、organization=青岛理工大学机器人战队、teamName=沧溟、secondaryTeamName=MOVE、competitions=[RoboCon,RoboMaster]、logoUrl=/mark.png。队名为同一战队的名称。管理员可通过设置更新这些字段；标志支持 HTTP(S) URL、本站静态资源路径及专用 /api/public/logo/:id 地址，不能使用私有附件 API 地址。公开配置不包含 SMTP/Judge 密钥或成员资料。旧默认 `/mark.svg` 会迁移到用户提供的 M 标志，自定义标志地址保留。
