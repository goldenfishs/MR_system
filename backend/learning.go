package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

type KnowledgeResource struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Direction   string   `json:"direction"`
	Kind        string   `json:"kind"`
	FileIDs     []string `json:"fileIds"`
	URL         string   `json:"url"`
	CreatedBy   string   `json:"createdBy"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}
type DiscussionReply struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	UserID    string `json:"userId"`
	UserName  string `json:"userName"`
	CreatedAt string `json:"createdAt"`
}
type Discussion struct {
	CategoryID string            `json:"categoryId"`
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	Direction  string            `json:"direction"`
	FileIDs    []string          `json:"fileIds"`
	Status     string            `json:"status"`
	Published  bool              `json:"published"`
	CreatedBy  string            `json:"createdBy"`
	AuthorName string            `json:"authorName"`
	CreatedAt  string            `json:"createdAt"`
	UpdatedAt  string            `json:"updatedAt"`
	Replies    []DiscussionReply `json:"replies"`
}

func normalizeTask(t *Task) {
	if t.BankVisibility == "" {
		t.BankVisibility = "public"
	}
	if t.Status == "" {
		// Existing stored tasks predate publication states and remain published.
		t.Status = "published"
	}
	if t.Category == "" {
		t.Category = "assignment"
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	if t.FileIDs == nil {
		t.FileIDs = []string{}
	}
	t.Tags = normalizedIDs(t.Tags)
	for i := range t.Questions {
		if t.Questions[i].Type == "" {
			// Older multi-question assignments were file-based drawing exercises.
			t.Questions[i].Type = "drawing"
		}
		if t.Questions[i].Options == nil {
			t.Questions[i].Options = []string{}
		}
		if t.Questions[i].FileIDs == nil {
			t.Questions[i].FileIDs = []string{}
		}
		t.Questions[i].FileIDs = normalizedIDs(t.Questions[i].FileIDs)
	}
}
func taskLocked(t Task, u User) bool {
	normalizeTask(&t)
	if hasPermission(u, t.Direction, "tasks") {
		return false
	}
	if t.BankVisibility == "internal" {
		return true
	}
	if t.Status != "published" {
		return true
	}
	if t.StartsAt == "" {
		return false
	}
	start, err := time.Parse(time.RFC3339, t.StartsAt)
	// Explicitly scheduled tasks, including legacy quizzes, are released only at start time.
	return err != nil || time.Now().Before(start)
}
func (a *App) registerLearningRoutes(m *http.ServeMux) {
	a.registerDiscussionCategoryRoutes(m)
	m.HandleFunc("GET /api/learning-access", a.wrap(true, a.getLearningAccess))
	m.HandleFunc("GET /api/knowledge-base", a.wrap(true, a.listKnowledge))
	m.HandleFunc("POST /api/knowledge-base", a.wrap(true, a.createKnowledge))
	m.HandleFunc("PATCH /api/knowledge-base/{id}", a.wrap(true, a.updateKnowledge))
	m.HandleFunc("DELETE /api/knowledge-base/{id}", a.wrap(true, a.deleteKnowledge))
	m.HandleFunc("GET /api/discussions", a.wrap(true, a.listDiscussions))
	m.HandleFunc("POST /api/discussions", a.wrap(true, a.createDiscussion))
	m.HandleFunc("POST /api/discussions/{id}/replies", a.wrap(true, a.replyDiscussion))
	m.HandleFunc("PATCH /api/discussions/{id}", a.wrap(true, a.updateDiscussion))
	m.HandleFunc("PATCH /api/tasks/{id}", a.wrap(true, a.updateTask))
	m.HandleFunc("DELETE /api/tasks/{id}", a.wrap(true, a.deleteTask))
	m.HandleFunc("POST /api/tasks/{id}/publish", a.wrap(true, a.publishTask))
}

// Resolve access from current enrollment records. Profile direction and cached
// login data are not proof of admission, and revocation takes effect immediately.
func (a *App) learningAllowed(u User) (bool, error) {
	if u.Role == "admin" || u.MemberType == "formal" || u.MemberType == "reserve" {
		return true, nil
	}
	dirs, err := a.memberDirections(u.ID)
	return len(dirs) > 0, err
}

func (a *App) requireLearningAccess(u User) error {
	allowed, err := a.learningAllowed(u)
	if err != nil {
		return err
	}
	if !allowed {
		return fail(http.StatusForbidden, "报名审核通过后才可访问技术分组")
	}
	return nil
}

func (a *App) getLearningAccess(w http.ResponseWriter, _ *http.Request, u User) error {
	allowed, err := a.learningAllowed(u)
	if err != nil {
		return err
	}
	directions := []string{}
	if allowed {
		directions = append(directions, technicalDirections...)
	}
	enrolled, err := a.memberDirections(u.ID)
	if err != nil {
		return err
	}
	applied, err := appliedDirectionsAt(a.store, u, enrolled)
	if err != nil {
		return err
	}
	respond(w, http.StatusOK, map[string]any{"allowed": allowed, "directions": directions, "appliedDirections": applied, "enrolledDirections": enrolled})
	return nil
}

func (a *App) listKnowledge(w http.ResponseWriter, r *http.Request, u User) error {
	allowed, err := a.learningAllowed(u)
	if err != nil {
		return err
	}
	if !allowed && !hasAnyPermission(u, "knowledge") {
		return fail(403, "报名审核通过后才可访问技术分组")
	}
	items, err := readAll[KnowledgeResource](a.store, "SELECT payload FROM knowledge_resources")
	if err != nil {
		return err
	}
	result := []KnowledgeResource{}
	for _, item := range items {
		if !allowed && !hasPermission(u, item.Direction, "knowledge") {
			continue
		}
		if direction := r.URL.Query().Get("direction"); direction != "" && item.Direction != "" && item.Direction != direction {
			continue
		}
		if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search"))); search != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.Description), search) {
			continue
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	respond(w, 200, map[string]any{"resources": result})
	return nil
}
func (a *App) validateKnowledge(item *KnowledgeResource, u User, existing ...[]string) error {
	if err := requirePermission(u, item.Direction, "knowledge"); err != nil {
		return err
	}
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	item.URL = strings.TrimSpace(item.URL)
	if item.Title == "" || len(item.Title) > 250 || len(item.Description) > 30000 || item.Direction != "" && !validDirection(item.Direction) || !validURL(item.URL) {
		return fail(400, "请填写有效的资料标题、内容、技术组和链接")
	}
	if item.Kind != "file" && item.Kind != "pdf" && item.Kind != "video" {
		return fail(400, "请选择文件、PDF 或视频资料")
	}
	if len(item.FileIDs) > 20 || len(item.FileIDs) == 0 && item.URL == "" {
		return fail(400, "请至少上传一个资料文件或填写资料链接，最多 20 个附件")
	}
	item.FileIDs = normalizedIDs(item.FileIDs)
	previous := []string{}
	if len(existing) > 0 {
		previous = existing[0]
	}
	if err := requireUpdatedFiles(a.store, u, item.FileIDs, previous); err != nil {
		return err
	}
	if item.Kind == "pdf" {
		for _, id := range item.FileIDs {
			f, err := a.fileAsset(id)
			if err != nil {
				return err
			}
			if f.MIME != "application/pdf" {
				return fail(400, "PDF 资料只能附带 PDF 文件")
			}
		}
	}
	return nil
}
func (a *App) createKnowledge(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "knowledge") {
		return fail(403, "只有管理员或导师可发布知识库资料")
	}
	var in KnowledgeResource
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := a.validateKnowledge(&in, u); err != nil {
		return err
	}
	in.ID, in.CreatedBy, in.CreatedAt = randomID(), u.ID, now()
	in.UpdatedAt = in.CreatedAt
	if err := a.store.exec("INSERT INTO knowledge_resources(id,payload) VALUES(?,?)", in.ID, encoded(in)); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"resource": in})
	return nil
}
func (a *App) updateKnowledge(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "knowledge") {
		return fail(403, "只有管理员或导师可维护知识库")
	}
	item, err := readOne[KnowledgeResource](a.store, "SELECT payload FROM knowledge_resources WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = requirePermission(u, item.Direction, "knowledge"); err != nil {
		return err
	}
	previousFiles := append([]string{}, item.FileIDs...)
	var in struct {
		Title       *string   `json:"title"`
		Description *string   `json:"description"`
		Direction   *string   `json:"direction"`
		Kind        *string   `json:"kind"`
		FileIDs     *[]string `json:"fileIds"`
		URL         *string   `json:"url"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if in.Title != nil {
		item.Title = *in.Title
	}
	if in.Description != nil {
		item.Description = *in.Description
	}
	if in.Direction != nil {
		item.Direction = *in.Direction
	}
	if in.Kind != nil {
		item.Kind = *in.Kind
	}
	if in.FileIDs != nil {
		item.FileIDs = *in.FileIDs
	}
	if in.URL != nil {
		item.URL = *in.URL
	}
	if err = a.validateKnowledge(&item, u, previousFiles); err != nil {
		return err
	}
	item.UpdatedAt = now()
	if err = a.store.exec("UPDATE knowledge_resources SET payload=? WHERE id=?", encoded(item), item.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"resource": item})
	return nil
}
func (a *App) deleteKnowledge(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "knowledge") {
		return fail(403, "只有管理员或导师可维护知识库")
	}
	item, err := readOne[KnowledgeResource](a.store, "SELECT payload FROM knowledge_resources WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = requirePermission(u, item.Direction, "knowledge"); err != nil {
		return err
	}
	if err = a.store.exec("DELETE FROM knowledge_resources WHERE id=?", item.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"deleted": true})
	return nil
}
func (a *App) knowledgeFileAccess(u User, fileID string) (bool, error) {
	allowed, err := a.learningAllowed(u)
	if err != nil {
		return false, err
	}
	items, err := readAll[KnowledgeResource](a.store, "SELECT payload FROM knowledge_resources")
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if includesID(item.FileIDs, fileID) && (allowed || hasPermission(u, item.Direction, "knowledge")) {
			return true, nil
		}
	}
	return false, nil
}
func discussionVisible(d Discussion, u User) bool {
	return d.Published || d.CreatedBy == u.ID || hasPermission(u, d.Direction, "discussions")
}
func (a *App) listDiscussions(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := readAll[Discussion](a.store, "SELECT payload FROM discussions")
	if err != nil {
		return err
	}
	result := []Discussion{}
	for _, item := range items {
		if !discussionVisible(item, u) {
			continue
		}
		if direction := r.URL.Query().Get("direction"); direction != "" && item.Direction != direction {
			continue
		}
		if category := r.URL.Query().Get("categoryId"); category != "" && (category == "__uncategorized" && item.CategoryID != "" || category != "__uncategorized" && item.CategoryID != category) {
			continue
		}
		if status := r.URL.Query().Get("status"); status != "" && item.Status != status {
			continue
		}
		if r.URL.Query().Get("published") == "true" && !item.Published {
			continue
		}
		if search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search"))); search != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.Content), search) {
			continue
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt > result[j].UpdatedAt })
	respond(w, 200, map[string]any{"discussions": result})
	return nil
}
func (a *App) createDiscussion(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		CategoryID string   `json:"categoryId"`
		Title      string   `json:"title"`
		Content    string   `json:"content"`
		Direction  string   `json:"direction"`
		FileIDs    []string `json:"fileIds"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Title, in.Content = strings.TrimSpace(in.Title), strings.TrimSpace(in.Content)
	if in.Title == "" || len(in.Title) > 250 || in.Content == "" || len(in.Content) > 30000 || in.Direction != "" && !validDirection(in.Direction) {
		return fail(400, "请填写有效的问题标题、内容和技术组")
	}
	in.CategoryID = strings.TrimSpace(in.CategoryID)
	if err := a.validateDiscussionCategoryID(in.CategoryID, in.Direction); err != nil {
		return err
	}
	if err := requireOwnedFiles(a.store, u, in.FileIDs); err != nil {
		return err
	}
	item := Discussion{CategoryID: in.CategoryID, ID: randomID(), Title: in.Title, Content: in.Content, Direction: in.Direction, FileIDs: normalizedIDs(in.FileIDs), Status: "open", CreatedBy: u.ID, AuthorName: u.Name, CreatedAt: now(), Replies: []DiscussionReply{}}
	item.UpdatedAt = item.CreatedAt
	if err := a.store.exec("INSERT INTO discussions(id,created_by,payload) VALUES(?,?,?)", item.ID, u.ID, encoded(item)); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"discussion": item})
	return nil
}
func (a *App) replyDiscussion(w http.ResponseWriter, r *http.Request, u User) error {
	item, err := readOne[Discussion](a.store, "SELECT payload FROM discussions WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if !discussionVisible(item, u) {
		return fail(404, "问题不存在")
	}
	// Curated public answers stay read-only to peers; authors and mentors can clarify.
	if item.CreatedBy != u.ID && !hasPermission(u, item.Direction, "discussions") {
		return fail(403, "仅提问者、导师和管理员可补充答复")
	}
	if item.Published {
		return fail(409, "公开问答需由管理员取消公开后再补充，避免未经审核的内容直接公开")
	}
	var in struct {
		Content string `json:"content"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" || len(in.Content) > 30000 {
		return fail(400, "请填写有效的答复内容")
	}
	if len(item.Replies) >= 200 {
		return fail(409, "此问题的答复数量已达上限，请新建相关问题")
	}
	item.Replies = append(item.Replies, DiscussionReply{ID: randomID(), Content: in.Content, UserID: u.ID, UserName: u.Name, CreatedAt: now()})
	item.UpdatedAt = now()
	if err = a.store.exec("UPDATE discussions SET payload=? WHERE id=?", encoded(item), item.ID); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"discussion": item})
	return nil
}
func (a *App) updateDiscussion(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "discussions") {
		return fail(403, "没有讨论管理权限")
	}
	item, err := readOne[Discussion](a.store, "SELECT payload FROM discussions WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = requirePermission(u, item.Direction, "discussions"); err != nil {
		return err
	}
	var in struct {
		CategoryID *string `json:"categoryId"`
		Status     *string `json:"status"`
		Published  *bool   `json:"published"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if in.CategoryID != nil {
		id := strings.TrimSpace(*in.CategoryID)
		if id != item.CategoryID {
			if err = a.validateDiscussionCategoryID(id, item.Direction); err != nil {
				return err
			}
			item.CategoryID = id
		}
	}
	if in.Status != nil {
		if *in.Status != "open" && *in.Status != "resolved" {
			return fail(400, "无效的问题状态")
		}
		if *in.Status == "resolved" && len(item.Replies) == 0 {
			return fail(409, "请先答复问题，再标记为已解决")
		}
		if item.Published && *in.Status != "resolved" && !hasPermission(u, item.Direction, "discussions") {
			return fail(403, "请由管理员取消公开后再重新处理问题")
		}
		item.Status = *in.Status
		if item.Status != "resolved" {
			item.Published = false
		}
	}
	if in.Published != nil {
		if !hasPermission(u, item.Direction, "discussions") {
			return fail(403, "没有公开精选问答权限")
		}
		if *in.Published && item.Status != "resolved" {
			return fail(409, "只能公开已解答的问题")
		}
		item.Published = *in.Published
	}
	item.UpdatedAt = now()
	if err = a.store.exec("UPDATE discussions SET payload=? WHERE id=?", encoded(item), item.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"discussion": item})
	return nil
}

func (a *App) updateTask(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "tasks") {
		return fail(403, "只有管理员或导师可修改任务")
	}
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = requirePermission(u, task.Direction, "tasks"); err != nil {
		return err
	}
	previousFiles := taskLearningFileIDs(task)
	var in struct {
		BankVisibility    *string         `json:"bankVisibility"`
		Title             *string         `json:"title"`
		Description       *string         `json:"description"`
		IllustrationURL   *string         `json:"illustrationUrl"`
		IllustrationAlt   *string         `json:"illustrationAlt"`
		Direction         *string         `json:"direction"`
		Team              *string         `json:"team"`
		TeamID            *string         `json:"teamId"`
		Kind              *string         `json:"kind"`
		Difficulty        *string         `json:"difficulty"`
		DueAt             *string         `json:"dueAt"`
		StartsAt          *string         `json:"startsAt"`
		ResourcesURL      *string         `json:"resourcesUrl"`
		MaxScore          *float64        `json:"maxScore"`
		Required          *bool           `json:"required"`
		FileIDs           *[]string       `json:"fileIds"`
		SubmissionType    *string         `json:"submissionType"`
		Category          *string         `json:"category"`
		Tags              *[]string       `json:"tags"`
		Judge             json.RawMessage `json:"judge"`
		MechanicalProblem json.RawMessage `json:"mechanicalProblem"`
		Questions         *[]TaskQuestion `json:"questions"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	var usage int
	if err = a.taskStudentUsage(task.ID, &usage); err != nil {
		return err
	}
	if usage > 0 {
		return fail(409, "此任务已有提交或判题记录，请发布新版本，保留原题与成绩的一致性")
	}
	if in.BankVisibility != nil {
		task.BankVisibility = *in.BankVisibility
	}
	if in.Title != nil {
		task.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		task.Description = strings.TrimSpace(*in.Description)
	}
	if in.IllustrationURL != nil {
		task.IllustrationURL = *in.IllustrationURL
	}
	if in.IllustrationAlt != nil {
		task.IllustrationAlt = *in.IllustrationAlt
	}
	if in.Direction != nil {
		task.Direction = *in.Direction
	}
	if in.Team != nil {
		task.Team = *in.Team
	}
	if in.TeamID != nil {
		task.TeamID = *in.TeamID
	}
	if in.Kind != nil {
		task.Kind = *in.Kind
	}
	if in.Difficulty != nil {
		task.Difficulty = *in.Difficulty
	}
	if in.DueAt != nil {
		task.DueAt = *in.DueAt
	}
	if in.StartsAt != nil {
		task.StartsAt = *in.StartsAt
	}
	if in.ResourcesURL != nil {
		task.ResourcesURL = *in.ResourcesURL
	}
	if in.MaxScore != nil {
		task.MaxScore = *in.MaxScore
	}
	if in.Required != nil {
		task.Required = *in.Required
	}
	if in.FileIDs != nil {
		task.FileIDs = *in.FileIDs
	}
	if in.SubmissionType != nil {
		task.SubmissionType = *in.SubmissionType
	}
	if in.Category != nil {
		task.Category = *in.Category
	}
	if in.Tags != nil {
		task.Tags = *in.Tags
	}
	if in.Questions != nil {
		task.Questions = *in.Questions
	}
	if in.Judge != nil {
		if strings.TrimSpace(string(in.Judge)) == "null" {
			task.Judge = nil
		} else {
			var judge JudgeConfig
			decoder := json.NewDecoder(strings.NewReader(string(in.Judge)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&judge); err != nil {
				return fail(400, "请求数据格式不正确")
			}
			task.Judge = &judge
		}
	}
	if in.MechanicalProblem != nil {
		if strings.TrimSpace(string(in.MechanicalProblem)) == "null" {
			task.MechanicalProblem = nil
		} else {
			var problem MechanicalProblem
			decoder := json.NewDecoder(strings.NewReader(string(in.MechanicalProblem)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&problem); err != nil {
				return fail(400, "客观题配置格式不正确")
			}
			task.MechanicalProblem = &problem
		}
	}
	if err = a.validateTaskContent(&task, u, previousFiles); err != nil {
		return err
	}
	if err = a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(task), task.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": task})
	return nil
}

func (a *App) publishTask(w http.ResponseWriter, r *http.Request, u User) error {
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = requirePermission(u, task.Direction, "tasks"); err != nil {
		return err
	}
	normalizeTask(&task)
	if task.Status != "draft" || task.Category != "assignment" {
		return fail(409, "只有未发布的作业草稿可以发布")
	}
	if len(task.Questions) == 0 {
		return fail(400, "发布作业前至少添加一道题目")
	}
	previousFiles := taskLearningFileIDs(task)
	task.Status = "published"
	if err = a.validateTaskContent(&task, u, previousFiles); err != nil {
		return err
	}
	task.PublishedAt = now()
	if err = a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(task), task.ID); err != nil {
		return err
	}
	a.activity(u, "发布培养作业", task.Title)
	respond(w, 200, map[string]any{"item": task})
	return nil
}

func (a *App) deleteTask(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "tasks") {
		return fail(403, "只有管理员或导师可删除任务")
	}
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = requirePermission(u, task.Direction, "tasks"); err != nil {
		return err
	}
	var count int
	if err = a.taskStudentUsage(task.ID, &count); err != nil {
		return err
	}
	if count > 0 {
		return fail(409, "已有提交或判题记录的任务不能删除")
	}
	if err = a.store.exec("DELETE FROM tasks WHERE id=?", task.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"deleted": true})
	return nil
}

// Preview runs validate task content without freezing it or creating student work.
func (a *App) taskStudentUsage(taskID string, count *int) error {
	if err := a.store.row("SELECT COUNT(*) FROM submissions WHERE task_id=?", taskID).Scan(count); err != nil {
		return err
	}
	runs, err := readAll[JudgeRun](a.store, "SELECT payload FROM judge_runs WHERE task_id=?", taskID)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if !run.Preview {
			*count++
		}
	}
	return nil
}
