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
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	Direction  string            `json:"direction"`
	Status     string            `json:"status"`
	Published  bool              `json:"published"`
	CreatedBy  string            `json:"createdBy"`
	AuthorName string            `json:"authorName"`
	CreatedAt  string            `json:"createdAt"`
	UpdatedAt  string            `json:"updatedAt"`
	Replies    []DiscussionReply `json:"replies"`
}

func normalizeTask(t *Task) {
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
}
func taskLocked(t Task, u User) bool {
	if canManage(u) || t.StartsAt == "" {
		return false
	}
	start, err := time.Parse(time.RFC3339, t.StartsAt)
	// Explicitly scheduled tasks, including legacy quizzes, are released only at start time.
	return err != nil || time.Now().Before(start)
}
func (a *App) registerLearningRoutes(m *http.ServeMux) {
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
}
func (a *App) listKnowledge(w http.ResponseWriter, r *http.Request, _ User) error {
	items, err := readAll[KnowledgeResource](a.store, "SELECT payload FROM knowledge_resources")
	if err != nil {
		return err
	}
	result := []KnowledgeResource{}
	for _, item := range items {
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
func (a *App) validateKnowledge(item *KnowledgeResource, u User) error {
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
	if err := requireOwnedFiles(a.store, u, item.FileIDs); err != nil {
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
	if !canManage(u) {
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
	if !canManage(u) {
		return fail(403, "只有管理员或导师可维护知识库")
	}
	item, err := readOne[KnowledgeResource](a.store, "SELECT payload FROM knowledge_resources WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if u.Role != "admin" && item.CreatedBy != u.ID {
		return fail(403, "只能编辑自己发布的资料")
	}
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
	if err = a.validateKnowledge(&item, u); err != nil {
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
	if !canManage(u) {
		return fail(403, "只有管理员或导师可维护知识库")
	}
	item, err := readOne[KnowledgeResource](a.store, "SELECT payload FROM knowledge_resources WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if u.Role != "admin" && item.CreatedBy != u.ID {
		return fail(403, "只能删除自己发布的资料")
	}
	if err = a.store.exec("DELETE FROM knowledge_resources WHERE id=?", item.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"deleted": true})
	return nil
}
func (a *App) knowledgeFileAccess(fileID string) (bool, error) {
	items, err := readAll[KnowledgeResource](a.store, "SELECT payload FROM knowledge_resources")
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if includesID(item.FileIDs, fileID) {
			return true, nil
		}
	}
	return false, nil
}
func discussionVisible(d Discussion, u User) bool {
	return d.Published || d.CreatedBy == u.ID || canManage(u)
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
		Title     string `json:"title"`
		Content   string `json:"content"`
		Direction string `json:"direction"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Title, in.Content = strings.TrimSpace(in.Title), strings.TrimSpace(in.Content)
	if in.Title == "" || len(in.Title) > 250 || in.Content == "" || len(in.Content) > 30000 || in.Direction != "" && !validDirection(in.Direction) {
		return fail(400, "请填写有效的问题标题、内容和技术组")
	}
	item := Discussion{ID: randomID(), Title: in.Title, Content: in.Content, Direction: in.Direction, Status: "open", CreatedBy: u.ID, AuthorName: u.Name, CreatedAt: now(), Replies: []DiscussionReply{}}
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
	if item.CreatedBy != u.ID && !canManage(u) {
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
	if !canManage(u) {
		return fail(403, "只有管理员或导师可处理问题")
	}
	item, err := readOne[Discussion](a.store, "SELECT payload FROM discussions WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	var in struct {
		Status    *string `json:"status"`
		Published *bool   `json:"published"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if in.Status != nil {
		if *in.Status != "open" && *in.Status != "resolved" {
			return fail(400, "无效的问题状态")
		}
		if *in.Status == "resolved" && len(item.Replies) == 0 {
			return fail(409, "请先答复问题，再标记为已解决")
		}
		if item.Published && *in.Status != "resolved" && u.Role != "admin" {
			return fail(403, "请由管理员取消公开后再重新处理问题")
		}
		item.Status = *in.Status
		if item.Status != "resolved" {
			item.Published = false
		}
	}
	if in.Published != nil {
		if u.Role != "admin" {
			return fail(403, "只有管理员可公开精选问答")
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
	if !canManage(u) {
		return fail(403, "只有管理员或导师可修改任务")
	}
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	if u.Role != "admin" && task.CreatedBy != u.ID {
		return fail(403, "只能编辑自己发布的任务")
	}
	var in struct {
		Title          *string         `json:"title"`
		Description    *string         `json:"description"`
		Direction      *string         `json:"direction"`
		Team           *string         `json:"team"`
		TeamID         *string         `json:"teamId"`
		Kind           *string         `json:"kind"`
		Difficulty     *string         `json:"difficulty"`
		DueAt          *string         `json:"dueAt"`
		StartsAt       *string         `json:"startsAt"`
		ResourcesURL   *string         `json:"resourcesUrl"`
		MaxScore       *float64        `json:"maxScore"`
		Required       *bool           `json:"required"`
		FileIDs        *[]string       `json:"fileIds"`
		SubmissionType *string         `json:"submissionType"`
		Category       *string         `json:"category"`
		Tags           *[]string       `json:"tags"`
		Judge          json.RawMessage `json:"judge"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	var usage int
	if err = a.store.row("SELECT (SELECT COUNT(*) FROM submissions WHERE task_id=?) + (SELECT COUNT(*) FROM judge_runs WHERE task_id=?)", task.ID, task.ID).Scan(&usage); err != nil {
		return err
	}
	if usage > 0 {
		return fail(409, "此任务已有提交或判题记录，请发布新版本，保留原题与成绩的一致性")
	}
	if in.Title != nil {
		task.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		task.Description = strings.TrimSpace(*in.Description)
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
	if task.Title == "" || len(task.Title) > 250 || task.Description == "" || len(task.Description) > 30000 || task.Direction != "" && !validDirection(task.Direction) || task.Team != "" && !validTeam(task.Team) || !validDate(task.DueAt) || !validURL(task.ResourcesURL) || !finite(task.MaxScore) || task.MaxScore <= 0 || task.MaxScore > 1000 {
		return fail(400, "任务内容无效")
	}
	if task.Kind != "code" && task.Kind != "document" && task.Kind != "hardware" && task.Kind != "quiz" {
		return fail(400, "无效的任务类型")
	}
	if task.Difficulty != "入门" && task.Difficulty != "进阶" && task.Difficulty != "挑战" {
		return fail(400, "无效的任务难度")
	}
	if err = a.validateTaskPlatform(&task, u); err != nil {
		return err
	}
	if err = a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(task), task.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": task})
	return nil
}
func (a *App) deleteTask(w http.ResponseWriter, r *http.Request, u User) error {
	if !canManage(u) {
		return fail(403, "只有管理员或导师可删除任务")
	}
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	if u.Role != "admin" && task.CreatedBy != u.ID {
		return fail(403, "只能删除自己发布的任务")
	}
	var count int
	if err = a.store.row("SELECT (SELECT COUNT(*) FROM submissions WHERE task_id=?) + (SELECT COUNT(*) FROM judge_runs WHERE task_id=?)", task.ID, task.ID).Scan(&count); err != nil {
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
