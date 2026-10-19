package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func mechanicalAssignmentForTest(t *testing.T, admin *testClient, team Team, files []string) Task {
	t.Helper()
	form := taskForm()
	form.Title = "机械组第一次作业"
	form.Description = "完成图纸建模并按题目上传成果"
	form.Direction = "机械"
	form.Team = "RoboMaster"
	form.TeamID = team.ID
	form.Kind = "document"
	form.Category = "assignment"
	form.SubmissionType = "files"
	form.MaxScore = 100
	form.Status = "draft"
	form.Questions = []TaskQuestion{
		{ID: "q1", Title: "第一题", Description: "按图纸完成零件建模", FileIDs: []string{files[0]}, MaxScore: 40},
		{ID: "q2", Title: "第二题", Description: "按图纸完成装配", FileIDs: []string{files[1]}, MaxScore: 60},
	}
	return taskForTest(t, admin, form)
}

func addMemberToTeamForAssignmentTest(t *testing.T, a *App, team Team, userID, direction string) {
	t.Helper()
	membership := Membership{TeamID: team.ID, UserID: userID, Direction: direction, JoinedAt: now()}
	if err := a.store.exec("INSERT INTO team_memberships(team_id,user_id,direction,joined_at,payload) VALUES(?,?,?,?,?)", membership.TeamID, membership.UserID, membership.Direction, membership.JoinedAt, encoded(membership)); err != nil {
		t.Fatal(err)
	}
}

func TestMechanicalAssignmentDraftPublishAndQuestionWorkflow(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := unmarshal[Team](t, admin.request("POST", "/api/teams", map[string]any{
		"name": "机械训练队", "competitions": []string{"RoboMaster"}, "directions": []string{"机械"},
	}, 201)["item"])
	questionFiles := []string{
		uploadForTest(t, admin, "第一题.pdf", []byte("%PDF-1.7\nquestion one"), 201).ID,
		uploadForTest(t, admin, "第二题.pdf", []byte("%PDF-1.7\nquestion two"), 201).ID,
	}
	incomplete := taskForm()
	incomplete.Title = "待补题目作业"
	incomplete.Direction = "机械"
	incomplete.Kind = "document"
	incomplete.Status = "draft"
	unfinished := taskForTest(t, admin, incomplete)
	admin.request("POST", "/api/tasks/"+unfinished.ID+"/publish", nil, 400)
	task := mechanicalAssignmentForTest(t, admin, team, questionFiles)
	if task.Status != "draft" || task.PublishedAt != "" || len(task.Questions) != 2 {
		t.Fatalf("draft task metadata was not preserved: %+v", task)
	}

	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	for _, listed := range unmarshal[[]Task](t, student.request("GET", "/api/tasks", nil, 200)["items"]) {
		if listed.ID == task.ID {
			t.Fatal("student can see a draft assignment")
		}
	}
	downloadForTest(t, student, questionFiles[0], "preview", 403)
	student.request("GET", "/api/tasks/"+task.ID+"/submissions", nil, 403)
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": []SubmissionAnswer{}}, 409)

	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "机械", Permissions: []string{"tasks"}}})
	manager := &testClient{t: t, handler: admin.handler}
	manager.login("mentor")
	managerListed := unmarshal[[]Task](t, manager.request("GET", "/api/tasks?direction=机械", nil, 200)["items"])
	foundDraft := false
	for _, listed := range managerListed {
		if listed.ID == task.ID && listed.Status == "draft" {
			foundDraft = true
		}
	}
	if !foundDraft {
		t.Fatal("authorized mechanical administrator cannot see draft")
	}
	downloadForTest(t, manager, questionFiles[0], "preview", 200)
	manager.request("POST", "/api/tasks/"+task.ID+"/publish", nil, 200)
	studentTasks := unmarshal[[]Task](t, student.request("GET", "/api/tasks?direction=机械", nil, 200)["items"])
	foundPublished := false
	for _, listed := range studentTasks {
		if listed.ID == task.ID && listed.Status == "published" && len(listed.Questions) == 2 {
			foundPublished = true
		}
	}
	if !foundPublished {
		t.Fatal("student cannot see published questions")
	}
	downloadForTest(t, student, questionFiles[0], "preview", 200)
	stored, err := a.store.task(task.ID)
	if err != nil || stored.Status != "published" || stored.PublishedAt == "" {
		t.Fatalf("publish did not set status and timestamp: %+v (%v)", stored, err)
	}
	admin.request("POST", "/api/tasks/"+task.ID+"/publish", nil, 409)

	addMemberToTeamForAssignmentTest(t, a, team, "demo-student", "机械")
	answersFiles := []string{
		uploadForTest(t, student, "answer-1-v1.zip", []byte("first answer v1"), 201).ID,
		uploadForTest(t, student, "answer-2-v1.zip", []byte("second answer v1"), 201).ID,
	}
	firstDraft := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{
		"answers": []SubmissionAnswer{{QuestionID: "q1", FileIDs: []string{answersFiles[0]}}, {QuestionID: "q2", FileIDs: []string{}}},
	}, 200)["item"])
	if firstDraft.Status != "draft" || firstDraft.Version != 1 || firstDraft.FinalizedAt != "" {
		t.Fatalf("unexpected first draft: %+v", firstDraft)
	}
	student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 400)
	secondDraft := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{
		"answers": []SubmissionAnswer{{QuestionID: "q1", FileIDs: []string{answersFiles[0]}}, {QuestionID: "q2", FileIDs: []string{answersFiles[1]}}},
	}, 200)["item"])
	if secondDraft.ID != firstDraft.ID || secondDraft.Version != 1 {
		t.Fatal("saving should update the same draft version")
	}
	finalized := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)["item"])
	if finalized.Status != "pending" || finalized.FinalizedAt == "" || len(finalized.FileIDs) != 2 {
		t.Fatalf("finalization should submit the complete assignment: %+v", finalized)
	}
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": finalized.Answers}, 409)

	partialReview := unmarshal[Submission](t, manager.request("PATCH", "/api/submissions/"+finalized.ID, map[string]any{
		"questionReviews": []map[string]any{
			{"questionId": "q1", "score": 35, "feedback": "尺寸标注有一处偏差", "status": "approved"},
		},
	}, 200)["item"])
	if partialReview.Status != "pending" || partialReview.Score == nil || *partialReview.Score != 35 {
		t.Fatalf("partial review should preserve an explicit pending state: %+v", partialReview)
	}
	requestedChanges := unmarshal[Submission](t, manager.request("PATCH", "/api/submissions/"+finalized.ID, map[string]any{
		"questionReviews": []map[string]any{
			{"questionId": "q2", "score": 45, "feedback": "请补充装配间隙说明", "status": "changes_requested"},
		},
	}, 200)["item"])
	if requestedChanges.Status != "changes_requested" || requestedChanges.Score == nil || *requestedChanges.Score != 80 || requestedChanges.Answers[0].Status != "approved" {
		t.Fatalf("question review was not aggregated: %+v", requestedChanges)
	}
	answersFilesV2 := []string{
		uploadForTest(t, student, "answer-1-v2.zip", []byte("first answer v2"), 201).ID,
		uploadForTest(t, student, "answer-2-v2.zip", []byte("second answer v2"), 201).ID,
	}
	newDraft := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{
		"answers": []SubmissionAnswer{{QuestionID: "q1", FileIDs: []string{answersFilesV2[0]}}, {QuestionID: "q2", FileIDs: []string{answersFilesV2[1]}}},
	}, 200)["item"])
	if newDraft.ID == finalized.ID || newDraft.Version != 2 || newDraft.Status != "draft" {
		t.Fatal("a changes-requested resubmission should create a new version")
	}
	student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)

	approved := unmarshal[Submission](t, manager.request("PATCH", "/api/submissions/"+newDraft.ID, map[string]any{
		"questionReviews": []map[string]any{
			{"questionId": "q1", "score": 40, "feedback": "通过", "status": "approved"},
			{"questionId": "q2", "score": 55, "feedback": "通过", "status": "approved"},
		},
	}, 200)["item"])
	if approved.Status != "approved" || approved.Score == nil || *approved.Score != 95 || approved.Answers[1].Score == nil || *approved.Answers[1].Score != 55 {
		t.Fatalf("approved question scores did not total correctly: %+v", approved)
	}
	student.request("GET", "/api/tasks/"+task.ID+"/submissions/export", nil, 403)
	request := httptest.NewRequest("GET", "/api/tasks/"+task.ID+"/submissions/export", nil)
	request.AddCookie(manager.cookie)
	response := httptest.NewRecorder()
	manager.handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("authorized export failed: %d %s", response.Code, response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, entry := range archive.File {
		file, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		data, readErr := io.ReadAll(file)
		file.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries[entry.Name] = string(data)
	}
	var firstEntry, secondEntry string
	for name, contents := range entries {
		if strings.Contains(name, "02_第二题/answer-2-v2.zip") {
			secondEntry = contents
		}
		if strings.Contains(name, "01_第一题/answer-1-v2.zip") {
			firstEntry = contents
		}
	}
	if firstEntry != "first answer v2" || secondEntry != "second answer v2" || len(entries) != 2 {
		t.Fatalf("export should include only organized latest files, got %#v", entries)
	}
}

func TestTaskQuestionValidationAndLegacyPublishedDefault(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	form := taskForm()
	form.Direction = "机械"
	form.Kind = "document"
	form.MaxScore = 100
	form.Questions = []TaskQuestion{{ID: "same", Title: "第一题", Description: "题目", MaxScore: 40}, {ID: "same", Title: "第二题", Description: "题目", MaxScore: 60}}
	admin.request("POST", "/api/tasks", form, 400)
	form.Questions = []TaskQuestion{{ID: "q1", Title: "第一题", Description: "题目", MaxScore: 40}}
	admin.request("POST", "/api/tasks", form, 400)
	form.Questions = nil
	legacy := taskForTest(t, admin, form)
	if legacy.Status != "published" {
		t.Fatalf("tasks created without a status should retain the published default: %+v", legacy)
	}
	storedLegacy := form
	storedLegacy.ID = randomID()
	storedLegacy.CreatedAt = now()
	storedLegacy.CreatedBy = "demo-admin"
	storedLegacy.Status = ""
	if err := a.store.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", storedLegacy.ID, encoded(storedLegacy)); err != nil {
		t.Fatal(err)
	}
	items := unmarshal[[]Task](t, admin.request("GET", "/api/tasks", nil, 200)["items"])
	for _, item := range items {
		if item.ID == storedLegacy.ID {
			if item.Status != "published" {
				t.Fatalf("legacy task without a status should be published: %+v", item)
			}
			return
		}
	}
	t.Fatal("legacy task was not returned")
}

func TestAssignmentChoiceAndTextAnswersAreSavedInline(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := unmarshal[Team](t, admin.request("POST", "/api/teams", map[string]any{
		"name": "题型模板测试队", "competitions": []string{"RoboMaster"}, "directions": []string{"机械"},
	}, 201)["item"])
	form := taskForm()
	form.Title = "题型模板作业"
	form.Description = "逐题完成并提交。"
	form.Direction, form.Team, form.TeamID = "机械", "RoboMaster", team.ID
	form.Kind, form.Category, form.SubmissionType = "document", "assignment", "files"
	form.Status, form.MaxScore = "draft", 6
	form.Questions = []TaskQuestion{
		{ID: "choice", Title: "单选题", Description: "选择正确答案", Type: "single_choice", Options: []string{"A项", "B项"}, MaxScore: 2},
		{ID: "blank", Title: "填空题", Description: "填写尺寸", Type: "fill_blank", Options: []string{}, MaxScore: 2},
		{ID: "short", Title: "简答题", Description: "说明设计理由", Type: "short_answer", Options: []string{}, MaxScore: 2},
	}
	task := taskForTest(t, admin, form)
	admin.request("POST", "/api/tasks/"+task.ID+"/publish", nil, 200)
	addMemberToTeamForAssignmentTest(t, a, team, "demo-student", "机械")
	student := &testClient{t: admin.t, handler: admin.handler}
	student.login("student")
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{
		"answers": []SubmissionAnswer{
			{QuestionID: "choice", Response: "5"},
			{QuestionID: "blank", Response: "10 mm"},
			{QuestionID: "short", Response: "因为装配空间受限。"},
		},
	}, 400)
	draft := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{
		"answers": []SubmissionAnswer{
			{QuestionID: "choice", Response: "1"},
			{QuestionID: "blank", Response: "10 mm"},
			{QuestionID: "short", Response: "因为装配空间受限。"},
		},
	}, 200)["item"])
	finalized := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)["item"])
	if finalized.ID != draft.ID || finalized.Answers[0].Response != "1" || finalized.Answers[1].Response != "10 mm" || finalized.Answers[2].Response != "因为装配空间受限。" {
		t.Fatalf("typed answers were not preserved through final submission: %+v", finalized.Answers)
	}
}
