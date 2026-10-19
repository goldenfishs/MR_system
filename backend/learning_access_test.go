package main

import (
	"testing"
)

func TestLearningAccessUsesCurrentLegacyAdmissionAndStaffRoles(t *testing.T) {
	a, c := setup(t)
	c.request("GET", "/api/learning-access", nil, 401)
	for _, role := range []string{"admin", "mentor", "interviewer"} {
		c.login(role)
		if !unmarshal[bool](t, c.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
			t.Fatalf("%s should retain learning access", role)
		}
	}
	c.login("student")
	applicant, err := readOne[Applicant](a.store, "SELECT payload FROM applicants WHERE user_id=?", "demo-student")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"submitted", "screening", "interview", "rejected", "accepted", "rejected"} {
		applicant.Status = status
		if err = a.store.exec("UPDATE applicants SET payload=? WHERE id=?", encoded(applicant), applicant.ID); err != nil {
			t.Fatal(err)
		}
		allowed := unmarshal[bool](t, c.request("GET", "/api/learning-access", nil, 200)["allowed"])
		if allowed != (status == "accepted") {
			t.Fatalf("status %s: got allowed=%t", status, allowed)
		}
		if status != "accepted" && len(unmarshal[[]Task](t, c.request("GET", "/api/tasks", nil, 200)["items"])) != 0 {
			t.Fatalf("status %s leaked training tasks", status)
		}
	}
	// Questions remain available to prospective members before admission.
	question := unmarshal[Discussion](t, c.request("POST", "/api/discussions", map[string]string{"title": "入队前的问题", "content": "电控组需要哪些基础？", "direction": "电控"}, 201)["discussion"])
	c.login("mentor")
	c.request("POST", "/api/discussions/"+question.ID+"/replies", map[string]string{"content": "可以先学习 C 语言和基本电路知识。"}, 201)
	c.request("PATCH", "/api/discussions/"+question.ID, map[string]string{"status": "resolved"}, 200)
	c.login("admin")
	c.request("PATCH", "/api/discussions/"+question.ID, map[string]bool{"published": true}, 200)
	c.login("student")
	if len(unmarshal[[]Discussion](t, c.request("GET", "/api/discussions?published=true", nil, 200)["discussions"])) != 1 {
		t.Fatal("prospective member cannot see the curated answer")
	}
}

func TestLearningTeamAdmissionGatesContentAndRevokesWithoutNewLogin(t *testing.T) {
	a, c := setup(t)
	// The modern enrollment flow must work without the retired applicant model.
	if err := a.store.exec("DELETE FROM applicants WHERE user_id=?", "demo-student"); err != nil {
		t.Fatal(err)
	}
	c.login("admin")
	team := unmarshal[Team](t, c.request("POST", "/api/teams", map[string]any{"name": "培养权限测试战队", "competitions": []string{"RoboCon", "RoboMaster"}, "directions": []string{"电控"}, "status": "open"}, 201)["item"])
	attachment := uploadForTest(t, c, "assignment.pdf", []byte("%PDF-1.7\nassignment"), 201)
	knowledgeFile := uploadForTest(t, c, "knowledge.pdf", []byte("%PDF-1.7\nknowledge"), 201)
	certificate := uploadForTest(t, c, "certificate.pdf", []byte("%PDF-1.7\ncertificate"), 201)
	form := taskForm()
	form.Title, form.Direction, form.Team = "所有入队成员的作业", "", ""
	form.Kind, form.SubmissionType = "document", "files"
	form.FileIDs = []string{attachment.ID}
	task := taskForTest(t, c, form)
	resource := unmarshal[KnowledgeResource](t, c.request("POST", "/api/knowledge-base", map[string]any{"title": "电控资料", "direction": "电控", "kind": "pdf", "fileIds": []string{knowledgeFile.ID}}, 201)["resource"])
	record := unmarshal[Record](t, c.request("POST", "/api/records", map[string]any{"userId": "demo-student", "title": "本人证书", "kind": "certificate", "fileIds": []string{certificate.ID}}, 201)["item"])
	run := JudgeRun{ID: randomID(), TaskID: task.ID, UserID: "demo-student", Status: "completed", CreatedAt: now()}
	if err := a.store.exec("INSERT INTO judge_runs(id,task_id,user_id,status,payload) VALUES(?,?,?,?,?)", run.ID, run.TaskID, run.UserID, run.Status, encoded(run)); err != nil {
		t.Fatal(err)
	}
	a.config.JudgeURL = "http://judge.invalid"
	c.login("student")
	resume := uploadForTest(t, c, "resume.pdf", []byte("%PDF-1.7\nresume"), 201)
	artifact := uploadForTest(t, c, "project.zip", []byte("project"), 201)
	completeProfile(t, c)
	application := unmarshal[TeamApplication](t, c.request("POST", "/api/teams/"+team.ID+"/applications", map[string]string{"direction": "电控", "resumeFileId": resume.ID}, 201)["item"])

	checkBlocked := func() {
		t.Helper()
		if unmarshal[bool](t, c.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
			t.Fatal("unapproved member has learning access")
		}
		if len(unmarshal[[]Task](t, c.request("GET", "/api/tasks", nil, 200)["items"])) != 0 {
			t.Fatal("unapproved member saw tasks")
		}
		c.request("GET", "/api/knowledge-base", nil, 403)
		for _, file := range []FileAsset{attachment, knowledgeFile} {
			c.request("GET", "/api/files/"+file.ID, nil, 403)
			downloadForTest(t, c, file.ID, "download", 403)
			downloadForTest(t, c, file.ID, "preview", 403)
		}
		c.request("GET", "/api/tasks/"+task.ID+"/submissions", nil, 403)
		c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"fileIds": []string{artifact.ID}}, 403)
		c.request("GET", "/api/judge-runs/"+run.ID, nil, 403)
		c.request("GET", "/api/judge/config", nil, 403)
		c.request("POST", "/api/tasks/"+task.ID+"/judge", map[string]any{}, 403)
		stats := unmarshal[map[string]int](t, c.request("GET", "/api/workspace-summary", nil, 200)["stats"])
		if stats["activeTasks"] != 0 || stats["pendingReviews"] != 0 {
			t.Fatal("workspace exposes blocked training activity")
		}
		// Admission controls never remove personal files or individually issued honors.
		downloadForTest(t, c, resume.ID, "download", 200)
		downloadForTest(t, c, artifact.ID, "download", 200)
		downloadForTest(t, c, certificate.ID, "preview", 200)
		items := unmarshal[[]Record](t, c.request("GET", "/api/records", nil, 200)["items"])
		if len(items) != 1 || items[0].ID != record.ID {
			t.Fatal("personal honor was hidden with learning access")
		}
	}
	checkBlocked()
	staff := &testClient{t: t, handler: c.handler}
	staff.login("admin")
	staff.request("POST", "/api/interviews/"+application.ID+"/decision", map[string]string{"status": "passed"}, 200)
	if !unmarshal[bool](t, c.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("approved modern member did not gain learning access")
	}
	resources := unmarshal[[]KnowledgeResource](t, c.request("GET", "/api/knowledge-base", nil, 200)["resources"])
	if len(resources) != 1 || resources[0].ID != resource.ID {
		t.Fatal("approved member cannot see the knowledge base")
	}
	visible := false
	for _, item := range unmarshal[[]Task](t, c.request("GET", "/api/tasks", nil, 200)["items"]) {
		if item.ID == task.ID {
			visible = true
		}
	}
	if !visible {
		t.Fatal("approved member cannot see the training task")
	}
	for _, file := range []FileAsset{attachment, knowledgeFile} {
		downloadForTest(t, c, file.ID, "preview", 200)
	}
	c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"fileIds": []string{artifact.ID}}, 201)
	c.request("GET", "/api/tasks/"+task.ID+"/submissions", nil, 200)
	c.request("GET", "/api/judge-runs/"+run.ID, nil, 200)
	staff.request("PATCH", "/api/team-applications/"+application.ID, map[string]string{"status": "rejected", "feedback": "撤销录取"}, 200)
	checkBlocked()
}
