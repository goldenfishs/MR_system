package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func mechanicalProblemForm() Task {
	task := taskForm()
	task.Title, task.Description, task.Direction = "齿轮传动分析", "选择齿轮传动比。", "机械"
	task.Category, task.Kind, task.SubmissionType = "problem", "document", ""
	task.MechanicalProblem = &MechanicalProblem{Type: "single_choice", Options: []string{"2", "3", "4"}, Answers: []string{"1"}, Explanation: "传动比等于从动齿数除以主动齿数。"}
	return task
}

func mechanicalLearnerForTest(t *testing.T, a *App, admin *testClient) (*testClient, Team, TrainingProgress) {
	t.Helper()
	team := trainingTeamForTest(t, admin, "机械")
	user, application := trainingApplicantForTest(t, a, admin, team, "机械", "mechanical-problems@test.local")
	progress := admitForTest(t, a, admin, application)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	return student, team, progress
}

func mechanicalListedForTest(t *testing.T, c *testClient, id string) Task {
	t.Helper()
	for _, task := range unmarshal[[]Task](t, c.request("GET", "/api/tasks?category=problem&direction="+url.QueryEscape("机械"), nil, 200)["items"]) {
		if task.ID == id {
			return task
		}
	}
	t.Fatal("mechanical problem missing from list")
	return Task{}
}

func TestMechanicalProblemValidationAndEditing(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	created := taskForTest(t, admin, mechanicalProblemForm())
	updated := unmarshal[Task](t, admin.request("PATCH", "/api/tasks/"+created.ID, map[string]any{
		"mechanicalProblem": MechanicalProblem{Type: "fill_blank", Answers: []string{" 3 ", " N "}},
	}, 200)["item"])
	if updated.MechanicalProblem.Type != "fill_blank" || updated.MechanicalProblem.BlankCount != 2 || updated.MechanicalProblem.Answers[0] != "3" {
		t.Fatalf("mechanical problem update not normalized: %+v", updated)
	}
	admin.request("PATCH", "/api/tasks/"+created.ID, map[string]any{"mechanicalProblem": nil}, 400)
	admin.request("PATCH", "/api/tasks/"+created.ID, map[string]any{"mechanicalProblem": map[string]any{"type": "fill_blank", "answers": []string{"3"}, "unknown": true}}, 400)
	for _, scenario := range []string{"missing", "unknown-type", "code", "file-submission", "judge", "operations", "assignment", "options-short", "options-long", "options-duplicate", "answer-index", "single-many", "multiple-duplicate", "fill-empty", "fill-too-many", "answer-long", "objective-file", "drawing-answer"} {
		t.Run(scenario, func(t *testing.T) {
			form := mechanicalProblemForm()
			p := form.MechanicalProblem
			switch scenario {
			case "missing":
				form.MechanicalProblem = nil
			case "unknown-type":
				p.Type = "essay"
			case "code":
				form.Kind = "code"
			case "file-submission":
				form.SubmissionType = "files"
			case "judge":
				form.Judge = &JudgeConfig{LanguageIDs: []int{54}, TestCases: []JudgeCase{{Input: "1", ExpectedOutput: "1"}}}
			case "operations":
				form.Direction = "运营"
			case "assignment":
				form.Category = "assignment"
			case "options-short":
				p.Options = []string{"one"}
			case "options-long":
				p.Options = make([]string, 21)
			case "options-duplicate":
				p.Options = []string{"one", " one "}
			case "answer-index":
				p.Answers = []string{"03"}
			case "single-many":
				p.Answers = []string{"0", "1"}
			case "multiple-duplicate":
				p.Type, p.Answers = "multiple_choice", []string{"0", "0"}
			case "fill-empty":
				p.Type, p.Options, p.Answers = "fill_blank", nil, []string{" "}
			case "fill-too-many":
				p.Type, p.Options, p.Answers = "fill_blank", nil, make([]string, 11)
			case "answer-long":
				p.Type, p.Options, p.Answers = "fill_blank", nil, []string{strings.Repeat("a", 1001)}
			case "objective-file":
				p.AnswerFileIDs = []string{"anything"}
			case "drawing-answer":
				p.Type, p.Options = "drawing", nil
			}
			admin.request("POST", "/api/tasks", form, 400)
		})
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "机械", Permissions: []string{"tasks"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	taskForTest(t, staff, mechanicalProblemForm())
	staff.request("POST", "/api/tasks", taskForm(), 403)
}

func TestMechanicalObjectivePracticeAndAnswerPrivacy(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student, team, progress := mechanicalLearnerForTest(t, a, admin)
	form := mechanicalProblemForm()
	form.TeamID = team.ID
	problem := taskForTest(t, admin, form)
	listed := mechanicalListedForTest(t, student, problem.ID)
	if !listed.CanSubmit || len(listed.MechanicalProblem.Answers) != 0 || listed.MechanicalProblem.Explanation != "" {
		t.Fatalf("unanswered question leaked answers or failed enrollment: %+v", listed)
	}
	outsider := &testClient{t: t, handler: admin.handler}
	outsider.login("student")
	if mechanicalListedForTest(t, outsider, problem.ID).CanSubmit {
		t.Fatal("another group may not answer mechanical questions")
	}
	path := "/api/tasks/" + problem.ID + "/practice"
	outsider.request("POST", path, map[string]any{"answers": []string{"1"}}, 403)
	student.request("POST", "/api/tasks/"+problem.ID+"/submissions", map[string]string{"content": "bypass"}, 400)
	student.request("POST", path, map[string]any{"answers": []string{"99"}}, 400)
	student.request("POST", path, map[string]any{"answers": []string{"1"}, "score": 100}, 400)
	for _, answer := range []string{"0", "1", "0"} {
		response := student.request("POST", path, map[string]any{"answers": []string{answer}}, 201)
		result := unmarshal[MechanicalPracticeResult](t, response["result"])
		submission := unmarshal[Submission](t, response["item"])
		correct := answer == "1"
		if result.Correct != correct || len(result.Answers) != 1 || result.Answers[0] != "1" || result.Explanation == "" || submission.ReviewedBy != "system" || submission.Score == nil || *submission.Score != result.Score {
			t.Fatalf("incorrect automatic grading response: %+v / %+v", result, submission)
		}
		if correct && (result.Score != 100 || submission.Status != "approved") || !correct && (result.Score != 0 || submission.Status != "changes_requested") {
			t.Fatal("automatic score/status incorrect")
		}
		admin.request("PATCH", "/api/submissions/"+submission.ID, map[string]any{"status": "approved", "score": 100}, 400)
	}
	listed = mechanicalListedForTest(t, student, problem.ID)
	if listed.AttemptCount != 3 || listed.AcceptedCount != 1 || !listed.MyAccepted || len(listed.MechanicalProblem.Answers) != 0 || listed.MechanicalProblem.Explanation != "" {
		t.Fatalf("practice statistics/answer redaction incorrect: %+v", listed)
	}
	otherView := mechanicalListedForTest(t, outsider, problem.ID)
	if otherView.MyAccepted || otherView.MySubmission != nil || otherView.AttemptCount != 3 {
		t.Fatal("public aggregate included another learner's answer")
	}
	if submissions := unmarshal[[]Submission](t, outsider.request("GET", "/api/tasks/"+problem.ID+"/submissions", nil, 200)["items"]); len(submissions) != 0 {
		t.Fatal("another group's user can read private answers")
	}
	admin.request("PATCH", "/api/tasks/"+problem.ID, map[string]string{"description": "change grading"}, 409)
	admin.request("DELETE", "/api/tasks/"+problem.ID, nil, 409)
	trainingDecisionForTest(admin, progress, "foundation", "eliminated", 200)
	student.request("POST", path, map[string]any{"answers": []string{"1"}}, 403)
}

func TestMechanicalMultipleChoiceAndFillBlankGrading(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student, team, _ := mechanicalLearnerForTest(t, a, admin)
	for _, test := range []struct {
		kind            string
		expected, input []string
		correct         bool
	}{
		{"multiple_choice", []string{"0", "2"}, []string{"2", "0"}, true},
		{"multiple_choice", []string{"0", "2"}, []string{"0"}, false},
		{"multiple_choice", []string{"0", "2"}, []string{"0", "1", "2"}, false},
		{"fill_blank", []string{"3", "N"}, []string{" 3 ", " N "}, true},
		{"fill_blank", []string{"3", "N"}, []string{"3", "n"}, false},
		{"fill_blank", []string{"3", "N"}, []string{"N", "3"}, false},
	} {
		form := mechanicalProblemForm()
		form.TeamID, form.MechanicalProblem.Type, form.MechanicalProblem.Answers = team.ID, test.kind, test.expected
		if test.kind == "fill_blank" {
			form.MechanicalProblem.Options = nil
		}
		problem := taskForTest(t, admin, form)
		if test.kind == "fill_blank" && mechanicalListedForTest(t, student, problem.ID).MechanicalProblem.BlankCount != 2 {
			t.Fatal("blank count hidden with answers")
		}
		result := unmarshal[MechanicalPracticeResult](t, student.request("POST", "/api/tasks/"+problem.ID+"/practice", map[string]any{"answers": test.input}, 201)["result"])
		if result.Correct != test.correct {
			t.Fatalf("%s grading failed: %+v", test.kind, result)
		}
	}
}

func TestMechanicalDrawingFilesAndScheduledPractice(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student, team, _ := mechanicalLearnerForTest(t, a, admin)
	drawing := uploadForTest(t, admin, "drawing.pdf", []byte("%PDF-1.7\ndrawing"), 201)
	answer := uploadForTest(t, admin, "reference.step", []byte("step reference"), 201)
	form := mechanicalProblemForm()
	form.TeamID, form.FileIDs = team.ID, []string{drawing.ID}
	form.StartsAt, form.DueAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339), time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339)
	form.MechanicalProblem = &MechanicalProblem{Type: "drawing", Explanation: "供自由练习参考", AnswerFileIDs: []string{answer.ID}}
	problem := taskForTest(t, admin, form)
	listed := mechanicalListedForTest(t, student, problem.ID)
	if !listed.Locked || listed.MechanicalProblem != nil || len(listed.FileIDs) != 0 {
		t.Fatal("scheduled drawing leaked files")
	}
	downloadForTest(t, student, answer.ID, "download", 403)
	downloadForTest(t, student, drawing.ID, "download", 403)
	admin.request("PATCH", "/api/tasks/"+problem.ID, map[string]string{"startsAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}, 200)
	listed = mechanicalListedForTest(t, student, problem.ID)
	if listed.CanSubmit || listed.MechanicalProblem.Explanation == "" || len(listed.MechanicalProblem.AnswerFileIDs) != 1 {
		t.Fatal("drawing should expose optional reference without accepting submissions")
	}
	downloadForTest(t, student, answer.ID, "download", 200)
	downloadForTest(t, student, drawing.ID, "download", 200)
	otherGroup := &testClient{t: t, handler: admin.handler}
	otherGroup.login("student")
	downloadForTest(t, otherGroup, answer.ID, "download", 200)
	if mechanicalListedForTest(t, otherGroup, problem.ID).CanSubmit {
		t.Fatal("drawing practice must not be submittable even from another group")
	}
	student.request("POST", "/api/tasks/"+problem.ID+"/practice", map[string]any{"answers": []string{"a"}}, 400)
	student.request("POST", "/api/tasks/"+problem.ID+"/submissions", map[string]string{"content": "done"}, 400)
	form.MechanicalProblem = &MechanicalProblem{Type: "drawing"}
	form.StartsAt, form.DueAt = "", ""
	taskForTest(t, admin, form)
	form.FileIDs = nil
	admin.request("POST", "/api/tasks", form, 400)
	objective := mechanicalProblemForm()
	objective.TeamID, objective.StartsAt, objective.DueAt = team.ID, time.Now().Add(time.Hour).UTC().Format(time.RFC3339), time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339)
	timed := taskForTest(t, admin, objective)
	student.request("POST", "/api/tasks/"+timed.ID+"/practice", map[string]any{"answers": []string{"1"}}, 409)
	if mechanicalListedForTest(t, student, timed.ID).MechanicalProblem != nil {
		t.Fatal("scheduled objective leaked options or answers")
	}
	admin.request("PATCH", "/api/tasks/"+timed.ID, map[string]string{"startsAt": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339), "dueAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}, 200)
	student.request("POST", "/api/tasks/"+timed.ID+"/practice", map[string]any{"answers": []string{"1"}}, 409)
}

func TestMechanicalReferenceFilesRejectPersonalDocuments(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	private := uploadForTest(t, admin, "private.pdf", []byte("%PDF-1.7\nprivate"), 201)
	drawing := uploadForTest(t, admin, "drawing.pdf", []byte("%PDF-1.7\ndrawing"), 201)
	if err := a.store.saveProfile(Profile{UserID: "demo-admin", PhotoFileID: private.ID}); err != nil {
		t.Fatal(err)
	}
	form := mechanicalProblemForm()
	form.FileIDs = []string{drawing.ID}
	form.MechanicalProblem = &MechanicalProblem{Type: "drawing", AnswerFileIDs: []string{private.ID}}
	admin.request("POST", "/api/tasks", form, 400)
	form.FileIDs, form.MechanicalProblem.AnswerFileIDs = []string{private.ID}, nil
	admin.request("POST", "/api/tasks", form, 400)
	if err := a.store.saveProfile(Profile{UserID: "demo-admin", AvatarFileID: private.ID}); err != nil {
		t.Fatal(err)
	}
	admin.request("POST", "/api/tasks", form, 400)
	if err := a.store.saveProfile(Profile{UserID: "demo-admin"}); err != nil {
		t.Fatal(err)
	}
	team := trainingTeamForTest(t, admin, "机械")
	application := TeamApplication{ID: randomID(), TeamID: team.ID, UserID: "demo-student", ResumeFileID: private.ID}
	if err := a.store.exec("INSERT INTO team_applications(id,team_id,user_id,payload) VALUES(?,?,?,?)", application.ID, application.TeamID, application.UserID, encoded(application)); err != nil {
		t.Fatal(err)
	}
	admin.request("POST", "/api/tasks", form, 400)
}
