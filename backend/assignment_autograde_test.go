package main

import (
	"strings"
	"testing"
)

func autoAssignmentForTest(t *testing.T, mixed bool) (*App, *testClient, *testClient, Task) {
	t.Helper()
	a, admin := setup(t)
	admin.login("admin")
	team := unmarshal[Team](t, admin.request("POST", "/api/teams", map[string]any{"name": "自动评分测试队", "competitions": []string{"RoboMaster"}, "directions": []string{"机械"}}, 201)["item"])
	form := taskForm()
	form.Direction, form.Kind, form.Category, form.Status, form.TeamID = "机械", "document", "assignment", "draft", team.ID
	form.MaxScore = 30
	form.Questions = []TaskQuestion{
		{ID: "single", Title: "单选", Description: "请选择", Type: "single_choice", Options: []string{"A", "B", "C"}, MaxScore: 10, AutoGrade: true, Answers: []string{"1"}, Explanation: "PRIVATE_EXPLANATION_SINGLE"},
		{ID: "multi", Title: "多选", Description: "请选择", Type: "multiple_choice", Options: []string{"A", "B", "C"}, MaxScore: 10, AutoGrade: true, Answers: []string{"2", "0"}, Explanation: "PRIVATE_EXPLANATION_MULTI"},
		{ID: "blank", Title: "填空", Description: "请输入", Type: "fill_blank", MaxScore: 10, AutoGrade: true, Answers: []string{" Result ", "Result alternative"}, Explanation: "PRIVATE_EXPLANATION_BLANK"},
	}
	if mixed {
		form.MaxScore = 40
		form.Questions = append(form.Questions, TaskQuestion{ID: "short", Title: "人工简答", Description: "说明理由", Type: "short_answer", MaxScore: 10})
	}
	task := taskForTest(t, admin, form)
	admin.request("POST", "/api/tasks/"+task.ID+"/publish", nil, 200)
	addMemberToTeamForAssignmentTest(t, a, team, "demo-student", "机械")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	return a, admin, student, task
}

func autoAssignmentAnswers(mixed bool) []SubmissionAnswer {
	answers := []SubmissionAnswer{{QuestionID: "single", Response: "1"}, {QuestionID: "multi", Response: `["0","2"]`}, {QuestionID: "blank", Response: "  Result alternative \n"}}
	if mixed {
		answers = append(answers, SubmissionAnswer{QuestionID: "short", Response: "我的说明"})
	}
	return answers
}

func TestAssignmentAutoGradeFinalizeAndPrivacy(t *testing.T) {
	a, admin, student, task := autoAssignmentForTest(t, false)
	// Read-only users and managers of another group never receive reference keys.
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "电控", Permissions: []string{"tasks"}}})
	outsider := &testClient{t: t, handler: admin.handler}
	outsider.login("mentor")
	for _, client := range []*testClient{student, outsider} {
		for _, listed := range unmarshal[[]Task](t, client.request("GET", "/api/tasks", nil, 200)["items"]) {
			if listed.ID != task.ID {
				continue
			}
			for _, q := range listed.Questions {
				if len(q.Answers) > 0 || q.Explanation != "" {
					t.Fatalf("reference answer leaked: %+v", q)
				}
			}
		}
	}
	stored, err := a.store.task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	original := encoded(stored)
	redacted := publicTask(stored, User{ID: "reader", Role: "student"})
	if strings.Contains(encoded(redacted), "PRIVATE_EXPLANATION") || strings.Contains(encoded(redacted), `"answers"`) {
		t.Fatal("publicTask leaked answer configuration")
	}
	if encoded(stored) != original {
		t.Fatal("publicTask mutated shared stored question slice")
	}
	if len(publicTask(stored, User{ID: "admin", Role: "admin"}).Questions[0].Answers) != 1 {
		t.Fatal("administrator lost answers")
	}
	student.request("PATCH", "/api/tasks/"+task.ID, map[string]any{"questions": task.Questions}, 403)
	outsider.request("PATCH", "/api/tasks/"+task.ID, map[string]any{"questions": task.Questions}, 403)
	answers := autoAssignmentAnswers(false)
	forged := append([]SubmissionAnswer{}, answers...)
	forged[0].AutoGraded = true
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": forged}, 400)
	draftBody := student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 200)
	draft := unmarshal[Submission](t, draftBody["item"])
	if draft.Score != nil || draft.Status != "draft" {
		t.Fatalf("draft graded prematurely: %+v", draft)
	}
	for _, answer := range draft.Answers {
		if answer.Score != nil || answer.Status != "" || answer.Feedback != "" || answer.AutoGraded {
			t.Fatal("draft exposes grading")
		}
	}
	if strings.Contains(encoded(draftBody), "PRIVATE_EXPLANATION") {
		t.Fatal("draft leaked explanation")
	}
	finalBody := student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)
	final := unmarshal[Submission](t, finalBody["item"])
	if final.Status != "approved" || final.Score == nil || *final.Score != 30 || final.ReviewedAt == "" {
		t.Fatalf("auto grading incomplete: %+v", final)
	}
	for _, answer := range final.Answers {
		if answer.Score == nil || *answer.Score != 10 || answer.Status != "approved" || !answer.AutoGraded {
			t.Fatalf("incorrect graded answer: %+v", answer)
		}
	}
	if strings.Contains(encoded(finalBody), "PRIVATE_EXPLANATION") {
		t.Fatal("finalize leaked explanation")
	}
	submissions := student.request("GET", "/api/tasks/"+task.ID+"/submissions", nil, 200)
	if strings.Contains(encoded(submissions), "PRIVATE_EXPLANATION") {
		t.Fatal("submission list leaked reference answer")
	}
	student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 409)
	outsider.request("PATCH", "/api/submissions/"+final.ID, map[string]any{"questionReviews": []map[string]any{{"questionId": "single", "score": 0, "status": "approved"}}}, 403)
	corrected := unmarshal[Submission](t, admin.request("PATCH", "/api/submissions/"+final.ID, map[string]any{"questionReviews": []map[string]any{{"questionId": "single", "score": 7, "status": "approved", "feedback": "人工复核"}}}, 200)["item"])
	if corrected.Score == nil || *corrected.Score != 27 || corrected.Answers[0].AutoGraded || corrected.Answers[0].Feedback != "人工复核" || corrected.Status != "approved" {
		t.Fatalf("manual override failed: %+v", corrected)
	}
}

func TestAssignmentAutoGradeMixedWrongAndBlank(t *testing.T) {
	_, admin, student, task := autoAssignmentForTest(t, true)
	answers := autoAssignmentAnswers(true)
	answers[0].Response = "0"     // wrong single
	answers[1].Response = `["2"]` // partial credit must not be awarded
	answers[2].Response = "   "
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 200)
	student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 400)
	answers[2].Response = "result" // case matters
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 200)
	final := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)["item"])
	if final.Score == nil || *final.Score != 0 || final.Status != "pending" || final.ReviewedAt != "" {
		t.Fatalf("mixed grading state incorrect: %+v", final)
	}
	for _, answer := range final.Answers[:3] {
		if answer.Score == nil || *answer.Score != 0 || answer.Status != "approved" || !answer.AutoGraded {
			t.Fatalf("wrong objective answer not marked reviewed: %+v", answer)
		}
	}
	if final.Answers[3].Score != nil || final.Answers[3].Status != "" {
		t.Fatal("manual answer graded automatically")
	}
	final = unmarshal[Submission](t, admin.request("PATCH", "/api/submissions/"+final.ID, map[string]any{"questionReviews": []map[string]any{{"questionId": "short", "score": 8, "status": "approved"}}}, 200)["item"])
	if final.Score == nil || *final.Score != 8 || final.Status != "approved" {
		t.Fatalf("mixed final total incorrect: %+v", final)
	}
}

func TestAssignmentAutoGradeValidation(t *testing.T) {
	cases := []struct {
		name string
		q    TaskQuestion
	}{
		{"missing", TaskQuestion{Type: "single_choice", Options: []string{"A", "B"}, AutoGrade: true}},
		{"out of bounds", TaskQuestion{Type: "single_choice", Options: []string{"A", "B"}, AutoGrade: true, Answers: []string{"2"}}},
		{"non canonical index", TaskQuestion{Type: "single_choice", Options: []string{"A", "B"}, AutoGrade: true, Answers: []string{"01"}}},
		{"single multiple answers", TaskQuestion{Type: "single_choice", Options: []string{"A", "B"}, AutoGrade: true, Answers: []string{"0", "1"}}},
		{"duplicate choice", TaskQuestion{Type: "multiple_choice", Options: []string{"A", "B"}, AutoGrade: true, Answers: []string{"0", "0"}}},
		{"duplicate trimmed blank", TaskQuestion{Type: "fill_blank", AutoGrade: true, Answers: []string{"yes", " yes "}}},
		{"blank answer", TaskQuestion{Type: "fill_blank", AutoGrade: true, Answers: []string{" \n"}}},
		{"long answer", TaskQuestion{Type: "fill_blank", AutoGrade: true, Answers: []string{strings.Repeat("x", 20001)}}},
		{"long explanation", TaskQuestion{Type: "fill_blank", Explanation: strings.Repeat("x", 20001)}},
		{"too many alternatives", TaskQuestion{Type: "fill_blank", Answers: make([]string, 21)}},
		{"short auto", TaskQuestion{Type: "short_answer", AutoGrade: true, Answers: []string{"text"}}},
		{"drawing auto", TaskQuestion{Type: "drawing", AutoGrade: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := tc.q
			q.ID = "q"
			q.Title = "题目"
			q.Description = "说明"
			q.MaxScore = 10
			task := Task{Category: "assignment", Status: "draft", MaxScore: 10, Questions: []TaskQuestion{q}}
			if validateTaskQuestions(&task) == nil {
				t.Fatal("invalid grading accepted")
			}
		})
	}
}

func TestAssignmentLegacyAndDisabledAutoGradeRemainManual(t *testing.T) {
	for _, q := range []TaskQuestion{
		{ID: "q", Title: "旧题", Type: "single_choice", Options: []string{"A", "B"}, MaxScore: 10},
		{ID: "q", Title: "手动题", Type: "single_choice", Options: []string{"A", "B"}, MaxScore: 10, Answers: []string{"1"}},
	} {
		task := Task{Category: "assignment", MaxScore: 10, Questions: []TaskQuestion{q}}
		if err := validateTaskQuestions(&task); err != nil {
			t.Fatal(err)
		}
		submission := Submission{Status: "pending", Answers: []SubmissionAnswer{{QuestionID: "q", Response: "1"}}}
		autoGradeTaskSubmission(task, &submission)
		if submission.Score != nil || submission.Status != "pending" || submission.Answers[0].Score != nil {
			t.Fatal("legacy/manual question was automatically graded")
		}
	}
}

func TestAssignmentAutoGradeWrongCompleteIsReviewed(t *testing.T) {
	task := Task{Questions: []TaskQuestion{{ID: "q", Type: "multiple_choice", AutoGrade: true, Answers: []string{"0", "2"}, MaxScore: 10}}}
	for _, response := range []string{`["0"]`, `["0","1","2"]`, `["0","0"]`} {
		submission := Submission{Status: "pending", Answers: []SubmissionAnswer{{QuestionID: "q", Response: response}}}
		autoGradeTaskSubmission(task, &submission)
		if submission.Status != "approved" || submission.Score == nil || *submission.Score != 0 {
			t.Fatalf("wrong response %q must be reviewed with zero points: %+v", response, submission)
		}
	}
}

func TestAssignmentShortAnswerAllowsOwnedFileOnly(t *testing.T) {
	_, admin, student, task := autoAssignmentForTest(t, true)
	answers := autoAssignmentAnswers(true)
	answers[3].Response = "   "
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 200)
	student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 400)
	foreign := uploadForTest(t, admin, "foreign.c", []byte("int main(void){return 0;}"), 201)
	answers[3].FileIDs = []string{foreign.ID}
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 403)
	own := uploadForTest(t, student, "answer.c", []byte("int main(void){return 0;}"), 201)
	answers[3].FileIDs = []string{own.ID}
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 200)
	final := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)["item"])
	if final.Status != "pending" || final.Answers[3].Response != "" || len(final.Answers[3].FileIDs) != 1 || final.Answers[3].FileIDs[0] != own.ID {
		t.Fatalf("file-only short answer was not accepted for manual review: %+v", final)
	}
	// A file cannot replace text needed by fill-blank grading.
	fillTask := Task{Questions: []TaskQuestion{{ID: "blank", Type: "fill_blank"}}}
	if _, _, err := normalizeSubmissionAnswers(fillTask, []SubmissionAnswer{{QuestionID: "blank", FileIDs: []string{own.ID}}}, true); err == nil {
		t.Fatal("file-only fill-blank answer was accepted")
	}
}
