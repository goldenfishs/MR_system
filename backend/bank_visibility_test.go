package main

import "testing"

func TestInternalBankScopeAttachmentsAndAllPracticeEntrypoints(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	file := uploadForTest(t, admin, "private-problem.pdf", []byte("%PDF-1.7\ninternal problem"), 201)
	form := taskForm()
	form.Category, form.Direction, form.BankVisibility = "problem", "视觉算法", "internal"
	form.SubmissionType = "code"
	form.FileIDs = []string{file.ID}
	form.Judge = &JudgeConfig{LanguageIDs: []int{71}, TestCases: []JudgeCase{{Input: "1", ExpectedOutput: "1", Hidden: true}}}
	problem := taskForTest(t, admin, form)
	if problem.BankVisibility != "internal" {
		t.Fatal("internal setting not persisted")
	}
	a.config.JudgeURL = "http://judge.invalid"
	run := JudgeRun{ID: randomID(), TaskID: problem.ID, UserID: "demo-student", Status: "completed", CreatedAt: now()}
	if err := a.store.exec("INSERT INTO judge_runs(id,task_id,user_id,status,payload) VALUES(?,?,?,?,?)", run.ID, run.TaskID, run.UserID, run.Status, encoded(run)); err != nil {
		t.Fatal(err)
	}
	assertHidden := func(c *testClient) {
		t.Helper()
		for _, task := range unmarshal[[]Task](t, c.request("GET", "/api/tasks?category=problem&bankVisibility=internal", nil, 200)["items"]) {
			if task.ID == problem.ID {
				t.Fatal("internal task exposed")
			}
		}
		c.request("GET", "/api/tasks/"+problem.ID+"/submissions", nil, 403)
		c.request("POST", "/api/tasks/"+problem.ID+"/submissions", map[string]string{"content": "attempt"}, 403)
		c.request("POST", "/api/tasks/"+problem.ID+"/judge", map[string]any{}, 403)
		c.request("POST", "/api/tasks/"+problem.ID+"/judge", map[string]any{"preview": true}, 403)
		c.request("GET", "/api/files/"+file.ID, nil, 403)
		downloadForTest(t, c, file.ID, "preview", 403)
		downloadForTest(t, c, file.ID, "download", 403)
	}
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	assertHidden(student)
	student.request("GET", "/api/judge-runs/"+run.ID, nil, 403)
	manager := &testClient{t: t, handler: admin.handler}
	manager.login("mentor")
	assertHidden(manager)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"视觉算法", []string{"knowledge"}}})
	assertHidden(manager)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"视觉算法", []string{"tasks"}}})
	items := unmarshal[[]Task](t, manager.request("GET", "/api/tasks?category=problem&bankVisibility=internal", nil, 200)["items"])
	if len(items) != 1 || items[0].ID != problem.ID {
		t.Fatal("authorized manager cannot select internal bank")
	}
	downloadForTest(t, manager, file.ID, "download", 200)
	manager.request("GET", "/api/tasks/"+problem.ID+"/submissions", nil, 200)
	// A valid scope reaches config validation; no Judge service is contacted.
	manager.request("POST", "/api/tasks/"+problem.ID+"/judge", map[string]any{"languageId": 999}, 400)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"视觉算法", []string{"knowledge"}}})
	assertHidden(manager)
}

func TestBankVisibilityValidationDefaultsAndObjectivePractice(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	legacy := taskForTest(t, admin, mechanicalProblemForm())
	if legacy.BankVisibility != "public" {
		t.Fatal("legacy bank must default to public")
	}
	updated := unmarshal[Task](t, admin.request("PATCH", "/api/tasks/"+legacy.ID, map[string]string{"bankVisibility": "internal"}, 200)["item"])
	stored, err := a.store.task(legacy.ID)
	if err != nil || stored.BankVisibility != "internal" || updated.BankVisibility != "internal" {
		t.Fatal("patch not persisted")
	}
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	student.request("POST", "/api/tasks/"+legacy.ID+"/practice", map[string]any{"answers": []string{"1"}}, 403)
	admin.request("PATCH", "/api/tasks/"+legacy.ID, map[string]string{"bankVisibility": "secret"}, 400)
	invalid := taskForm()
	invalid.BankVisibility = "internal"
	admin.request("POST", "/api/tasks", invalid, 400)
	admin.request("PATCH", "/api/tasks/"+legacy.ID, map[string]string{"bankVisibility": "public"}, 200)
	// Cross-group public practice remains available.
	student.request("POST", "/api/tasks/"+legacy.ID+"/practice", map[string]any{"answers": []string{"1"}}, 201)
}
