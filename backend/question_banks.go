package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Question sources stay inside the server binary. Catalog responses never
// expose answers, explanations, or hidden programming cases.
//
//go:embed question_banks/*.json
var questionBankFiles embed.FS

type bankQuestion struct {
	ID          string       `json:"id"`
	Direction   string       `json:"direction"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Tags        []string     `json:"tags"`
	Difficulty  string       `json:"difficulty"`
	Type        string       `json:"type"`
	Options     []string     `json:"options"`
	Answers     []string     `json:"answers"`
	Explanation string       `json:"explanation"`
	SourceURLs  []string     `json:"sourceUrls"`
	ImageURL    string       `json:"imageUrl"`
	ImageAlt    string       `json:"imageAlt"`
	Judge       *JudgeConfig `json:"judge,omitempty"`
}

type questionBank struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Direction string         `json:"direction"`
	Count     int            `json:"count"`
	Topics    []string       `json:"topics"`
	Questions []bankQuestion `json:"-"`
}

func loadQuestionBanks() ([]questionBank, error) {
	banks := []questionBank{
		{ID: "algorithm-basics-v1", Name: "C++ / Python 基础与算法", Direction: "视觉算法", Topics: []string{"C++", "Python", "数据结构", "算法编程"}},
		{ID: "mcu-basics-v1", Name: "C语言与STM32学习题库", Direction: "电控", Topics: []string{"C语言基础", "C语言进阶", "STM32单片机基础", "单片机通用基础"}},
		{ID: "hardware-basics-v1", Name: "元件识别与电路基础", Direction: "硬件", Topics: []string{"元件识别", "模拟电路", "数字电路", "电源基础"}},
	}
	entries, err := questionBankFiles.ReadDir("question_banks")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := questionBankFiles.ReadFile("question_banks/" + entry.Name())
		if err != nil {
			return nil, err
		}
		var questions []bankQuestion
		if err = json.Unmarshal(data, &questions); err != nil {
			return nil, err
		}
		for _, q := range questions {
			if q.ID == "" || seen[q.ID] {
				return nil, fmt.Errorf("duplicate or missing built-in question key")
			}
			seen[q.ID] = true
			if len(q.SourceURLs) == 0 {
				return nil, fmt.Errorf("question %s has no source", q.ID)
			}
			for _, source := range q.SourceURLs {
				if !validURL(source) || !strings.HasPrefix(source, "https://") {
					return nil, fmt.Errorf("invalid question source")
				}
			}
			found := false
			for i := range banks {
				if banks[i].Direction == q.Direction {
					banks[i].Questions = append(banks[i].Questions, q)
					banks[i].Count++
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("invalid built-in question direction")
			}
		}
	}
	return banks, nil
}

func (q bankQuestion) task(u User) Task {
	t := Task{ID: tokenHash("mr-system-question-bank/" + q.ID), Title: q.Title, Description: q.Description,
		Direction: q.Direction, Category: "problem", Kind: "document", Difficulty: q.Difficulty, MaxScore: 100,
		Tags: append([]string{}, q.Tags...), FileIDs: []string{}, ResourcesURL: q.SourceURLs[0],
		IllustrationURL: q.ImageURL, IllustrationAlt: q.ImageAlt, CreatedBy: u.ID, CreatedAt: now()}
	if q.Type == "code" {
		t.Kind, t.SubmissionType, t.Judge = "code", "code", q.Judge
	} else {
		t.MechanicalProblem = &MechanicalProblem{Type: q.Type, Options: append([]string{}, q.Options...), Answers: append([]string{}, q.Answers...), Explanation: q.Explanation, AnswerFileIDs: []string{}}
	}
	return t
}

func (a *App) registerQuestionBankRoutes(m *http.ServeMux) {
	a.registerBankSyncRoutes(m)
	m.HandleFunc("GET /api/question-banks", a.wrap(true, a.listQuestionBanks))
	m.HandleFunc("POST /api/question-banks/{id}/import", a.wrap(true, a.importQuestionBank))
}

func (a *App) listQuestionBanks(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "tasks") {
		return fail(403, "没有题库管理权限")
	}
	banks, err := loadQuestionBanks()
	if err != nil {
		return err
	}
	items := []questionBank{}
	for _, bank := range banks {
		if hasPermission(u, bank.Direction, "tasks") {
			items = append(items, bank)
		}
	}
	respond(w, 200, map[string]any{"items": items})
	return nil
}

func (a *App) importQuestionBank(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "tasks") {
		return fail(403, "没有题库管理权限")
	}
	var in struct {
		SyncCourseTags bool `json:"syncCourseTags"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			return err
		}
	}
	banks, err := loadQuestionBanks()
	if err != nil {
		return err
	}
	var selected *questionBank
	for i := range banks {
		if banks[i].ID == r.PathValue("id") {
			selected = &banks[i]
			break
		}
	}
	if selected == nil {
		return fail(404, "题库不存在")
	}
	if err = requirePermission(u, selected.Direction, "tasks"); err != nil {
		return err
	}
	// Validate the whole batch before opening a transaction. The standard write
	// wrapper serializes imports with task edits/deletes in this single instance.
	tasks := make([]Task, 0, len(selected.Questions))
	for _, q := range selected.Questions {
		t := q.task(u)
		if err = a.validateTaskContent(&t, u); err != nil {
			return err
		}
		tasks = append(tasks, t)
	}
	tx, err := a.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	imported, skipped, grouped := 0, 0, 0
	for _, t := range tasks {
		var count int
		if err = s.row("SELECT COUNT(*) FROM tasks WHERE id=?", t.ID).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			// Opt-in course metadata only; preserve authored content and results.
			if in.SyncCourseTags {
				existing, readErr := s.task(t.ID)
				if readErr != nil {
					return readErr
				}
				if addCourseTag(&existing, t) {
					if err = s.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(existing), existing.ID); err != nil {
						return err
					}
					grouped++
				}
			}
			skipped++
			continue
		}
		if err = s.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", t.ID, encoded(t)); err != nil {
			return err
		}
		imported++
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if imported > 0 {
		a.activity(u, "导入基础题库", fmt.Sprintf("%s：%d 道", selected.Name, imported))
	}
	if grouped > 0 {
		a.activity(u, "补充题库课程标签", fmt.Sprintf("%s：%d 道", selected.Name, grouped))
	}
	respond(w, 200, map[string]any{"imported": imported, "skipped": skipped, "grouped": grouped, "total": len(tasks)})
	return nil
}

func addCourseTag(existing *Task, source Task) bool {
	if existing.Direction != "电控" || existing.Direction != source.Direction || existing.Category != "problem" || len(existing.Tags) >= 10 {
		return false
	}
	courses := []string{"C语言基础", "C语言进阶", "STM32单片机基础", "单片机通用基础"}
	for _, tag := range existing.Tags {
		if includesID(courses, tag) {
			return false
		} // Keep administrator classification.
	}
	for _, tag := range source.Tags {
		if includesID(courses, tag) {
			existing.Tags = append([]string{tag}, existing.Tags...)
			return true
		}
	}
	return false
}
