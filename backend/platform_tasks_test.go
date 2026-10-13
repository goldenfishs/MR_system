package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

func taskForm() Task {
	return Task{Title: "电控训练", Description: "完成控制代码并上传成果", Direction: "电控", Team: "RoboMaster", Kind: "code", Difficulty: "入门", MaxScore: 100, SubmissionType: "files", FileIDs: []string{}}
}
func taskForTest(t *testing.T, c *testClient, task Task) Task {
	t.Helper()
	return unmarshal[Task](t, c.request("POST", "/api/tasks", task, 201)["item"])
}
func waitCampaign(t *testing.T, c *testClient, id string) MailCampaign {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		item := unmarshal[MailCampaign](t, c.request("GET", "/api/mail-campaigns/"+id, nil, 200)["item"])
		if item.Status != "sending" {
			return item
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("campaign did not finish")
	return MailCampaign{}
}
func waitJudge(t *testing.T, c *testClient, id string) JudgeRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		item := unmarshal[JudgeRun](t, c.request("GET", "/api/judge-runs/"+id, nil, 200)["item"])
		if item.Status == "completed" || item.Status == "error" {
			return item
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("judge did not finish")
	return JudgeRun{}
}

type noticeFixture struct {
	mu       sync.Mutex
	calls    map[string]int
	failOnce string
	block    <-chan struct{}
	started  chan struct{}
}

func (f *noticeFixture) SendMessage(ctx context.Context, to, subject, body string) error {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[to]++
	call := f.calls[to]
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if to == f.failOnce && call == 1 {
		return errors.New("temporary SMTP failure")
	}
	return nil
}
func (f *noticeFixture) count(to string) int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls[to] }

func TestMailCampaignDraftPrivacyRetryAndIdempotence(t *testing.T) {
	a, c := setup(t)
	mailer := &noticeFixture{failOnce: "student@demo.local"}
	a.config.NoticeMailer = mailer
	c.login("admin")
	c.request("POST", "/api/mail-campaigns", map[string]any{"subject": "bad\r\nBcc:bad@example.com", "body": "body", "userIds": []string{"demo-student"}}, 400)
	c.request("POST", "/api/mail-campaigns", map[string]any{"subject": "通知", "body": "内容", "userIds": []string{"missing"}}, 400)
	campaign := unmarshal[MailCampaign](t, c.request("POST", "/api/mail-campaigns", map[string]any{"subject": "培训通知", "body": "请参加本周训练", "userIds": []string{"demo-student", "demo-mentor", "demo-student"}}, 201)["item"])
	if campaign.Status != "draft" || len(campaign.Recipients) != 2 || mailer.count("student@demo.local") != 0 {
		t.Fatal("creating a draft sent mail or duplicated recipients")
	}
	c.login("student")
	c.request("GET", "/api/mail-campaigns", nil, 403)
	c.request("GET", "/api/mail-campaigns/"+campaign.ID, nil, 403)
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 403)
	c.login("admin")
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 202)
	item := waitCampaign(t, c, campaign.ID)
	if item.Status != "partial" || item.Recipients[0].Status != "failed" || item.Recipients[1].Status != "sent" {
		t.Fatalf("incorrect delivery outcomes: %+v", item)
	}
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 200)
	if mailer.count("student@demo.local") != 1 || mailer.count("mentor@demo.local") != 1 {
		t.Fatal("repeated send duplicated delivery")
	}
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/retry", nil, 202)
	item = waitCampaign(t, c, campaign.ID)
	if item.Status != "completed" || mailer.count("student@demo.local") != 2 || mailer.count("mentor@demo.local") != 1 {
		t.Fatal("retry should resend only failed recipients")
	}
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/retry", nil, 409)
}
func TestMailSendingDoesNotHoldGlobalMutationLock(t *testing.T) {
	a, c := setup(t)
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	a.config.NoticeMailer = &noticeFixture{block: release, started: started}
	c.login("admin")
	campaign := unmarshal[MailCampaign](t, c.request("POST", "/api/mail-campaigns", map[string]any{"subject": "异步通知", "body": "本地测试", "userIds": []string{"demo-student"}}, 201)["item"])
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 202)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("mailer not called")
	}
	done := make(chan struct{})
	go func() { a.store.mu.Lock(); a.store.mu.Unlock(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("SMTP held global write lock")
	}
	c.request("PATCH", "/api/settings", map[string]string{"announcement": "still responsive"}, 200)
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 200)
	second := unmarshal[MailCampaign](t, c.request("POST", "/api/mail-campaigns", map[string]any{"subject": "第二批", "body": "本地测试", "userIds": []string{"demo-mentor"}}, 201)["item"])
	c.request("POST", "/api/mail-campaigns/"+second.ID+"/send", nil, 202)
	third := unmarshal[MailCampaign](t, c.request("POST", "/api/mail-campaigns", map[string]any{"subject": "第三批", "body": "本地测试", "userIds": []string{"demo-admin"}}, 201)["item"])
	c.request("POST", "/api/mail-campaigns/"+third.ID+"/send", nil, 429)
	close(release)
	waitCampaign(t, c, campaign.ID)
	waitCampaign(t, c, second.ID)
}
func TestMailAndJudgeDisabledAndRecovery(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	cfg := c.request("GET", "/api/mail-campaigns", nil, 200)
	if unmarshal[bool](t, cfg["configured"]) {
		t.Fatal("unconfigured mail presented as available")
	}
	campaign := unmarshal[MailCampaign](t, c.request("POST", "/api/mail-campaigns", map[string]any{"subject": "重启测试", "body": "body", "userIds": []string{"demo-student", "demo-mentor"}}, 201)["item"])
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 503)
	campaign.Status = "sending"
	campaign.Recipients[0].Status = "sending"
	if err := a.saveMailCampaign(campaign); err != nil {
		t.Fatal(err)
	}
	task := taskForTest(t, c, taskForm())
	run := JudgeRun{ID: randomID(), TaskID: task.ID, UserID: "demo-student", Status: "running", CreatedAt: now()}
	if err := a.store.exec("INSERT INTO judge_runs(id,task_id,user_id,status,payload) VALUES(?,?,?,?,?)", run.ID, run.TaskID, run.UserID, run.Status, encoded(run)); err != nil {
		t.Fatal(err)
	}
	if err := a.recoverPlatformJobs(); err != nil {
		t.Fatal(err)
	}
	campaign = unmarshal[MailCampaign](t, c.request("GET", "/api/mail-campaigns/"+campaign.ID, nil, 200)["item"])
	if campaign.Recipients[0].Status != "unknown" || campaign.Recipients[1].Status != "failed" {
		t.Fatal("crash recovery could duplicate a delivered message")
	}
	mailer := &noticeFixture{}
	a.config.NoticeMailer = mailer
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/retry", nil, 202)
	waitCampaign(t, c, campaign.ID)
	if mailer.count("student@demo.local") != 0 || mailer.count("mentor@demo.local") != 1 {
		t.Fatal("recovery retried an unknown delivery")
	}
	c.login("student")
	recovered := unmarshal[JudgeRun](t, c.request("GET", "/api/judge-runs/"+run.ID, nil, 200)["item"])
	if recovered.Status != "error" || recovered.Error == "" {
		t.Fatal("judge run not recovered")
	}
	config := c.request("GET", "/api/judge/config", nil, 200)
	if unmarshal[bool](t, config["configured"]) {
		t.Fatal("unconfigured judge presented as available")
	}
	c.request("POST", "/api/tasks/missing/judge", map[string]any{"languageId": 71, "sourceFileId": "missing"}, 503)
}

func TestTaskAttachmentsMembershipAndDirections(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	resource := uploadForTest(t, c, "instructions.pdf", []byte("%PDF-1.4\ntraining"), 201)
	team := unmarshal[Team](t, c.request("POST", "/api/teams", map[string]any{"name": "2027战队", "description": "机器人培养", "competition": "RoboMaster", "directions": []string{"电控", "机械"}}, 201)["item"])
	form := taskForm()
	form.TeamID = team.ID
	form.FileIDs = []string{resource.ID}
	task := taskForTest(t, c, form)
	form.TeamID = "unknown"
	c.request("POST", "/api/tasks", form, 400)
	c.login("student")
	tasks := unmarshal[[]Task](t, c.request("GET", "/api/tasks", nil, 200)["items"])
	found := false
	for _, item := range tasks {
		if item.ID == task.ID {
			found = true
			if item.CanSubmit {
				t.Fatal("nonmember can submit a team task")
			}
		}
	}
	if !found {
		t.Fatal("admitted learner cannot browse group content")
	}
	c.request("GET", "/api/files/"+resource.ID, nil, 200)
	c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{"content": "link only"}, 403)
	membership := Membership{TeamID: team.ID, UserID: "demo-student", Direction: "机械", JoinedAt: now()}
	if err := a.store.exec("INSERT INTO team_memberships(team_id,user_id,direction,joined_at,payload) VALUES(?,?,?,?,?)", team.ID, membership.UserID, membership.Direction, membership.JoinedAt, encoded(membership)); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{"content": "wrong direction"}, 403)
	if err := a.store.exec("UPDATE team_memberships SET direction='电控' WHERE team_id=? AND user_id=?", team.ID, membership.UserID); err != nil {
		t.Fatal(err)
	}
	// A newly admitted team member does not need a legacy recruitment record.
	if err := a.store.exec("DELETE FROM applicants WHERE user_id=?", membership.UserID); err != nil {
		t.Fatal(err)
	}
	downloadForTest(t, c, resource.ID, "download", 200)
	c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{"content": "text cannot replace file"}, 400)
	c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"fileIds": []string{resource.ID}}, 403)
	artifact := uploadForTest(t, c, "project.zip", []byte("project upload fixture"), 201)
	submission := unmarshal[Submission](t, c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"fileIds": []string{artifact.ID}}, 201)["item"])
	if len(submission.FileIDs) != 1 {
		t.Fatal("attachment not persisted")
	}
	c.login("interviewer")
	c.request("GET", "/api/files/"+artifact.ID, nil, 403)
	c.login("mentor")
	downloadForTest(t, c, artifact.ID, "download", 200)
	c.request("PATCH", "/api/submissions/"+submission.ID, map[string]any{"status": "approved", "score": 93, "feedback": "达到要求"}, 200)
	c.login("student")
	history := unmarshal[[]Submission](t, c.request("GET", "/api/tasks/"+task.ID+"/submissions", nil, 200)["items"])
	if history[0].Score == nil || *history[0].Score != 93 {
		t.Fatal("manual grading was lost")
	}
}

type judgeFixture struct {
	mu          sync.Mutex
	submissions map[string]map[string]any
	calls       int
	release     <-chan struct{}
	token       string
}

func (f *judgeFixture) handler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Auth-Token") != f.token {
		http.Error(w, "bad auth", 401)
		return
	}
	if r.URL.Path == "/languages" {
		respond(w, 200, []JudgeLanguage{{ID: 71, Name: "Python 3"}})
		return
	}
	if r.Method == "POST" && r.URL.Path == "/submissions" {
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		if input["enable_network"] != false || input["memory_limit"] != float64(256*1024) || input["cpu_time_limit"].(float64) > 5 || r.URL.Query().Get("base64_encoded") != "true" || r.URL.Query().Get("wait") != "false" {
			http.Error(w, "unsafe runtime options", 400)
			return
		}
		if f.release != nil {
			select {
			case <-f.release:
			case <-r.Context().Done():
				return
			}
		}
		f.mu.Lock()
		f.calls++
		token := fmt.Sprintf("test-%d", f.calls)
		if f.submissions == nil {
			f.submissions = map[string]map[string]any{}
		}
		f.submissions[token] = input
		f.mu.Unlock()
		respond(w, 201, map[string]string{"token": token})
		return
	}
	if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/submissions/") {
		f.mu.Lock()
		submission, ok := f.submissions[strings.TrimPrefix(r.URL.Path, "/submissions/")]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		status := map[string]any{"id": 3, "description": "Accepted"}
		if submission["stdin"] != submission["expected_output"] {
			status = map[string]any{"id": 4, "description": "Wrong Answer"}
		}
		respond(w, 200, map[string]any{"stdout": submission["stdin"], "stderr": submission["stdin"], "compile_output": submission["stdin"], "time": "0.01", "memory": 1024, "status": status})
		return
	}
	http.NotFound(w, r)
}
func configuredJudgeTask(t *testing.T, a *App, c *testClient, f *judgeFixture) (Task, FileAsset) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(server.Close)
	a.config.JudgeURL = server.URL
	a.config.JudgeToken = f.token
	c.login("admin")
	task := taskForm()
	task.SubmissionType = "code"
	task.Judge = &JudgeConfig{LanguageIDs: []int{71}, TestCases: []JudgeCase{{Input: "public", ExpectedOutput: "public"}, {Input: "hidden-secret-928", ExpectedOutput: "hidden-secret-928", Hidden: true}}, TimeLimitSeconds: 1}
	task = taskForTest(t, c, task)
	c.login("student")
	source := uploadForTest(t, c, "answer.py", []byte("print(input())\n"), 201)
	return task, source
}
func TestJudgeSandboxProjectionAndSubmissionAuthenticity(t *testing.T) {
	a, c := setup(t)
	fixture := &judgeFixture{token: "fixture-auth-token"}
	task, source := configuredJudgeTask(t, a, c, fixture)
	config := c.request("GET", "/api/judge/config", nil, 200)
	if !unmarshal[bool](t, config["configured"]) || strings.Contains(string(config["languages"]), fixture.token) {
		t.Fatal("judge availability/token exposure")
	}
	taskPayload := c.request("GET", "/api/tasks", nil, 200)
	if strings.Contains(string(taskPayload["items"]), "hidden-secret") {
		t.Fatal("hidden cases exposed to student")
	}
	c.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{"languageId": 999, "sourceFileId": source.ID}, 400)
	run := unmarshal[JudgeRun](t, c.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{"languageId": 71, "sourceFileId": source.ID}, 202)["item"])
	run = waitJudge(t, c, run.ID)
	if run.Status != "completed" || run.Passed != 2 || run.Score != 100 || len(run.Results) != 2 {
		t.Fatalf("unexpected judge result: %+v", run)
	}
	if run.Results[0].Stdout != "public" || run.Results[1].Stdout != "" || run.Results[1].Stderr != "" || run.Results[1].CompileOutput != "" {
		t.Fatal("hidden runtime output exposed")
	}
	c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"fileIds": []string{source.ID}}, 400)
	c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"fileIds": []string{source.ID}, "judgeRunId": "invented"}, 400)
	other := uploadForTest(t, c, "other.py", []byte("print('different')"), 201)
	c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"fileIds": []string{other.ID}, "judgeRunId": run.ID}, 400)
	submitted := unmarshal[Submission](t, c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"fileIds": []string{source.ID}, "judgeRunId": run.ID}, 201)["item"])
	if submitted.JudgeRunID != run.ID || submitted.Score != nil || submitted.Status != "pending" {
		t.Fatal("judge should remain separate from manual grading")
	}
	c.login("mentor")
	c.request("GET", "/api/judge-runs/"+run.ID, nil, 200)
	c.login("admin")
	adminRun := unmarshal[JudgeRun](t, c.request("GET", "/api/judge-runs/"+run.ID, nil, 200)["item"])
	if adminRun.Results[1].Stdout != "hidden-secret-928" {
		t.Fatal("admin cannot inspect hidden result")
	}
	c.login("student")
	c.register("另一个成员", "judge.other@example.com", "LongPassword!123")
	c.request("GET", "/api/judge-runs/"+run.ID, nil, 403)
}
func TestJudgeExamWindowOwnershipAndCapacity(t *testing.T) {
	a, c := setup(t)
	release := make(chan struct{})
	fixture := &judgeFixture{token: "test", release: release}
	task, source := configuredJudgeTask(t, a, c, fixture)
	c.login("admin")
	future := taskForm()
	future.Kind = "quiz"
	future.SubmissionType = "code"
	future.StartsAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	future.DueAt = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	future.Judge = task.Judge
	future = taskForTest(t, c, future)
	foreign := uploadForTest(t, c, "staff.py", []byte("print(input())"), 201)
	c.login("student")
	c.request("POST", "/api/tasks/"+future.ID+"/judge", map[string]any{"languageId": 71, "sourceFileId": source.ID}, 409)
	c.request("POST", "/api/tasks/"+future.ID+"/submissions", map[string]any{"fileIds": []string{source.ID}}, 409)
	c.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{"languageId": 71, "sourceFileId": foreign.ID}, 403)
	oversized := uploadForTest(t, c, "large.py", []byte(strings.Repeat("x", (64<<10)+1)), 201)
	c.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{"languageId": 71, "sourceFileId": oversized.ID}, 400)
	runs := []JudgeRun{}
	for i := 0; i < 2; i++ {
		runs = append(runs, unmarshal[JudgeRun](t, c.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{"languageId": 71, "sourceFileId": source.ID}, 202)["item"]))
	}
	c.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{"languageId": 71, "sourceFileId": source.ID}, 429)
	close(release)
	for _, run := range runs {
		if waitJudge(t, c, run.ID).Status != "completed" {
			t.Fatal("queued judge failed")
		}
	}
}
func TestNotificationSMTPUsesSingleRecipientAndSafeHeaders(t *testing.T) {
	host, port, messages, _ := localSMTP(t)
	sender, err := newSMTPMailer(SMTPConfig{Host: host, Port: port, From: "sender@example.test", FromName: "训练中心", Security: "none", Demo: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.SendMessage(context.Background(), "student@example.test", "训练通知", "第一行\n第二行"); err != nil {
		t.Fatal(err)
	}
	data := <-messages
	message, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if message.Header.Get("To") != "<student@example.test>" || message.Header.Get("Bcc") != "" || message.Header.Get("Cc") != "" {
		t.Fatal("recipient privacy violated")
	}
	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, message.Body))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "第一行\r\n第二行" {
		t.Fatalf("invalid mail body: %q", body)
	}
	if err = sender.SendMessage(context.Background(), "student@example.test", "标题\r\nBcc: victim@example.test", "内容"); err == nil {
		t.Fatal("header injection allowed")
	}
}
