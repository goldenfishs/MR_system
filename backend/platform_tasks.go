package main

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// NoticeMailer deliberately sends one recipient per message. No recipient list is
// ever passed to SMTP, and draft creation never triggers delivery.
type NoticeMailer interface {
	SendMessage(context.Context, string, string, string) error
}

type MailRecipient struct {
	Body                   string `json:"body,omitempty"`
	InterviewApplicationID string `json:"interviewApplicationId,omitempty"`
	InterviewSchedule      string `json:"interviewSchedule,omitempty"`
	UserID                 string `json:"userId"`
	Name                   string `json:"name"`
	Email                  string `json:"email"`
	Status                 string `json:"status"`
	Error                  string `json:"error,omitempty"`
	SentAt                 string `json:"sentAt,omitempty"`
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
	normalizeTask(&t)
	if t.BankVisibility == "internal" {
		return false, nil
	}
	if t.Status != "published" {
		return false, nil
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
	normalizeTask(&t)
	if t.BankVisibility == "internal" && !hasPermission(u, t.Direction, "tasks") {
		return fail(403, "内部题库仅限对应组的题目管理员使用")
	}
	if t.Status != "published" {
		return fail(409, "该任务尚未发布")
	}
	if t.Category == "problem" {
		if err := a.requireLearningAccess(u); err != nil {
			return err
		}
	} else if err := a.requireTaskEnrollment(t, u); err != nil {
		return err
	}
	if t.StartsAt != "" {
		start, err := time.Parse(time.RFC3339, t.StartsAt)
		if err != nil || time.Now().Before(start) {
			return fail(409, "该任务尚未开始")
		}
	}
	redoAllowed := false
	if t.Category == "assignment" {
		items, err := a.store.submissions(t.ID)
		if err != nil {
			return err
		}
		if latest := latestSubmissionForUser(items, u.ID); latest != nil {
			if latest.Status == "approved" {
				return fail(409, "作业已批改确认，请联系管理员打回重做后再提交")
			}
			redoAllowed = latest.Status == "changes_requested" || (latest.Status == "draft" && latest.RedoAllowed)
		}
	}
	if expired(t.DueAt) && !redoAllowed {
		return fail(409, "该任务已截止")
	}
	return nil
}

// Shared by interactive publishing, editing and built-in question bank import.
// All entry points must apply the same content, scope and grading validation.
func (a *App) validateTaskContent(t *Task, u User, previousFiles ...[]string) error {
	t.Title = strings.TrimSpace(t.Title)
	t.Description = strings.TrimSpace(t.Description)
	if t.Title == "" || len(t.Title) > 250 || t.Description == "" || len(t.Description) > 30000 || (t.Team != "" && !validTeam(t.Team)) || (t.Direction != "" && !validDirection(t.Direction)) || !validDate(t.DueAt) || !validURL(t.ResourcesURL) || !finite(t.MaxScore) || t.MaxScore <= 0 || t.MaxScore > 1000 {
		return fail(400, "请填写有效的任务内容、截止日期、分值和适用范围")
	}
	normalizeTask(t)
	if t.Status != "draft" && t.Status != "published" {
		return fail(400, "任务状态只能为草稿或已发布")
	}
	switch t.Kind {
	case "code", "document", "hardware", "quiz":
	default:
		return fail(400, "无效的任务类型")
	}
	switch t.Difficulty {
	case "入门", "进阶", "挑战":
	default:
		return fail(400, "无效的任务难度")
	}
	if err := validateIllustration(t.IllustrationURL, t.IllustrationAlt); err != nil {
		return err
	}
	return a.validateTaskPlatform(t, u, previousFiles...)
}

func (a *App) validateTaskPlatform(t *Task, u User, existing ...[]string) error {
	normalizeTask(t)
	if t.BankVisibility != "public" && t.BankVisibility != "internal" {
		return fail(400, "题库可见范围只能为公开或内部")
	}
	if t.BankVisibility == "internal" && t.Category != "problem" {
		return fail(400, "内部可见范围仅适用于题库题目")
	}
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
	if t.Category == "problem" {
		if !supportsProblemBank(t.Direction) {
			return fail(400, "题库仅支持机械、电控、算法和硬件组")
		}
		if t.Direction != "机械" && t.MechanicalProblem == nil && (t.Kind != "code" || t.Judge == nil) {
			return fail(400, "请选择客观题并配置答案，或为编程题配置在线测试用例")
		}
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
	if err := validateTaskQuestions(t); err != nil {
		return err
	}
	if err := requireUpdatedFiles(a.store, u, t.FileIDs, previous); err != nil {
		return err
	}
	for _, question := range t.Questions {
		allowed := append([]string{}, previous...)
		preservedSource := false
		if len(existing) > 0 && question.SourceProblemID != "" {
			if old, err := a.store.task(t.ID); err == nil {
				for _, oldQuestion := range old.Questions {
					if oldQuestion.ID == question.ID && oldQuestion.SourceProblemID == question.SourceProblemID {
						preservedSource = true
						break
					}
				}
			}
		}
		if question.SourceProblemID != "" && !preservedSource {
			source, err := a.store.task(question.SourceProblemID)
			if err != nil {
				return fail(400, "来源题目不存在，请重新选题")
			}
			if source.Category != "problem" || !hasPermission(u, source.Direction, "tasks") {
				return fail(403, "没有来源题目的出题权限")
			}
			allowed = append(allowed, source.FileIDs...)
		}
		if err := requireUpdatedFiles(a.store, u, question.FileIDs, allowed); err != nil {
			return err
		}
	}
	if t.Direction == "机械" {
		if err := a.requireMechanicalLearningFiles(taskLearningFileIDs(*t)); err != nil {
			return err
		}
	}
	return validateJudgeConfig(t)
}

func taskLearningFileIDs(t Task) []string {
	ids := append([]string{}, t.FileIDs...)
	for _, question := range t.Questions {
		ids = append(ids, question.FileIDs...)
	}
	if t.MechanicalProblem != nil {
		ids = append(ids, t.MechanicalProblem.AnswerFileIDs...)
	}
	return ids
}

func validTaskQuestionID(id string) bool {
	if id == "" || len(id) > 100 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func validateTaskQuestions(t *Task) error {
	if err := validateQuestionGroups(t.Questions); err != nil {
		return err
	}
	if len(t.Questions) == 0 {
		return nil
	}
	if t.Category != "assignment" {
		return fail(400, "仅作业支持分题配置")
	}
	if len(t.Questions) > 50 {
		return fail(400, "每份作业最多设置 50 道题")
	}
	seen := map[string]bool{}
	total := 0.0
	for i := range t.Questions {
		question := &t.Questions[i]
		question.ID = strings.TrimSpace(question.ID)
		question.Title = strings.TrimSpace(question.Title)
		question.Description = strings.TrimSpace(question.Description)
		if !validTaskQuestionID(question.ID) || seen[question.ID] {
			return fail(400, "作业题目编号不能为空、重复或格式无效")
		}
		seen[question.ID] = true
		if question.Title == "" || len(question.Title) > 200 || len(question.Description) > 20000 || len(question.FileIDs) > 20 || !finite(question.MaxScore) || question.MaxScore <= 0 || question.MaxScore > t.MaxScore {
			return fail(400, "请填写有效的题目标题、附件和分值")
		}
		switch question.Type {
		case "single_choice", "multiple_choice":
			if question.Judge != nil {
				return fail(400, "只有代码题可以配置在线判题")
			}
			if len(question.Options) < 2 || len(question.Options) > 20 {
				return fail(400, "选择题需要设置 2–20 个选项")
			}
			for optionIndex := range question.Options {
				question.Options[optionIndex] = strings.TrimSpace(question.Options[optionIndex])
				if question.Options[optionIndex] == "" || len(question.Options[optionIndex]) > 1000 {
					return fail(400, "选择题选项不能为空或超过 1000 字节")
				}
			}
		case "fill_blank", "short_answer", "drawing":
			if question.Judge != nil {
				return fail(400, "只有代码题可以配置在线判题")
			}
			if len(question.Options) > 0 {
				return fail(400, "非选择题不能包含选项")
			}
		case "code":
			if t.Direction == "机械" || t.Direction == "运营" {
				return fail(400, "机械组和运营组使用文字或文件作业，不设置代码自动评测")
			}
			if len(question.Options) > 0 {
				return fail(400, "代码题不能包含选择选项")
			}
			if question.Judge == nil {
				return fail(400, "代码题必须配置在线判题测试用例")
			}
			if !question.Judge.Enabled {
				return fail(400, "代码题必须启用在线自动判题")
			}
			judgeTask := *t
			judgeTask.Kind = "code"
			judgeTask.SubmissionType = "code"
			judgeTask.MaxScore = question.MaxScore
			judgeTask.Judge = question.Judge
			if err := validateJudgeConfig(&judgeTask); err != nil {
				return err
			}
			question.Judge = judgeTask.Judge
		default:
			return fail(400, "请选择有效的作业题型")
		}
		if err := validateTaskQuestionGrading(question); err != nil {
			return err
		}
		if question.Options == nil {
			question.Options = []string{}
		}
		question.FileIDs = normalizedIDs(question.FileIDs)
		if t.Status == "published" && strings.TrimSpace(question.Description) == "" && len(question.FileIDs) == 0 {
			return fail(400, "每道题至少需要填写题目说明或上传题目文件")
		}
		total += question.MaxScore
	}
	if math.Abs(total-t.MaxScore) > 0.000001 {
		return fail(400, "各题分值总和必须等于作业总分")
	}
	return nil
}

// Reference answers are private configuration, never embedded in feedback.
func validateTaskQuestionGrading(q *TaskQuestion) error {
	q.Explanation = strings.TrimSpace(q.Explanation)
	if len(q.Explanation) > 20000 || len(q.Answers) > 20 {
		return fail(400, "答案解析不能超过 20000 字节，标准答案最多 20 项")
	}
	objective := q.Type == "single_choice" || q.Type == "multiple_choice" || q.Type == "fill_blank"
	if !objective && (q.AutoGrade || len(q.Answers) > 0) {
		return fail(400, "仅选择题和填空题支持标准答案自动评分")
	}
	if q.AutoGrade && len(q.Answers) == 0 {
		return fail(400, "自动评分题必须填写标准答案")
	}
	if q.Type == "single_choice" && len(q.Answers) > 1 {
		return fail(400, "单选题只能设置一个标准答案")
	}
	seen := map[string]bool{}
	for i := range q.Answers {
		answer := strings.TrimSpace(q.Answers[i])
		if answer == "" || len(answer) > 20000 || seen[answer] {
			return fail(400, "标准答案不能为空、重复或超过 20000 字节")
		}
		if q.Type == "single_choice" || q.Type == "multiple_choice" {
			index, err := strconv.Atoi(answer)
			if err != nil || index < 0 || index >= len(q.Options) || strconv.Itoa(index) != answer {
				return fail(400, "标准答案的选项编号无效")
			}
		}
		seen[answer] = true
		q.Answers[i] = answer
	}
	return nil
}

func submissionHasFile(s Submission, fileID string) bool {
	for _, answer := range s.Answers {
		if includesID(answer.FileIDs, fileID) {
			return true
		}
	}
	return false
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
	if run.Preview || run.UserID != u.ID || run.TaskID != t.ID || run.Status != "completed" || !includesID(fileIDs, run.SourceFileID) {
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
		isQuestionFile := false
		for _, question := range t.Questions {
			if includesID(question.FileIDs, fileID) {
				isQuestionFile = true
				break
			}
		}
		if (includesID(t.FileIDs, fileID) || isReference || isQuestionFile) && !taskLocked(t, u) {
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
		if includesID(s.FileIDs, fileID) || submissionHasFile(s, fileID) {
			task, err := a.store.task(s.TaskID)
			if err != nil {
				return false, err
			}
			if s.UserID == u.ID || hasPermission(u, task.Direction, "tasks") {
				return true, nil
			}
		}
		for _, answer := range s.Answers {
			if answer.JudgeRunID == "" {
				continue
			}
			run, err := readOne[JudgeRun](a.store, "SELECT payload FROM judge_runs WHERE id=?", answer.JudgeRunID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return false, err
			}
			if run.TaskID != s.TaskID || run.UserID != s.UserID || run.QuestionID != answer.QuestionID || run.SourceFileID != fileID {
				continue
			}
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
	m.HandleFunc("POST /api/tasks/{id}/submission-draft", a.wrap(true, a.saveTaskSubmissionDraft))
	m.HandleFunc("POST /api/tasks/{id}/submission-finalize", a.wrap(true, a.finalizeTaskSubmission))
	m.HandleFunc("GET /api/tasks/{id}/submissions/export", a.wrap(true, a.exportTaskSubmissions))
	m.HandleFunc("GET /api/mail-campaigns", a.wrap(true, a.listMailCampaigns))
	m.HandleFunc("POST /api/mail-campaigns", a.wrap(true, a.createMailCampaign))
	m.HandleFunc("GET /api/mail-campaigns/{id}", a.wrap(true, a.getMailCampaign))
	m.HandleFunc("POST /api/mail-campaigns/{id}/send", a.wrapWithMutationLock(true, false, a.sendMailCampaign))
	m.HandleFunc("POST /api/mail-campaigns/{id}/retry", a.wrapWithMutationLock(true, false, a.retryMailCampaign))
	m.HandleFunc("GET /api/judge/config", a.wrap(true, a.getJudgeConfig))
	m.HandleFunc("POST /api/tasks/{id}/judge", a.wrapWithMutationLock(true, false, a.startJudge))
	m.HandleFunc("GET /api/judge-runs/{id}", a.wrap(true, a.getJudgeRun))
}

func safeZipSegment(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsControl(r) || strings.ContainsRune(`/\\:*?"<>|`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	clean := strings.Trim(strings.TrimSpace(b.String()), ".")
	if clean == "" || clean == ".." {
		return "未命名"
	}
	if len([]rune(clean)) > 100 {
		clean = string([]rune(clean)[:100])
	}
	return clean
}

func uniqueZipEntry(entries map[string]bool, candidate string) string {
	if !entries[candidate] {
		entries[candidate] = true
		return candidate
	}
	extension := path.Ext(candidate)
	stem := strings.TrimSuffix(candidate, extension)
	for suffix := 2; ; suffix++ {
		name := fmt.Sprintf("%s (%d)%s", stem, suffix, extension)
		if !entries[name] {
			entries[name] = true
			return name
		}
	}
}

func (a *App) exportTaskSubmissions(w http.ResponseWriter, r *http.Request, u User) error {
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = requirePermission(u, task.Direction, "tasks"); err != nil {
		return err
	}
	normalizeTask(&task)
	if task.Category != "assignment" || len(task.Questions) == 0 {
		return fail(400, "仅支持导出多题作业的提交文件")
	}
	submissions, err := a.store.submissions(task.ID)
	if err != nil {
		return err
	}
	latest := []Submission{}
	seenUsers := map[string]bool{}
	for _, submission := range submissions {
		if seenUsers[submission.UserID] {
			continue
		}
		if submission.FinalizedAt == "" || submission.Status == "draft" {
			continue
		}
		seenUsers[submission.UserID] = true
		latest = append(latest, submission)
	}
	if len(latest) == 0 {
		return fail(409, "目前还没有已提交的作业")
	}
	tmp, err := os.CreateTemp(a.uploadRoot(), "task-submissions-*.zip")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	archive := zip.NewWriter(tmp)
	entries := map[string]bool{}
	fileCount := 0
	questionIndexes := map[string]int{}
	questionTitles := map[string]string{}
	for i, question := range task.Questions {
		questionIndexes[question.ID] = i + 1
		questionTitles[question.ID] = question.Title
	}
	for _, submission := range latest {
		user, userErr := a.store.user(submission.UserID)
		if userErr != nil {
			archive.Close()
			tmp.Close()
			return userErr
		}
		studentFolder := safeZipSegment(submission.UserName) + "_" + safeZipSegment(task.Title) + "_" + safeZipSegment(user.ID)
		for _, answer := range submission.Answers {
			questionIndex, exists := questionIndexes[answer.QuestionID]
			if !exists {
				continue
			}
			questionFolder := fmt.Sprintf("%02d_%s", questionIndex, safeZipSegment(questionTitles[answer.QuestionID]))
			for _, fileID := range answer.FileIDs {
				asset, assetErr := a.fileAsset(fileID)
				if assetErr != nil {
					archive.Close()
					tmp.Close()
					return assetErr
				}
				entryName := uniqueZipEntry(entries, path.Join(studentFolder, questionFolder, safeZipSegment(asset.Name)))
				entry, createErr := archive.Create(entryName)
				if createErr != nil {
					archive.Close()
					tmp.Close()
					return createErr
				}
				allowed, accessErr := a.canDownloadFile(u, fileID)
				if accessErr != nil || !allowed {
					archive.Close()
					tmp.Close()
					if accessErr != nil {
						return accessErr
					}
					return fail(403, "作业中包含无权导出的私人凭证")
				}
				file, _, openErr := a.openAsset(fileID)
				if openErr != nil {
					archive.Close()
					tmp.Close()
					return openErr
				}
				_, copyErr := io.Copy(entry, file)
				closeErr := file.Close()
				if copyErr != nil {
					archive.Close()
					tmp.Close()
					return copyErr
				}
				if closeErr != nil {
					archive.Close()
					tmp.Close()
					return closeErr
				}
				fileCount++
			}
			if answer.JudgeRunID != "" {
				run, runErr := readOne[JudgeRun](a.store, "SELECT payload FROM judge_runs WHERE id=?", answer.JudgeRunID)
				if runErr != nil {
					archive.Close()
					tmp.Close()
					return runErr
				}
				if run.TaskID != task.ID || run.UserID != submission.UserID || run.QuestionID != answer.QuestionID {
					archive.Close()
					tmp.Close()
					return fail(409, "代码判题记录与提交题目不一致")
				}
				asset, assetErr := a.fileAsset(run.SourceFileID)
				if assetErr != nil {
					archive.Close()
					tmp.Close()
					return assetErr
				}
				entryName := uniqueZipEntry(entries, path.Join(studentFolder, questionFolder, safeZipSegment(asset.Name)))
				entry, createErr := archive.Create(entryName)
				if createErr != nil {
					archive.Close()
					tmp.Close()
					return createErr
				}
				allowed, accessErr := a.canDownloadFile(u, run.SourceFileID)
				if accessErr != nil || !allowed {
					archive.Close()
					tmp.Close()
					if accessErr != nil {
						return accessErr
					}
					return fail(403, "作业中包含无权导出的私人凭证")
				}
				source, _, openErr := a.openAsset(run.SourceFileID)
				if openErr != nil {
					archive.Close()
					tmp.Close()
					return openErr
				}
				_, copyErr := io.Copy(entry, source)
				closeErr := source.Close()
				if copyErr != nil {
					archive.Close()
					tmp.Close()
					return copyErr
				}
				if closeErr != nil {
					archive.Close()
					tmp.Close()
					return closeErr
				}
				fileCount++
			}
		}
	}
	if fileCount == 0 {
		archive.Close()
		tmp.Close()
		return fail(409, "已提交的作业中没有可导出的答案文件")
	}
	if err = archive.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	file, err := os.Open(tmpName)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeZipSegment(task.Title) + "_作业提交.zip"}))
	w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	http.ServeContent(w, r, "submissions.zip", info.ModTime(), file)
	return nil
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
		if ((!retry && recipient.Status == "pending") || (retry && recipient.Status == "failed")) && !a.interviewDeliveryValid(recipient) {
			return fail(409, "面试安排或结果已改变，请重新生成面试通知")
		}
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
			allowed, err = a.mailRecipientAllowed(actor, *recipient)
		}
		if err != nil || !allowed {
			recipient.Status = "failed"
			recipient.Error = "收件人授权已失效"
			_ = a.saveMailCampaign(item)
			continue
		}
		if !a.interviewDeliveryValid(*recipient) {
			recipient.Status = "failed"
			recipient.Error = "面试安排已改变，请重新生成通知"
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
		body := item.Body
		if recipient.Body != "" {
			body = recipient.Body
		}
		err = mailer.SendMessage(ctx, recipient.Email, item.Subject, body)
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
