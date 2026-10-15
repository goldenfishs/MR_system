package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

// NoticeMailer deliberately sends one recipient per message. No recipient list is
// ever passed to SMTP, and draft creation never triggers delivery.
type NoticeMailer interface {
	SendMessage(context.Context, string, string, string) error
}

type MailRecipient struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	SentAt string `json:"sentAt,omitempty"`
}
type MailCampaign struct {
	ID         string          `json:"id"`
	Subject    string          `json:"subject"`
	Body       string          `json:"body"`
	Status     string          `json:"status"`
	Recipients []MailRecipient `json:"recipients"`
	CreatedBy  string          `json:"createdBy"`
	CreatedAt  string          `json:"createdAt"`
	UpdatedAt  string          `json:"updatedAt"`
}

func normalizedIDs(ids []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}
func includesID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func (a *App) taskVisibleTo(t Task, u User) (bool, error) {
	if hasPermission(u, t.Direction, "tasks") {
		return true, nil
	}
	return a.learningAllowed(u)
}
func (a *App) requireTaskEnrollment(t Task, u User) error {
	if eliminated, err := trainingEliminated(a.store, u); err != nil {
		return err
	} else if eliminated {
		return fail(403, "新生已在阶段考核中淘汰，重新录取后才可提交")
	}
	if err := a.requireLearningAccess(u); err != nil {
		return err
	}
	directions, err := a.memberDirections(u.ID)
	if err != nil {
		return err
	}
	if u.MemberType == "freshman" && u.Role != "admin" && len(directions) == 0 {
		return fail(403, "管理授权不替代本人录取资格，报名通过后才可提交")
	}
	if t.TeamID != "" {
		var direction string
		err := a.store.row("SELECT direction FROM team_memberships WHERE team_id=? AND user_id=?", t.TeamID, u.ID).Scan(&direction)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows || !includesID(directions, direction) {
			return fail(403, "加入对应团队后才可提交任务")
		}
		progress, err := latestTrainingInScope(a.store, u.ID, t.TeamID, direction)
		if err != nil {
			return err
		}
		if progress != nil && progress.Status == "eliminated" {
			return fail(403, "该组阶段考核已淘汰，重新录取后才可提交")
		}
		directions = []string{direction}
	}
	if t.Direction != "" && !includesID(directions, t.Direction) {
		return fail(403, "请提交本人所属技术组的作业或考试")
	}
	return nil
}
func (a *App) requireTaskSubmission(t Task, u User) error {
	if err := a.requireTaskEnrollment(t, u); err != nil {
		return err
	}
	if t.StartsAt != "" {
		start, err := time.Parse(time.RFC3339, t.StartsAt)
		if err != nil || time.Now().Before(start) {
			return fail(409, "该任务尚未开始")
		}
	}
	if expired(t.DueAt) {
		return fail(409, "该任务已截止")
	}
	return nil
}
func (a *App) validateTaskPlatform(t *Task, u User, existing ...[]string) error {
	if err := requirePermission(u, t.Direction, "tasks"); err != nil {
		return err
	}
	normalizeTask(t)
	if (t.Direction == "机械" || t.Direction == "运营") && (t.Kind == "code" || t.SubmissionType == "code" || t.Judge != nil) {
		return fail(400, "机械和运营组使用文字或文件作业，不启用代码题与编程测评")
	}
	if t.Category != "assignment" && t.Category != "exam" && t.Category != "problem" {
		return fail(400, "无效的内容分类")
	}
	if t.Category == "exam" && (t.StartsAt == "" || t.DueAt == "") {
		return fail(400, "考试必须设置发布开始时间和截止时间")
	}
	if t.Category == "problem" && t.Direction != "机械" && (t.Direction != "电控" && t.Direction != "视觉算法" && t.Direction != "硬件" || t.Kind != "code" || t.Judge == nil) {
		return fail(400, "机械题库使用选择、填空或图纸题；电控、视觉算法和硬件题库须配置在线编程测试用例")
	}
	if len(t.Tags) > 10 {
		return fail(400, "最多设置 10 个题目标签")
	}
	for _, tag := range t.Tags {
		if tag == "" || len(tag) > 60 {
			return fail(400, "题目标签不能为空或超过 60 字节")
		}
	}

	if t.TeamID != "" {
		team, err := readOne[Team](a.store, "SELECT payload FROM teams WHERE id=?", t.TeamID)
		if errors.Is(err, sql.ErrNoRows) {
			return fail(400, "请选择已发布的团队")
		} else if err != nil {
			return err
		}
		if t.Direction != "" && !includesID(team.Directions, t.Direction) {
			return fail(400, "任务技术组须属于所选团队的招募方向")
		}
	}
	if !validDate(t.StartsAt) {
		return fail(400, "开始时间不合法")
	}
	if t.StartsAt != "" && t.DueAt != "" {
		start, _ := time.Parse(time.RFC3339, t.StartsAt)
		end, _ := time.Parse(time.RFC3339, t.DueAt)
		if !start.Before(end) {
			return fail(400, "截止时间应晚于开始时间")
		}
	}
	switch t.SubmissionType {
	case "", "files", "project", "code":
	default:
		return fail(400, "无效的提交形式")
	}
	if len(t.FileIDs) > 20 {
		return fail(400, "每项任务最多附带 20 个文件")
	}
	t.FileIDs = normalizedIDs(t.FileIDs)
	previous := []string{}
	if len(existing) > 0 {
		previous = existing[0]
	}
	if err := a.validateMechanicalProblem(t, u, previous); err != nil {
		return err
	}
	if err := requireUpdatedFiles(a.store, u, t.FileIDs, previous); err != nil {
		return err
	}
	return validateJudgeConfig(t)
}
func (a *App) validateSubmissionFiles(t Task, u User, fileIDs []string, runID string) error {
	if len(fileIDs) > 20 {
		return fail(400, "每份成果最多上传 20 个文件")
	}
	if t.SubmissionType != "" && len(fileIDs) == 0 {
		return fail(400, "请上传任务成果文件")
	}
	if err := requireOwnedFiles(a.store, u, fileIDs); err != nil {
		return err
	}
	if t.Judge != nil && runID == "" {
		return fail(400, "请先完成在线编译测试再提交")
	}
	if runID == "" {
		return nil
	}
	run, err := readOne[JudgeRun](a.store, "SELECT payload FROM judge_runs WHERE id=?", runID)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(400, "判题记录不存在")
	}
	if err != nil {
		return err
	}
	if run.UserID != u.ID || run.TaskID != t.ID || run.Status != "completed" || !includesID(fileIDs, run.SourceFileID) {
		return fail(400, "请使用本任务已完成的本人判题记录及对应源代码文件")
	}
	source, err := a.readJudgeSource(run.SourceFileID, u)
	if err != nil {
		return err
	}
	if tokenHash(string(source)) != run.SourceHash {
		return fail(409, "源代码文件与判题记录不一致，请重新判题")
	}
	return nil
}

// Called by the shared file download handler after owner, admin and explicit
// grants. Permissions are derived from current memberships, not a permanent grant.
func (a *App) taskFileAccess(u User, fileID string) (bool, error) {
	tasks, err := readAll[Task](a.store, "SELECT payload FROM tasks")
	if err != nil {
		return false, err
	}
	for _, t := range tasks {
		isReference := t.MechanicalProblem != nil && t.MechanicalProblem.Type == "drawing" && includesID(t.MechanicalProblem.AnswerFileIDs, fileID)
		if (includesID(t.FileIDs, fileID) || isReference) && !taskLocked(t, u) {
			visible, err := a.taskVisibleTo(t, u)
			if err != nil || visible {
				return visible, err
			}
		}
	}
	submissions, err := readAll[Submission](a.store, "SELECT payload FROM submissions")
	if err != nil {
		return false, err
	}
	for _, s := range submissions {
		if includesID(s.FileIDs, fileID) {
			task, err := a.store.task(s.TaskID)
			if err != nil {
				return false, err
			}
			if s.UserID == u.ID || hasPermission(u, task.Direction, "tasks") {
				return true, nil
			}
		}
	}
	return false, nil
}

func (a *App) registerTaskPlatformRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/tasks/{id}/practice", a.wrap(true, a.practiceMechanicalProblem))
	m.HandleFunc("GET /api/mail-campaigns", a.wrap(true, a.listMailCampaigns))
	m.HandleFunc("POST /api/mail-campaigns", a.wrap(true, a.createMailCampaign))
	m.HandleFunc("GET /api/mail-campaigns/{id}", a.wrap(true, a.getMailCampaign))
	m.HandleFunc("POST /api/mail-campaigns/{id}/send", a.wrapWithMutationLock(true, false, a.sendMailCampaign))
	m.HandleFunc("POST /api/mail-campaigns/{id}/retry", a.wrapWithMutationLock(true, false, a.retryMailCampaign))
	m.HandleFunc("GET /api/judge/config", a.wrap(true, a.getJudgeConfig))
	m.HandleFunc("POST /api/tasks/{id}/judge", a.wrapWithMutationLock(true, false, a.startJudge))
	m.HandleFunc("GET /api/judge-runs/{id}", a.wrap(true, a.getJudgeRun))
}
func (a *App) listMailCampaigns(w http.ResponseWriter, _ *http.Request, u User) error {
	if !hasAnyPermission(u, "mail") {
		return fail(403, "没有批量邮件权限")
	}
	items, err := readAll[MailCampaign](a.store, "SELECT payload FROM mail_campaigns")
	if err != nil {
		return err
	}
	filtered := []MailCampaign{}
	for _, item := range items {
		allowed, err := a.campaignAllowed(u, item)
		if err != nil {
			return err
		}
		if allowed {
			filtered = append(filtered, item)
		}
	}
	items = filtered
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	if len(items) > 100 {
		items = items[:100]
	}
	respond(w, 200, map[string]any{"items": items, "configured": a.currentMailService().notice != nil})
	return nil
}
func (a *App) createMailCampaign(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "mail") {
		return fail(403, "没有批量邮件权限")
	}
	var in struct {
		Subject string   `json:"subject"`
		Body    string   `json:"body"`
		UserIDs []string `json:"userIds"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimSpace(in.Body)
	if in.Subject == "" || len(in.Subject) > 200 || strings.ContainsAny(in.Subject, "\r\n") || in.Body == "" || len(in.Body) > 50000 || len(in.UserIDs) == 0 || len(in.UserIDs) > 200 {
		return fail(400, "请填写邮件主题、正文，并选择 1 至 200 位收件人")
	}
	campaign := MailCampaign{ID: randomID(), Subject: in.Subject, Body: in.Body, Status: "draft", CreatedBy: u.ID, CreatedAt: now(), UpdatedAt: now(), Recipients: []MailRecipient{}}
	for _, id := range normalizedIDs(in.UserIDs) {
		member, err := a.store.user(id)
		if errors.Is(err, sql.ErrNoRows) {
			return fail(400, "收件人不存在")
		}
		if err != nil {
			return err
		}
		allowed, err := a.recipientAllowed(u, id, "mail")
		if err != nil {
			return err
		}
		if !allowed {
			return fail(403, "收件人不在获授权的技术组内")
		}
		if _, valid := normalizedEmail(member.Email); !valid {
			return fail(400, "收件人邮箱无效")
		}
		campaign.Recipients = append(campaign.Recipients, MailRecipient{UserID: id, Name: member.Name, Email: member.Email, Status: "pending"})
	}
	if err := a.store.exec("INSERT INTO mail_campaigns(id,status,created_by,payload) VALUES(?,?,?,?)", campaign.ID, campaign.Status, u.ID, encoded(campaign)); err != nil {
		return err
	}
	a.activity(u, "创建邮件草稿", campaign.Subject)
	respond(w, 201, map[string]any{"item": campaign})
	return nil
}
func (a *App) getMailCampaign(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "mail") {
		return fail(403, "没有批量邮件权限")
	}
	item, err := readOne[MailCampaign](a.store, "SELECT payload FROM mail_campaigns WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	allowed, err := a.campaignAllowed(u, item)
	if err != nil {
		return err
	}
	if !allowed {
		return fail(403, "没有此邮件的收件人授权")
	}
	respond(w, 200, map[string]any{"item": item})
	return nil
}
func (a *App) sendMailCampaign(w http.ResponseWriter, r *http.Request, u User) error {
	return a.beginMailCampaign(w, r, u, false)
}
func (a *App) retryMailCampaign(w http.ResponseWriter, r *http.Request, u User) error {
	return a.beginMailCampaign(w, r, u, true)
}
func (a *App) beginMailCampaign(w http.ResponseWriter, r *http.Request, u User, retry bool) error {
	if !hasAnyPermission(u, "mail") {
		return fail(403, "没有批量邮件权限")
	}
	mailer := a.currentMailService().notice
	if mailer == nil {
		return fail(503, "尚未配置 SMTP，无法发送邮件")
	}
	a.platformMu.Lock()
	defer a.platformMu.Unlock()
	item, err := readOne[MailCampaign](a.store, "SELECT payload FROM mail_campaigns WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	allowed, err := a.campaignAllowed(u, item)
	if err != nil {
		return err
	}
	if !allowed {
		return fail(403, "没有此邮件的收件人授权")
	}
	// Returning the same in-flight or completed item makes accidental repeated
	// clicks idempotent. A retry is a distinct, explicitly requested operation.
	if item.Status == "sending" || !retry && item.Status != "draft" {
		respond(w, 200, map[string]any{"item": item})
		return nil
	}
	var activeCampaigns int
	if err = a.store.row("SELECT COUNT(*) FROM mail_campaigns WHERE status='sending'").Scan(&activeCampaigns); err != nil {
		return err
	}
	if activeCampaigns >= 2 {
		return &apiError{status: 429, message: "最多同时发送两批邮件，请稍后重试", retryAfterSeconds: 10}
	}
	eligible := false
	for _, recipient := range item.Recipients {
		if !retry && recipient.Status == "pending" || retry && recipient.Status == "failed" {
			eligible = true
		}
	}
	if !eligible {
		return fail(409, "没有可发送的收件人；状态不明的邮件请先人工核对，系统不会自动重发")
	}
	previousStatus := item.Status
	item.Status = "sending"
	item.UpdatedAt = now()
	result, err := a.store.db.Exec(a.store.q("UPDATE mail_campaigns SET status=?,payload=? WHERE id=? AND status=?"), item.Status, encoded(item), item.ID, previousStatus)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fail(409, "该邮件正在发送，请刷新状态")
	}
	workerItem := item
	workerItem.Recipients = append([]MailRecipient{}, item.Recipients...)
	go a.deliverMailCampaign(workerItem, retry, u.ID, mailer)
	respond(w, 202, map[string]any{"item": item})
	return nil
}
func (a *App) saveMailCampaign(item MailCampaign) error {
	return a.store.exec("UPDATE mail_campaigns SET status=?,payload=? WHERE id=?", item.Status, encoded(item), item.ID)
}
func (a *App) deliverMailCampaign(item MailCampaign, retry bool, actorID string, mailer NoticeMailer) {
	for i := range item.Recipients {
		recipient := &item.Recipients[i]
		if !retry && recipient.Status != "pending" || retry && recipient.Status != "failed" {
			continue
		}
		actor, err := a.store.user(actorID)
		allowed := false
		if err == nil {
			allowed, err = a.recipientAllowed(actor, recipient.UserID, "mail")
		}
		if err != nil || !allowed {
			recipient.Status = "failed"
			recipient.Error = "收件人授权已失效"
			_ = a.saveMailCampaign(item)
			continue
		}
		recipient.Status = "sending"
		recipient.Error = ""
		item.UpdatedAt = now()
		if err := a.saveMailCampaign(item); err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err = mailer.SendMessage(ctx, recipient.Email, item.Subject, item.Body)
		cancel()
		if err != nil {
			recipient.Status = "failed"
			recipient.Error = "邮件服务投递失败，可重试"
			var uncertain *uncertainDeliveryError
			if errors.As(err, &uncertain) {
				recipient.Status = "unknown"
				recipient.Error = "邮件是否投递成功尚不确定，请人工核对后处理"
			}
		} else {
			recipient.Status = "sent"
			recipient.SentAt = now()
		}
		item.UpdatedAt = now()
		if err := a.saveMailCampaign(item); err != nil {
			return
		}
	}
	item.Status = "completed"
	for _, recipient := range item.Recipients {
		if recipient.Status != "sent" {
			item.Status = "partial"
			break
		}
	}
	item.UpdatedAt = now()
	_ = a.saveMailCampaign(item)
}

// A crash cannot prove whether the SMTP server accepted an in-flight message.
// Mark it unknown and never automatically resend it. Unattempted recipients and
// judge runs are safe to retry through an explicit user action.
func (a *App) recoverPlatformJobs() error {
	campaigns, err := readAll[MailCampaign](a.store, "SELECT payload FROM mail_campaigns WHERE status='sending'")
	if err != nil {
		return err
	}
	for _, item := range campaigns {
		for i := range item.Recipients {
			recipient := &item.Recipients[i]
			if recipient.Status == "sending" {
				recipient.Status = "unknown"
				recipient.Error = "服务重启时邮件正在发送，请人工核对"
			} else if recipient.Status == "pending" {
				recipient.Status = "failed"
				recipient.Error = "服务重启前未发送，可重试"
			}
		}
		item.Status = "partial"
		item.UpdatedAt = now()
		if err = a.saveMailCampaign(item); err != nil {
			return err
		}
	}
	runs, err := readAll[JudgeRun](a.store, "SELECT payload FROM judge_runs WHERE status IN ('queued','running')")
	if err != nil {
		return err
	}
	for _, run := range runs {
		run.Status = "error"
		run.Error = "判题服务重启，请重新提交编译测试"
		run.FinishedAt = now()
		if err = a.saveJudgeRun(run); err != nil {
			return err
		}
	}
	return nil
}
