package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssignmentCodeQuestionJudgeAndAutomaticScore(t *testing.T) {
	a, admin := setup(t)
	fixture := &judgeFixture{token: "assignment-code-fixture"}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	t.Cleanup(server.Close)
	a.config.JudgeURL = server.URL
	a.config.JudgeToken = fixture.token

	admin.login("admin")
	team := unmarshal[Team](t, admin.request("POST", "/api/teams", map[string]any{
		"name": "电控代码作业测试队", "competitions": []string{"RoboMaster"}, "directions": []string{"电控"},
	}, 201)["item"])
	form := taskForm()
	form.Title = "电控在线代码作业"
	form.Description = "完成每道代码题并运行测试用例"
	form.Direction = "电控"
	form.Team = "RoboMaster"
	form.TeamID = team.ID
	form.Kind = "document"
	form.Category = "assignment"
	form.SubmissionType = "files"
	form.MaxScore = 40
	form.Status = "draft"
	config := &JudgeConfig{Enabled: true, LanguageIDs: []int{50}, TimeLimitSeconds: 1, TestCases: []JudgeCase{
		{Input: "42", ExpectedOutput: "42"},
		{Input: "42", ExpectedOutput: "hidden-expected", Hidden: true},
	}}
	form.Questions = []TaskQuestion{
		{ID: "code-one", Title: "代码题一", Description: "读取一行并原样输出。", Type: "code", MaxScore: 20, Judge: config},
		{ID: "code-two", Title: "代码题二", Description: "按要求编写程序。", Type: "code", MaxScore: 20, Judge: &JudgeConfig{Enabled: true, LanguageIDs: []int{50}, TimeLimitSeconds: 1, TestCases: []JudgeCase{{Input: "hello", ExpectedOutput: "hello"}}}},
	}
	task := taskForTest(t, admin, form)
	if task.Status != "draft" || len(task.Questions) != 2 || !task.Questions[0].Judge.Enabled {
		t.Fatalf("code questions were not configured in the draft: %+v", task)
	}
	admin.request("POST", "/api/tasks/"+task.ID+"/publish", nil, 200)
	addMemberToTeamForAssignmentTest(t, a, team, "demo-student", "电控")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")

	var listed Task
	for _, item := range unmarshal[[]Task](t, student.request("GET", "/api/tasks?direction=%E7%94%B5%E6%8E%A7", nil, 200)["items"]) {
		if item.ID == task.ID {
			listed = item
		}
	}
	if len(listed.Questions) != 2 || len(listed.Questions[0].Judge.TestCases) != 1 || listed.Questions[0].Judge.TotalCases != 2 {
		t.Fatalf("student should see public examples and total case count only: %+v", listed.Questions)
	}

	sourceOne := uploadForTest(t, student, "answer-one.c", []byte("#include <stdio.h>\nint main(void) { int n; if (scanf(\"%d\", &n) == 1) printf(\"%d\\n\", n); return 0; }\n"), 201)
	runOne := unmarshal[JudgeRun](t, student.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{
		"questionId": "code-one", "languageId": 50, "sourceFileId": sourceOne.ID,
	}, 202)["item"])
	runOne = waitJudge(t, student, runOne.ID)
	if runOne.Status != "completed" || runOne.Passed != 1 || runOne.Total != 2 || runOne.Score != 10 || runOne.QuestionID != "code-one" {
		t.Fatalf("unexpected per-question score: %+v", runOne)
	}
	if runOne.Results[1].Stdout != "" || runOne.Results[1].Stderr != "" {
		t.Fatal("hidden result details were exposed to the student")
	}

	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": []SubmissionAnswer{
		{QuestionID: "code-one", JudgeRunID: runOne.ID},
		{QuestionID: "code-two", JudgeRunID: runOne.ID},
	}}, 403)

	sourceTwo := uploadForTest(t, student, "answer-two.c", []byte("#include <stdio.h>\nint main(void) { int n; if (scanf(\"%d\", &n) == 1) printf(\"%d\\n\", n); return 0; }\n"), 201)
	runTwo := unmarshal[JudgeRun](t, student.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{
		"questionId": "code-two", "languageId": 50, "sourceFileId": sourceTwo.ID,
	}, 202)["item"])
	runTwo = waitJudge(t, student, runTwo.ID)
	if runTwo.Status != "completed" || runTwo.Passed != 1 || runTwo.Score != 20 {
		t.Fatalf("second question was not judged independently: %+v", runTwo)
	}

	draft := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": []SubmissionAnswer{
		{QuestionID: "code-one", JudgeRunID: runOne.ID},
		{QuestionID: "code-two", JudgeRunID: runTwo.ID},
	}}, 200)["item"])
	if draft.Score != nil || draft.Answers[0].Score != nil {
		t.Fatal("saving code answers must not award points before final submission")
	}
	final := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)["item"])
	if final.Status != "approved" || final.Score == nil || *final.Score != 30 || !final.Answers[0].AutoGraded || !final.Answers[1].AutoGraded {
		t.Fatalf("final submission was not automatically graded: %+v", final)
	}
	if *final.Answers[0].Score != 10 || *final.Answers[1].Score != 20 {
		t.Fatalf("per-question scores were not preserved: %+v", final.Answers)
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "电控", Permissions: []string{"tasks"}}})
	manager := &testClient{t: t, handler: admin.handler}
	manager.login("mentor")
	for _, source := range []FileAsset{sourceOne, sourceTwo} {
		response := downloadForTest(t, manager, source.ID, "download", 200)
		if !bytes.Contains(response.Body.Bytes(), []byte("int main")) {
			t.Fatalf("group administrator could not read submitted source %s", source.Name)
		}
	}
	request := httptest.NewRequest("GET", "/api/tasks/"+task.ID+"/submissions/export", nil)
	request.AddCookie(manager.cookie)
	response := httptest.NewRecorder()
	manager.handler.ServeHTTP(response, request)
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil || response.Code != 200 {
		t.Fatalf("code submission export failed: %d (%v)", response.Code, err)
	}
	if len(archive.File) != 2 {
		t.Fatalf("code source files missing from batch export: %+v", archive.File)
	}
}

func TestManagerAssignmentPreviewIsScopedAndNotStudentWork(t *testing.T) {
	a, admin := setup(t)
	fixture := &judgeFixture{token: "preview"}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	a.config.JudgeURL, a.config.JudgeToken = server.URL, fixture.token
	admin.login("admin")
	form := taskForm()
	form.Direction, form.Kind, form.Category, form.Status = "电控", "document", "assignment", "draft"
	form.SubmissionType = "files"
	form.Questions = []TaskQuestion{{ID: "q1", Title: "测试代码题", Description: "输出42", Type: "code", MaxScore: form.MaxScore, Judge: &JudgeConfig{Enabled: true, LanguageIDs: []int{50}, TestCases: []JudgeCase{{Input: "42", ExpectedOutput: "42"}}}}}
	task := taskForTest(t, admin, form)
	source := uploadForTest(t, admin, "test.c", []byte("int main(void){return 0;}"), 201)
	request := map[string]any{"questionId": "q1", "languageId": 50, "sourceFileId": source.ID, "preview": true}
	run := unmarshal[JudgeRun](t, admin.request("POST", "/api/tasks/"+task.ID+"/judge", request, 202)["item"])
	run = waitJudge(t, admin, run.ID)
	if !run.Preview || run.Status != "completed" {
		t.Fatalf("preview not completed: %+v", run)
	}
	items := unmarshal[[]Task](t, admin.request("GET", "/api/tasks", nil, 200)["items"])
	for _, item := range items {
		if item.ID == task.ID && (item.AttemptCount != 0 || item.SubmissionCount != 0 || item.MyAccepted) {
			t.Fatal("preview changed student statistics")
		}
	}
	if len(unmarshal[[]Submission](t, admin.request("GET", "/api/tasks/"+task.ID+"/submissions", nil, 200)["items"])) != 0 {
		t.Fatal("preview created a submission")
	}
	user, err := a.store.user(run.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.validateQuestionJudgeRun(task, task.Questions[0], user, run.ID, true); err == nil {
		t.Fatal("preview accepted as a graded answer")
	}
	admin.request("PATCH", "/api/tasks/"+task.ID, map[string]any{"title": "测试后仍能编辑"}, 200)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "电控", Permissions: []string{"tasks"}}})
	manager := &testClient{t: t, handler: admin.handler}
	manager.login("mentor")
	ownSource := uploadForTest(t, manager, "manager.c", []byte("int main(void){return 0;}"), 201)
	request["sourceFileId"] = ownSource.ID
	run = unmarshal[JudgeRun](t, manager.request("POST", "/api/tasks/"+task.ID+"/judge", request, 202)["item"])
	waitJudge(t, manager, run.ID)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "机械", Permissions: []string{"tasks"}}})
	manager.request("POST", "/api/tasks/"+task.ID+"/judge", request, 403)
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	student.request("POST", "/api/tasks/"+task.ID+"/judge", request, 403)
	admin.request("DELETE", "/api/tasks/"+task.ID, nil, 200)
}
