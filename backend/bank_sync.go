package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Sync metadata lives in its own settings row. It is never part of public settings.
const bankSyncStateID = "question-bank-sync-v1"

type bankSyncSettings struct {
	Repository        string   `json:"repository"`
	Branch            string   `json:"branch"`
	Catalogs          []string `json:"catalogs"`
	DefaultVisibility string   `json:"defaultVisibility"`
}
type bankSyncCatalog struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}
type bankSyncRecord struct {
	Key      string   `json:"key"`
	Task     Task     `json:"task"`
	Catalogs []string `json:"catalogs"`
}
type bankSyncMapping struct {
	TaskID      string   `json:"taskId"`
	Fingerprint string   `json:"fingerprint"`
	Catalogs    []string `json:"catalogs"`
}
type bankSyncResult struct {
	Commit    string `json:"commit"`
	At        string `json:"at"`
	Imported  int    `json:"imported"`
	Updated   int    `json:"updated"`
	Unchanged int    `json:"unchanged"`
	Conflicts int    `json:"conflicts"`
}
type bankSyncSnapshot struct {
	Commit   string           `json:"commit"`
	Settings bankSyncSettings `json:"settings"`
	Records  []bankSyncRecord `json:"records"`
}
type bankSyncState struct {
	Settings bankSyncSettings           `json:"settings"`
	Catalogs []bankSyncCatalog          `json:"catalogs"`
	LastSync *bankSyncResult            `json:"lastSync,omitempty"`
	Snapshot *bankSyncSnapshot          `json:"snapshot,omitempty"`
	Mappings map[string]bankSyncMapping `json:"mappings"`
}
type bankSyncItem struct {
	Key       string   `json:"key"`
	Title     string   `json:"title"`
	Direction string   `json:"direction"`
	Catalogs  []string `json:"catalogs"`
	Action    string   `json:"action"`
	Reason    string   `json:"reason,omitempty"`
	target    Task
	source    Task
}

var bankSyncRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9_-][A-Za-z0-9_.-]{0,99}$`)
var bankSyncKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,149}$`)

func bankSyncAdmin(u User) error {
	if u.Role != "admin" {
		return fail(403, "只有系统管理员可以管理题库同步")
	}
	return nil
}
func loadBankSyncState(s *Store) (bankSyncState, error) {
	state, err := readOne[bankSyncState](s, "SELECT payload FROM settings WHERE id=?", bankSyncStateID)
	if errors.Is(err, sql.ErrNoRows) {
		state.Settings = bankSyncSettings{"goldenfishs/MR_question_bank", "main", []string{"c", "cpp", "python", "stm32"}, "internal"}
		state.Catalogs = []bankSyncCatalog{{"c", "C语言题库", 0}, {"cpp", "C++题库", 0}, {"python", "Python题库", 0}, {"stm32", "STM32单片机题库", 0}}
		err = nil
	}
	if state.Mappings == nil {
		state.Mappings = map[string]bankSyncMapping{}
	}
	return state, err
}
func saveBankSyncState(s *Store, state bankSyncState) error {
	return s.exec("INSERT INTO settings(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", bankSyncStateID, encoded(state))
}
func (a *App) registerBankSyncRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/bank-sync", a.wrap(true, a.getBankSync))
	m.HandleFunc("PUT /api/bank-sync/settings", a.wrap(true, a.updateBankSyncSettings))
	m.HandleFunc("POST /api/bank-sync/preview", a.wrapWithMutationLock(true, false, a.previewBankSync))
	m.HandleFunc("POST /api/bank-sync/apply", a.wrap(true, a.applyBankSync))
	m.HandleFunc("POST /api/bank-sync/visibility", a.wrap(true, a.setBankSyncVisibility))
}
func (a *App) getBankSync(w http.ResponseWriter, r *http.Request, u User) error {
	if err := bankSyncAdmin(u); err != nil {
		return err
	}
	state, err := loadBankSyncState(a.store)
	if err != nil {
		return err
	}
	tasks, err := readAll[Task](a.store, "SELECT payload FROM tasks")
	if err != nil {
		return err
	}
	type summary struct {
		ID             string   `json:"id"`
		Title          string   `json:"title"`
		Direction      string   `json:"direction"`
		BankVisibility string   `json:"bankVisibility"`
		Tags           []string `json:"tags"`
		SourceKey      string   `json:"sourceKey,omitempty"`
		Catalogs       []string `json:"catalogs"`
	}
	lookup := map[string]string{}
	for key, m := range state.Mappings {
		if strings.HasPrefix(key, state.Settings.Repository+"/") {
			lookup[m.TaskID] = key
		}
	}
	items := []summary{}
	for _, t := range tasks {
		normalizeTask(&t)
		if t.Category != "problem" {
			continue
		}
		item := summary{ID: t.ID, Title: t.Title, Direction: t.Direction, BankVisibility: t.BankVisibility, Tags: t.Tags, Catalogs: []string{}}
		if key, ok := lookup[t.ID]; ok {
			item.SourceKey = strings.TrimPrefix(key, state.Settings.Repository+"/")
			item.Catalogs = state.Mappings[key].Catalogs
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Title < items[j].Title })
	respond(w, 200, map[string]any{"settings": state.Settings, "catalogs": state.Catalogs, "lastSync": state.LastSync, "questions": items})
	return nil
}
func (a *App) updateBankSyncSettings(w http.ResponseWriter, r *http.Request, u User) error {
	if err := bankSyncAdmin(u); err != nil {
		return err
	}
	var in bankSyncSettings
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Repository = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(in.Repository), "https://github.com/"), ".git")
	in.Branch = strings.TrimSpace(in.Branch)
	if !bankSyncRepoPattern.MatchString(in.Repository) || in.Branch == "" || len(in.Branch) > 100 || strings.ContainsAny(in.Branch, "?#\\ \r\n") || strings.Contains(in.Branch, "..") || len(in.Catalogs) > 50 || len(in.Catalogs) == 0 || (in.DefaultVisibility != "public" && in.DefaultVisibility != "internal") {
		return fail(400, "请填写有效的 GitHub 仓库、分支、题库分类和可见范围")
	}
	in.Catalogs = normalizedIDs(in.Catalogs)
	for _, id := range in.Catalogs {
		if !bankSyncKeyPattern.MatchString(id) {
			return fail(400, "题库分类标识不合法")
		}
	}
	state, err := loadBankSyncState(a.store)
	if err != nil {
		return err
	}
	if state.Settings.Repository != in.Repository {
		state.Catalogs = []bankSyncCatalog{}
	}
	state.Settings = in
	state.Snapshot = nil
	if err = saveBankSyncState(a.store, state); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"settings": in})
	return nil
}
func (a *App) previewBankSync(w http.ResponseWriter, r *http.Request, u User) error {
	if err := bankSyncAdmin(u); err != nil {
		return err
	}
	state, err := loadBankSyncState(a.store)
	if err != nil {
		return err
	}
	commit, files, err := fetchBankSyncArchive(r.Context(), state.Settings)
	if err != nil {
		return err
	}
	catalogs, records, err := a.parseBankSyncArchive(files, u)
	if err != nil {
		return err
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	latest, err := loadBankSyncState(a.store)
	if err != nil {
		return err
	}
	if encoded(latest.Settings) != encoded(state.Settings) {
		return fail(409, "同步设置已变化，请重新预览")
	}
	for _, id := range latest.Settings.Catalogs {
		found := false
		for _, c := range catalogs {
			found = found || c.ID == id
		}
		if !found {
			return fail(400, "所选题库分类不在仓库中："+id)
		}
	}
	latest.Catalogs = catalogs
	latest.Snapshot = &bankSyncSnapshot{commit, latest.Settings, records}
	items, err := planBankSync(a.store, latest, u)
	if err != nil {
		return err
	}
	if err = saveBankSyncState(a.store, latest); err != nil {
		return err
	}
	counts := map[string]int{"add": 0, "unchanged": 0, "update": 0, "conflict": 0}
	for _, item := range items {
		counts[item.Action]++
	}
	respond(w, 200, map[string]any{"commit": commit, "catalogs": catalogs, "items": items, "counts": counts})
	return nil
}

// Fingerprints contain authored content only. Visibility is a local choice and is never overwritten by a pull.
func bankSyncFingerprint(t Task) string {
	normalizeTask(&t)
	t.ID = ""
	t.CreatedAt = ""
	t.CreatedBy = ""
	t.BankVisibility = "public"
	t.ResourcesURL = ""
	t.Status = "published"
	t.PublishedAt = ""
	t.Assigned = false
	t.CanSubmit = false
	t.MyAccepted = false
	t.AcceptedCount = 0
	t.AttemptCount = 0
	t.Locked = false
	t.SubmissionCount = 0
	t.MySubmission = nil
	t.Tags = append([]string{}, t.Tags...)
	t.FileIDs = append([]string{}, t.FileIDs...)
	return tokenHash(encoded(t))
}
func bankSyncSelected(catalogs, selected []string) bool {
	for _, c := range catalogs {
		if includesID(selected, c) {
			return true
		}
	}
	return false
}
func planBankSync(s *Store, state bankSyncState, u User) ([]bankSyncItem, error) {
	if state.Snapshot == nil {
		return nil, fail(409, "请先预览同步内容")
	}
	tasks, err := readAll[Task](s, "SELECT payload FROM tasks")
	if err != nil {
		return nil, err
	}
	byID := map[string]Task{}
	byTitle := map[string][]Task{}
	protected := map[string]bool{}
	for _, t := range tasks {
		normalizeTask(&t)
		byID[t.ID] = t
		if t.Category == "problem" {
			key := t.Direction + "\n" + t.Title
			byTitle[key] = append(byTitle[key], t)
		}
		for _, q := range t.Questions {
			if q.SourceProblemID != "" {
				protected[q.SourceProblemID] = true
			}
		}
	}
	var rows *sql.Rows
	if s.tx != nil {
		rows, err = s.tx.Query("SELECT DISTINCT task_id FROM submissions UNION SELECT DISTINCT task_id FROM judge_runs")
	} else {
		rows, err = s.db.Query("SELECT DISTINCT task_id FROM submissions UNION SELECT DISTINCT task_id FROM judge_runs")
	}
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		protected[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	claimed := map[string]string{}
	for key, mapping := range state.Mappings {
		claimed[mapping.TaskID] = key
	}
	items := []bankSyncItem{}
	for _, rec := range state.Snapshot.Records {
		if !bankSyncSelected(rec.Catalogs, state.Settings.Catalogs) {
			continue
		}
		source := rec.Task
		normalizeTask(&source)
		item := bankSyncItem{Key: rec.Key, Title: source.Title, Direction: source.Direction, Catalogs: rec.Catalogs, Action: "add", source: source}
		mapKey := state.Settings.Repository + "/" + rec.Key
		mapping, mapped := state.Mappings[mapKey]
		var target Task
		found := false
		if mapped {
			target, found = byID[mapping.TaskID]
			if !found {
				item.Action = "conflict"
				item.Reason = "已关联题目被删除，请人工处理"
				items = append(items, item)
				continue
			}
		} else {
			target, found = byID[tokenHash("mr-system-question-bank/"+rec.Key)]
			if !found {
				candidates := byTitle[source.Direction+"\n"+source.Title]
				for _, candidate := range candidates {
					if bankSyncFingerprint(candidate) == bankSyncFingerprint(source) {
						if found {
							item.Action = "conflict"
							item.Reason = "存在多个相同题目，需人工确认"
							break
						}
						target = candidate
						found = true
					}
				}
				if !found && len(candidates) > 0 {
					item.Action = "conflict"
					item.Reason = "已有同名题目，内容不同；保留本地版本"
				}
			}
		}
		if found && item.Action != "conflict" {
			item.target = target
			localHash := bankSyncFingerprint(target)
			remoteHash := bankSyncFingerprint(source)
			if localHash == remoteHash {
				item.Action = "unchanged"
			} else if !mapped || localHash != mapping.Fingerprint {
				item.Action = "conflict"
				item.Reason = "本地内容与来源不同，保留本地编辑"
			} else if protected[target.ID] {
				item.Action = "conflict"
				item.Reason = "已有作答或作业引用，保留历史题目"
			} else {
				item.Action = "update"
			}
		}
		if found && item.Action != "conflict" {
			if owner, ok := claimed[target.ID]; ok && owner != mapKey {
				item.Action = "conflict"
				item.Reason = "该题已关联其他来源题目，请人工确认"
			} else {
				claimed[target.ID] = mapKey
			}
		}
		if item.Action == "add" {
			source.ID = tokenHash("mr-bank-sync/" + mapKey)
			if _, exists := byID[source.ID]; exists {
				item.Action = "conflict"
				item.Reason = "题目标识冲突"
			}
			source.CreatedAt = now()
			source.CreatedBy = u.ID
			source.BankVisibility = state.Settings.DefaultVisibility
			item.target = source
		}
		items = append(items, item)
	}
	return items, nil
}
func (a *App) applyBankSync(w http.ResponseWriter, r *http.Request, u User) error {
	if err := bankSyncAdmin(u); err != nil {
		return err
	}
	var in struct {
		Commit string `json:"commit"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	state, err := loadBankSyncState(a.store)
	if err != nil {
		return err
	}
	if state.Snapshot == nil || in.Commit == "" || state.Snapshot.Commit != in.Commit || encoded(state.Snapshot.Settings) != encoded(state.Settings) {
		return fail(409, "预览已失效，请重新拉取预览")
	}
	tx, err := a.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	items, err := planBankSync(s, state, u)
	if err != nil {
		return err
	}
	result := bankSyncResult{Commit: in.Commit, At: now()}
	for _, item := range items {
		switch item.Action {
		case "conflict":
			result.Conflicts++
			continue
		case "add":
			if err = s.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", item.target.ID, encoded(item.target)); err != nil {
				return err
			}
			result.Imported++
		case "update":
			replacement := item.source
			replacement.ID = item.target.ID
			replacement.CreatedBy = item.target.CreatedBy
			replacement.CreatedAt = item.target.CreatedAt
			replacement.BankVisibility = item.target.BankVisibility
			replacement.ResourcesURL = item.target.ResourcesURL
			replacement.Status = item.target.Status
			replacement.PublishedAt = item.target.PublishedAt
			item.target = replacement
			if err = s.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(replacement), replacement.ID); err != nil {
				return err
			}
			result.Updated++
		case "unchanged":
			result.Unchanged++
		}
		state.Mappings[state.Settings.Repository+"/"+item.Key] = bankSyncMapping{item.target.ID, bankSyncFingerprint(item.target), item.Catalogs}
	}
	state.LastSync = &result
	state.Snapshot = nil
	if err = saveBankSyncState(s, state); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "同步题库", fmt.Sprintf("新增 %d，更新 %d，保留 %d，冲突 %d", result.Imported, result.Updated, result.Unchanged, result.Conflicts))
	respond(w, 200, result)
	return nil
}
func (a *App) setBankSyncVisibility(w http.ResponseWriter, r *http.Request, u User) error {
	if err := bankSyncAdmin(u); err != nil {
		return err
	}
	var in struct {
		IDs        []string `json:"ids"`
		Visibility string   `json:"visibility"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if len(in.IDs) == 0 || len(in.IDs) > 5000 || (in.Visibility != "public" && in.Visibility != "internal") {
		return fail(400, "请选择题目和有效的可见范围")
	}
	tx, err := a.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	ids := normalizedIDs(in.IDs)
	for _, id := range ids {
		t, err := s.task(id)
		if err != nil {
			return err
		}
		normalizeTask(&t)
		if t.Category != "problem" {
			return fail(400, "只能修改题库题目的可见范围")
		}
		t.BankVisibility = in.Visibility
		if err = s.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(t), id); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "调整题库可见范围", fmt.Sprintf("%d 道题：%s", len(ids), in.Visibility))
	respond(w, 200, map[string]any{"updated": len(ids)})
	return nil
}
