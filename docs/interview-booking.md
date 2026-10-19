# 面试时段与自主预约

管理员、获授权的组管理员在「面试管理」中为招募团队和组别创建预约场次：选择开始时间、教室、每个时段长度、时段数量和每个时段人数。网页按北京时间展示，接口以带时区的 RFC3339 时间传输。

报名状态与面试状态均为「待面试」的成员可以按日期选择时段，查看剩余名额、改签或取消自己的预约。多组报名各自预约；同一人的时段不能重叠。免试、已录取、已拒绝的报名不再参与预约。管理员可以代成员选择时段；此操作不会确认面试通过，结果仍在原面试管理流程中处理。

时段停止预约后保留已有预约。调整容量不能少于当前待面试人数；已有预约时不允许更改时间或教室，须先取消或改签。过期时段不可预约或重新开放；已开始的预约只能由有权限的管理员取消。不同场次不能占用同一教室的重叠时段，手动排期也遵循此规则。按准确教室名称判断场地相同，建议统一名称，例如「教学楼 A201」。

原「批量排期」仍可使用。手动排期会释放成员原预约名额；批量变更在一个事务内完成，失败时不保存任何部分修改。新排期记录实际时长；升级前没有时长的旧手动排期在冲突判断中按 15 分钟处理。

面试通知邮件仍通过原通知流程生成并人工发送，本功能不会自动发送邮件。改签会让旧排期邮件草稿失效，需重新生成。面试结束后保留报名记录中的原时间、教室和时段引用用于追溯。

## API

所有接口要求登录，管理操作检查对应组别的 recruitment 权限。

- GET /api/interview-slots?teamId=&direction=&applicationId=：返回 items、myBookings、serverTime。学生仅能看到本人待面试报名对应的场次；无姓名、简历、邮箱或其他报名者资料。myBookings 仅包含当前用户本人的 applicationId、slotId。
- POST /api/interview-slots/batch：参数 teamId、direction、startAt、intervalMinutes（1–120）、count（1–100）、capacity（1–100）、room。返回 201 和 items。
- PATCH /api/interview-slots/{id}：参数 room、capacity、closed；还支持 startAt、intervalMinutes 调整无预约时段。返回 item。
- POST /api/interview-slots/{id}/book：参数 applicationId。为本人预约，或由管理员代其有管理权限组别的成员安排；已有预约时原子改签。返回更新后的 TeamApplication，interview 新增 slotId 和 durationMinutes。
- DELETE /api/interviews/{applicationId}/booking：取消预约并清除排期字段，返回更新后的 TeamApplication。此接口不取消原报名，也不改变报名审核结果。

满额、时间冲突、关闭、过期、已处理报名等返回 409。越权返回 403。跨团队/组别时段返回 400。

## 存储与并发

时段保存在 settings 的独立 interview-slots-v1 行，无需修改数据库结构；预约只保存在报名记录中，避免双份状态失配。同步使用事务内数据库行写锁，覆盖预约、取消、批量排期、审核、面试结果和重新报名。SQLite 与 PostgreSQL 均支持，跨独立进程/连接保护最后一个名额。计数只包含待面试预约，已处理记录保留历史但不占用可用名额。

后端测试包含隐私、权限范围、满额、并发最后名额、失败改签回滚、取消释放、跨组冲突、教室冲突、批量重排、旧排期兼容和 PostgreSQL 双连接并发及重连持久性。PostgreSQL 测试需 TEST_DATABASE_URL 指向允许创建测试 schema 的专用测试数据库。
