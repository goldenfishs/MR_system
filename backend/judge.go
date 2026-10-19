package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Judge0 CE API: https://ce.judge0.com/. User programs only run on that
// separately deployed sandbox service; this web process never executes them.
type JudgeCase struct {
	Input          string `json:"input"`
	ExpectedOutput string `json:"expectedOutput"`
	Hidden         bool   `json:"hidden"`
}
type JudgeConfig struct {
	Enabled          bool        `json:"enabled"`
	TestCases        []JudgeCase `json:"testCases"`
	LanguageIDs      []int       `json:"languageIds"`
	TimeLimitSeconds float64     `json:"timeLimitSeconds"`
	TotalCases       int         `json:"totalCases"`
}
type JudgeLanguage struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
type JudgeResult struct {
	Index         int    `json:"index"`
	Status        string `json:"status"`
	StatusID      int    `json:"statusId"`
	Stdout        string `json:"stdout,omitempty"`
	Stderr        string `json:"stderr,omitempty"`
	CompileOutput string `json:"compileOutput,omitempty"`
	Time          string `json:"time,omitempty"`
	Memory        *int   `json:"memory,omitempty"`
	Hidden        bool   `json:"hidden"`
}
type JudgeRun struct {
	Preview      bool          `json:"preview,omitempty"`
	ID           string        `json:"id"`
	TaskID       string        `json:"taskId"`
	QuestionID   string        `json:"questionId,omitempty"`
	UserID       string        `json:"userId"`
	SourceFileID string        `json:"sourceFileId"`
	SourceHash   string        `json:"sourceHash"`
	LanguageID   int           `json:"languageId"`
	Status       string        `json:"status"`
	Passed       int           `json:"passed"`
	Total        int           `json:"total"`
	Score        float64       `json:"score"`
	Results      []JudgeResult `json:"results"`
	CreatedAt    string        `json:"createdAt"`
	FinishedAt   string        `json:"finishedAt,omitempty"`
	Error        string        `json:"error,omitempty"`
}

func validateJudgeConfig(t *Task) error {
	if t.Judge == nil {
		return nil
	}
	j := t.Judge
	if t.SubmissionType != "code" || t.Kind != "code" && t.Kind != "quiz" {
		return fail(400, "在线判题仅适用于代码任务和编程考试")
	}
	if len(j.TestCases) == 0 || len(j.TestCases) > 10 || len(j.LanguageIDs) == 0 || len(j.LanguageIDs) > 20 {
		return fail(400, "请设置 1 至 10 个测试用例及允许的编程语言")
	}
	for _, c := range j.TestCases {
		if len(c.Input) > 16000 || len(c.ExpectedOutput) > 16000 {
			return fail(400, "单个测试用例输入或输出不能超过 16 KB")
		}
	}
	seen := map[int]bool{}
	for _, id := range j.LanguageIDs {
		if id < 1 || id > 10000 || seen[id] {
			return fail(400, "判题语言编号无效或重复")
		}
		seen[id] = true
	}
	if j.TimeLimitSeconds == 0 {
		j.TimeLimitSeconds = 2
	}
	if !finite(j.TimeLimitSeconds) || j.TimeLimitSeconds < 0.1 || j.TimeLimitSeconds > 5 {
		return fail(400, "运行时限必须在 0.1 至 5 秒之间")
	}
	if t.Kind == "quiz" && (t.StartsAt == "" || t.DueAt == "") {
		return fail(400, "编程考试必须设置开始和截止时间")
	}
	j.Enabled = true
	j.TotalCases = len(j.TestCases)
	return nil
}
func publicTask(t Task, u User) Task {
	// Copy before normalization/redaction: Task is a value, but Questions is a shared slice.
	t.Questions = append([]TaskQuestion(nil), t.Questions...)
	normalizeTask(&t)
	if taskLocked(t, u) {
		t.Locked = true
		t.Description, t.ResourcesURL = "", ""
		t.IllustrationURL, t.IllustrationAlt = "", ""
		t.FileIDs = []string{}
		t.Questions = nil
		t.Judge = nil
		t.MechanicalProblem = nil
		return t
	}
	if !hasPermission(u, t.Direction, "tasks") {
		for i := range t.Questions {
			t.Questions[i].Answers = nil
			t.Questions[i].Explanation = ""
			if t.Questions[i].Judge != nil {
				config := *t.Questions[i].Judge
				config.TestCases = []JudgeCase{}
				for _, c := range t.Questions[i].Judge.TestCases {
					if !c.Hidden {
						config.TestCases = append(config.TestCases, c)
					}
				}
				config.Enabled = true
				config.TotalCases = len(t.Questions[i].Judge.TestCases)
				t.Questions[i].Judge = &config
			}
		}
	}
	if t.MechanicalProblem != nil && !hasPermission(u, t.Direction, "tasks") {
		problem := *t.MechanicalProblem
		if problem.Type != "drawing" {
			problem.Answers = []string{}
			problem.Explanation = ""
			problem.AnswerFileIDs = []string{}
		}
		t.MechanicalProblem = &problem
	}
	if t.Judge == nil || hasPermission(u, t.Direction, "tasks") {
		return t
	}
	config := *t.Judge
	config.TestCases = []JudgeCase{}
	for _, c := range t.Judge.TestCases {
		if !c.Hidden {
			config.TestCases = append(config.TestCases, c)
		}
	}
	config.Enabled = true
	config.TotalCases = len(t.Judge.TestCases)
	t.Judge = &config
	return t
}
func publicJudgeRun(run JudgeRun, u User) JudgeRun {
	if u.Role == "admin" {
		return run
	}
	run.Results = append([]JudgeResult{}, run.Results...)
	for i := range run.Results {
		if run.Results[i].Hidden {
			run.Results[i].Stdout = ""
			run.Results[i].Stderr = ""
			run.Results[i].CompileOutput = ""
			run.Results[i].Time = ""
			run.Results[i].Memory = nil
		}
	}
	return run
}
func validateJudgeURL(raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("JUDGE_URL must be an HTTP(S) service URL without credentials, query or fragment")
	}
	return nil
}
func (a *App) judgeRequest(ctx context.Context, method, path string, body any, out any) error {
	if a.config.JudgeURL == "" {
		return fail(503, "尚未配置独立判题服务")
	}
	if err := validateJudgeURL(a.config.JudgeURL); err != nil {
		return errors.New("invalid judge configuration")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.config.JudgeURL, "/")+path, reader)
	if err != nil {
		return errors.New("cannot create judge request")
	}
	req.Header.Set("Content-Type", "application/json")
	if a.config.JudgeToken != "" {
		req.Header.Set("X-Auth-Token", a.config.JudgeToken)
	}
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("judge service is unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("judge service returned status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return errors.New("invalid judge service response")
	}
	if err = json.Unmarshal(data, out); err != nil {
		return errors.New("invalid judge service response")
	}
	return nil
}
func (a *App) getJudgeConfig(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "tasks") {
		if err := a.requireLearningAccess(u); err != nil {
			return err
		}
	}
	if a.config.JudgeURL == "" {
		respond(w, 200, map[string]any{"configured": false, "languages": []JudgeLanguage{}})
		return nil
	}
	var languages []JudgeLanguage
	if err := a.judgeRequest(r.Context(), http.MethodGet, "/languages", nil, &languages); err != nil {
		return fail(503, "判题服务暂时不可用，请联系管理员")
	}
	if len(languages) > 500 {
		return fail(503, "判题服务返回无效语言列表")
	}
	respond(w, 200, map[string]any{"configured": true, "languages": languages})
	return nil
}
func (a *App) readJudgeSource(id string, u User) ([]byte, error) {
	asset, err := a.fileAsset(id)
	if err != nil {
		return nil, fail(400, "源代码文件不存在")
	}
	if asset.OwnerID != u.ID {
		return nil, fail(403, "只能编译本人上传的源代码")
	}
	if asset.Size == 0 || asset.Size > 64<<10 {
		return nil, fail(400, "单个源代码文件大小应在 1 字节至 64 KB 之间")
	}
	switch strings.ToLower(filepath.Ext(asset.Name)) {
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".py", ".java", ".js", ".ts", ".go", ".rs", ".cs", ".rb", ".php", ".kt", ".swift", ".lua", ".pas", ".hs", ".scala", ".m", ".ml", ".pl", ".sh", ".txt":
	default:
		return nil, fail(400, "请上传可识别的源代码文本文件，不能直接编译压缩包")
	}
	reader, _, err := a.openAsset(id)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	source, err := io.ReadAll(io.LimitReader(reader, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(source) == 0 || len(source) > 64<<10 || !utf8.Valid(source) || bytes.ContainsRune(source, 0) {
		return nil, fail(400, "源代码必须是 64 KB 以内的 UTF-8 文本")
	}
	return source, nil
}

// Managers may validate bank questions without enrolling as students.
func (a *App) requireJudgeExecution(t Task, u User, preview ...bool) error {
	if len(preview) > 0 && preview[0] {
		return requirePermission(u, t.Direction, "tasks")
	}
	normalizeTask(&t)
	if t.Category == "problem" && hasPermission(u, t.Direction, "tasks") {
		return nil
	}
	return a.requireTaskSubmission(t, u)
}

func (a *App) startJudge(w http.ResponseWriter, r *http.Request, u User) error {
	if a.config.JudgeURL == "" {
		return fail(503, "尚未配置独立判题服务，请联系管理员")
	}
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	var in struct {
		LanguageID   int    `json:"languageId"`
		SourceFileID string `json:"sourceFileId"`
		QuestionID   string `json:"questionId"`
		Preview      bool   `json:"preview"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if err = a.requireJudgeExecution(task, u, in.Preview); err != nil {
		return err
	}
	judgeConfig, questionMaxScore, err := taskJudgeConfig(task, in.QuestionID)
	if err != nil {
		return err
	}
	allowed := false
	for _, id := range judgeConfig.LanguageIDs {
		if id == in.LanguageID {
			allowed = true
		}
	}
	if !allowed {
		return fail(400, "此任务不支持所选编程语言")
	}
	source, err := a.readJudgeSource(in.SourceFileID, u)
	if err != nil {
		return err
	}
	// Serialize the final eligibility check and durable enqueue with membership
	// changes. The earlier file read performs no network I/O and happens before
	// this short write section; running programs never hold the write lock.
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	// A scheduled task can be edited while source bytes are read. Reload it under
	// the mutation lock before enqueueing so deadlines and tests cannot be stale.
	task, err = a.store.task(task.ID)
	if err != nil {
		return err
	}
	if task.Judge == nil || !task.Judge.Enabled {
		if in.QuestionID == "" {
			return fail(409, "任务判题配置已变更，请重新打开任务")
		}
	}
	judgeConfig, questionMaxScore, err = taskJudgeConfig(task, in.QuestionID)
	if err != nil {
		return fail(409, "作业题判题配置已变更，请重新打开任务")
	}
	languageAllowed := false
	for _, id := range judgeConfig.LanguageIDs {
		if id == in.LanguageID {
			languageAllowed = true
		}
	}
	if !languageAllowed {
		return fail(409, "任务编程语言已变更，请重新选择")
	}
	if err = a.requireJudgeExecution(task, u, in.Preview); err != nil {
		return err
	}
	a.platformMu.Lock()
	if a.judgeSlots == nil {
		a.judgeSlots = make(chan struct{}, 2)
	}
	slots := a.judgeSlots
	a.platformMu.Unlock()
	select {
	case slots <- struct{}{}:
	default:
		return &apiError{status: 429, message: "判题队列繁忙，请稍后重试", retryAfterSeconds: 5}
	}
	reserved := true
	defer func() {
		if reserved {
			<-slots
		}
	}()
	a.platformMu.Lock()
	var runs int
	cutoff := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	// CreatedAt is in the immutable payload; count these bounded rows without
	// dialect-specific JSON SQL, so SQLite and PostgreSQL behave identically.
	recent, err := readAll[JudgeRun](a.store, "SELECT payload FROM judge_runs WHERE user_id=?", u.ID)
	if err == nil {
		for _, run := range recent {
			if run.CreatedAt >= cutoff {
				runs++
			}
		}
	}
	if err != nil {
		a.platformMu.Unlock()
		return err
	}
	if runs >= 5 {
		a.platformMu.Unlock()
		return &apiError{status: 429, message: "每分钟最多提交 5 次编译测试", retryAfterSeconds: 60}
	}
	run := JudgeRun{Preview: in.Preview, ID: randomID(), TaskID: task.ID, QuestionID: in.QuestionID, UserID: u.ID, SourceFileID: in.SourceFileID, SourceHash: tokenHash(string(source)), LanguageID: in.LanguageID, Status: "queued", Total: len(judgeConfig.TestCases), Results: []JudgeResult{}, CreatedAt: now()}
	err = a.store.exec("INSERT INTO judge_runs(id,task_id,user_id,status,payload) VALUES(?,?,?,?,?)", run.ID, task.ID, u.ID, run.Status, encoded(run))
	a.platformMu.Unlock()
	if err != nil {
		return err
	}
	reserved = false
	judgeTask := task
	judgeTask.Judge = judgeConfig
	judgeTask.MaxScore = questionMaxScore
	go func() { defer func() { <-slots }(); a.executeJudge(run, judgeTask, source) }()
	respond(w, 202, map[string]any{"item": publicJudgeRun(run, u)})
	return nil
}

func taskJudgeConfig(task Task, questionID string) (*JudgeConfig, float64, error) {
	if questionID == "" {
		if task.Judge == nil || !task.Judge.Enabled {
			return nil, 0, fail(400, "此任务未启用在线判题")
		}
		return task.Judge, task.MaxScore, nil
	}
	if task.Category != "assignment" {
		return nil, 0, fail(400, "只有多题作业支持逐题代码判题")
	}
	for _, question := range task.Questions {
		if question.ID != questionID {
			continue
		}
		if question.Type != "code" || question.Judge == nil || !question.Judge.Enabled {
			return nil, 0, fail(400, "此作业题未启用在线判题")
		}
		return question.Judge, question.MaxScore, nil
	}
	return nil, 0, fail(404, "作业题目不存在")
}

func (a *App) validateQuestionJudgeRun(task Task, question TaskQuestion, u User, runID string, requireComplete bool) (JudgeRun, error) {
	if runID == "" {
		return JudgeRun{}, fail(400, "请先运行并提交代码题")
	}
	run, err := readOne[JudgeRun](a.store, "SELECT payload FROM judge_runs WHERE id=?", runID)
	if err != nil {
		return JudgeRun{}, fail(400, "在线判题记录不存在，请重新运行代码")
	}
	if run.Preview {
		return JudgeRun{}, fail(400, "管理测试记录不能用于提交作业")
	}
	if run.TaskID != task.ID || run.QuestionID != question.ID || run.UserID != u.ID {
		return JudgeRun{}, fail(403, "此判题记录不属于当前用户和作业题目")
	}
	if question.Judge == nil || !question.Judge.Enabled {
		return JudgeRun{}, fail(409, "代码题判题配置已变更，请联系管理员")
	}
	allowed := false
	for _, languageID := range question.Judge.LanguageIDs {
		if languageID == run.LanguageID {
			allowed = true
			break
		}
	}
	if !allowed {
		return JudgeRun{}, fail(409, "判题使用的语言已不再受支持，请重新运行代码")
	}
	source, err := a.readJudgeSource(run.SourceFileID, u)
	if err != nil || tokenHash(string(source)) != run.SourceHash {
		return JudgeRun{}, fail(409, "判题源码已失效，请重新运行代码")
	}
	if requireComplete {
		if run.Status != "completed" {
			return JudgeRun{}, fail(409, "代码仍在评测或评测失败，请等待完成后再提交")
		}
		if run.Total != len(question.Judge.TestCases) || run.Total == 0 || run.Passed < 0 || run.Passed > run.Total || len(run.Results) != run.Total {
			return JudgeRun{}, fail(409, "判题结果与当前题目配置不一致，请重新运行代码")
		}
		accepted := 0
		for _, result := range run.Results {
			if result.StatusID == 3 {
				accepted++
			}
		}
		if accepted != run.Passed {
			return JudgeRun{}, fail(409, "判题用例统计不一致，请重新运行代码")
		}
		run.Score = float64(run.Passed) / float64(run.Total) * question.MaxScore
	}
	return run, nil
}
func (a *App) getJudgeRun(w http.ResponseWriter, r *http.Request, u User) error {
	run, err := readOne[JudgeRun](a.store, "SELECT payload FROM judge_runs WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	task, err := a.store.task(run.TaskID)
	if err != nil {
		return err
	}
	if run.UserID != u.ID && !hasPermission(u, task.Direction, "tasks") {
		return fail(403, "没有权限查看此判题记录")
	}
	visible, err := a.taskVisibleTo(task, u)
	if err != nil {
		return err
	}
	if !visible {
		return fail(403, "没有权限查看此任务的判题记录")
	}
	respond(w, 200, map[string]any{"item": publicJudgeRun(run, u)})
	return nil
}
func (a *App) saveJudgeRun(run JudgeRun) error {
	return a.store.exec("UPDATE judge_runs SET status=?,payload=? WHERE id=?", run.Status, encoded(run), run.ID)
}
func (a *App) executeJudge(run JudgeRun, task Task, source []byte) {
	run.Status = "running"
	if err := a.saveJudgeRun(run); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for index, testCase := range task.Judge.TestCases {
		result, err := a.executeJudgeCase(ctx, run.LanguageID, source, *task.Judge, testCase, index)
		if err != nil {
			run.Status = "error"
			run.Error = "判题服务未完成测试，请稍后重新提交"
			run.FinishedAt = now()
			_ = a.saveJudgeRun(run)
			return
		}
		run.Results = append(run.Results, result)
		if result.StatusID == 3 {
			run.Passed++
		}
		run.Score = float64(run.Passed) / float64(run.Total) * task.MaxScore
		if err = a.saveJudgeRun(run); err != nil {
			return
		}
	}
	run.Status = "completed"
	run.FinishedAt = now()
	_ = a.saveJudgeRun(run)
}
func safeJudgeToken(token string) bool {
	if len(token) == 0 || len(token) > 100 {
		return false
	}
	for _, r := range token {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}
func decodeJudgeOutput(value *string) string {
	if value == nil {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(*value)
	if err != nil {
		return ""
	}
	if len(decoded) > 8192 {
		decoded = decoded[:8192]
	}
	return strings.ToValidUTF8(string(decoded), "�")
}
func (a *App) executeJudgeCase(parent context.Context, language int, source []byte, config JudgeConfig, testCase JudgeCase, index int) (JudgeResult, error) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	body := map[string]any{"language_id": language, "source_code": base64.StdEncoding.EncodeToString(source), "stdin": base64.StdEncoding.EncodeToString([]byte(testCase.Input)), "expected_output": base64.StdEncoding.EncodeToString([]byte(testCase.ExpectedOutput)), "cpu_time_limit": config.TimeLimitSeconds, "cpu_extra_time": 0.5, "wall_time_limit": 10, "memory_limit": 256 * 1024, "stack_limit": 64 * 1024, "max_processes_and_or_threads": 32, "enable_per_process_and_thread_time_limit": false, "enable_per_process_and_thread_memory_limit": false, "max_file_size": 1024, "enable_network": false, "number_of_runs": 1}
	var created struct {
		Token string `json:"token"`
	}
	if err := a.judgeRequest(ctx, http.MethodPost, "/submissions?base64_encoded=true&wait=false", body, &created); err != nil {
		return JudgeResult{}, err
	}
	if !safeJudgeToken(created.Token) {
		return JudgeResult{}, errors.New("invalid judge token")
	}
	for {
		var result struct {
			Stdout        *string `json:"stdout"`
			Stderr        *string `json:"stderr"`
			CompileOutput *string `json:"compile_output"`
			Time          string  `json:"time"`
			Memory        *int    `json:"memory"`
			Status        struct {
				ID          int    `json:"id"`
				Description string `json:"description"`
			} `json:"status"`
		}
		err := a.judgeRequest(ctx, http.MethodGet, "/submissions/"+created.Token+"?base64_encoded=true&fields=stdout,stderr,compile_output,status,time,memory", nil, &result)
		if err != nil {
			return JudgeResult{}, err
		}
		if result.Status.ID >= 3 && result.Status.ID <= 12 || result.Status.ID == 14 {
			return JudgeResult{Index: index, Status: result.Status.Description, StatusID: result.Status.ID, Stdout: decodeJudgeOutput(result.Stdout), Stderr: decodeJudgeOutput(result.Stderr), CompileOutput: decodeJudgeOutput(result.CompileOutput), Time: result.Time, Memory: result.Memory, Hidden: testCase.Hidden}, nil
		}
		if result.Status.ID != 1 && result.Status.ID != 2 {
			return JudgeResult{}, errors.New("judge returned internal error")
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return JudgeResult{}, ctx.Err()
		case <-timer.C:
		}
	}
}
