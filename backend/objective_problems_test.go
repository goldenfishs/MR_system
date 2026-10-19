package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func problemListedForTest(t *testing.T, c *testClient, direction, id string) Task {
	t.Helper()
	for _, task := range unmarshal[[]Task](t, c.request("GET", "/api/tasks?category=problem&direction="+url.QueryEscape(direction), nil, 200)["items"]) {
		if task.ID == id {
			return task
		}
	}
	t.Fatal("problem missing from bank")
	return Task{}
}

func TestTechnicalBanksObjectivePracticeAndPermissions(t *testing.T) {
	for _, direction := range []string{"电控", "视觉算法", "硬件"} {
		t.Run(direction, func(t *testing.T) {
			a, admin := setup(t)
			admin.login("admin")
			team := trainingTeamForTest(t, admin, direction)
			user, application := trainingApplicantForTest(t, a, admin, team, direction, "objective@test.local")
			progress := admitForTest(t, a, admin, application)
			student := &testClient{t: t, handler: admin.handler}
			student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
			outsider, _, _ := mechanicalLearnerForTest(t, a, admin)
			var lastProblem Task
			for _, kind := range []string{"single_choice", "multiple_choice", "fill_blank"} {
				form := mechanicalProblemForm()
				form.Direction, form.TeamID = direction, team.ID
				form.MechanicalProblem.Type = kind
				correct, incorrect := []string{"1"}, []string{"0"}
				if kind == "multiple_choice" {
					form.MechanicalProblem.Answers = []string{"0", "2"}
					correct, incorrect = []string{"2", "0"}, []string{"0", "1"}
				} else if kind == "fill_blank" {
					form.MechanicalProblem.Options = nil
					form.MechanicalProblem.Answers = []string{"GPIO", "3.3"}
					correct, incorrect = []string{" GPIO ", " 3.3 "}, []string{"gpio", "3.3"}
				}
				problem := taskForTest(t, admin, form)
				lastProblem = problem
				listed := problemListedForTest(t, student, direction, problem.ID)
				if !listed.CanSubmit || listed.MechanicalProblem == nil || len(listed.MechanicalProblem.Answers) != 0 || listed.MechanicalProblem.Explanation != "" {
					t.Fatalf("unanswered question leaked answers or cannot be practiced: %+v", listed)
				}
				if kind == "fill_blank" && listed.MechanicalProblem.BlankCount != 2 {
					t.Fatal("fill blank question omitted answer fields")
				}
				path := "/api/tasks/" + problem.ID + "/practice"
				// Verify cross-group practice after checking answer privacy.
				for index, input := range [][]string{incorrect, correct} {
					response := student.request("POST", path, map[string]any{"answers": input}, 201)
					result := unmarshal[MechanicalPracticeResult](t, response["result"])
					submission := unmarshal[Submission](t, response["item"])
					if result.Correct != (index == 1) || result.Explanation == "" || len(result.Answers) != len(correct) || submission.ReviewedBy != "system" || submission.Score == nil || *submission.Score != result.Score {
						t.Fatalf("incorrect automatic grading: %+v / %+v", result, submission)
					}
					if index == 1 && result.Score != form.MaxScore || index == 0 && result.Score != 0 {
						t.Fatalf("incorrect score: %+v", result)
					}
					admin.request("PATCH", "/api/submissions/"+submission.ID, map[string]any{"status": "approved", "score": 100}, 400)
				}
				listed = problemListedForTest(t, student, direction, problem.ID)
				if listed.AttemptCount != 2 || listed.AcceptedCount != 1 || !listed.MyAccepted || len(listed.MechanicalProblem.Answers) != 0 {
					t.Fatalf("statistics or redaction incorrect: %+v", listed)
				}
				otherView := problemListedForTest(t, outsider, direction, problem.ID)
				if !otherView.CanSubmit || otherView.MyAccepted || otherView.MySubmission != nil || len(otherView.MechanicalProblem.Answers) != 0 {
					t.Fatal("another group received submission rights or another user's answers")
				}
				if items := unmarshal[[]Submission](t, outsider.request("GET", "/api/tasks/"+problem.ID+"/submissions", nil, 200)["items"]); len(items) != 0 {
					t.Fatal("another learner's submitted answers leaked")
				}
				outsider.request("POST", path, map[string]any{"answers": correct}, 201)
				student.request("POST", "/api/tasks/"+problem.ID+"/submissions", map[string]string{"content": "bypass objective grading"}, 400)
				admin.request("PATCH", "/api/tasks/"+problem.ID, map[string]string{"description": "change grading"}, 409)
			}
			grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: direction, Permissions: []string{"tasks"}}})
			staff := &testClient{t: t, handler: admin.handler}
			staff.login("mentor")
			if len(problemListedForTest(t, staff, direction, lastProblem.ID).MechanicalProblem.Answers) == 0 {
				t.Fatal("authorized staff cannot inspect answers")
			}
			staff.request("POST", "/api/tasks", mechanicalProblemForm(), 403)
			trainingDecisionForTest(admin, progress, "foundation", "eliminated", 200)
			student.request("POST", "/api/tasks/"+lastProblem.ID+"/practice", map[string]any{"answers": []string{"GPIO", "3.3"}}, 403)
		})
	}
}

func TestTechnicalBanksRejectMixedQuestionModesAndKeepCode(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	for _, direction := range []string{"电控", "视觉算法", "硬件"} {
		for _, mode := range []string{"drawing", "code", "judge", "files", "project", "assignment", "answers-missing"} {
			form := mechanicalProblemForm()
			form.Direction = direction
			switch mode {
			case "drawing":
				form.MechanicalProblem.Type = "drawing"
			case "code":
				form.Kind = "code"
			case "judge":
				form.Judge = &JudgeConfig{LanguageIDs: []int{54}, TestCases: []JudgeCase{{Input: "1 2", ExpectedOutput: "3"}}}
			case "files", "project":
				form.SubmissionType = mode
			case "assignment":
				form.Category = mode
			case "answers-missing":
				form.MechanicalProblem = nil
			}
			admin.request("POST", "/api/tasks", form, 400)
		}
		form := mechanicalProblemForm()
		form.Direction = direction
		problem := taskForTest(t, admin, form)
		code := unmarshal[Task](t, admin.request("PATCH", "/api/tasks/"+problem.ID, map[string]any{
			"mechanicalProblem": nil, "kind": "code", "submissionType": "code",
			"judge": JudgeConfig{LanguageIDs: []int{54, 71}, TestCases: []JudgeCase{{Input: "1 2", ExpectedOutput: "3"}, {Input: "0 -1", ExpectedOutput: "-1", Hidden: true}}},
		}, 200)["item"])
		if code.MechanicalProblem != nil || code.Judge == nil || code.Judge.TotalCases != 2 {
			t.Fatal("code problem compatibility lost")
		}
		admin.request("POST", "/api/tasks/"+problem.ID+"/practice", map[string]any{"answers": []string{"1"}}, 400)
		restored := unmarshal[Task](t, admin.request("PATCH", "/api/tasks/"+problem.ID, map[string]any{
			"judge": nil, "kind": "quiz", "submissionType": "", "mechanicalProblem": form.MechanicalProblem,
		}, 200)["item"])
		if restored.MechanicalProblem == nil || restored.Judge != nil {
			t.Fatal("cannot change an unused code problem to objective practice")
		}
	}
	form := mechanicalProblemForm()
	form.Direction = "运营"
	admin.request("POST", "/api/tasks", form, 400)
}

func TestTechnicalObjectiveScheduleAndPersonalFilePrivacy(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student") // Existing electrical team member.
	file := uploadForTest(t, admin, "circuit.pdf", []byte("%PDF-1.7\ncircuit"), 201)
	form := mechanicalProblemForm()
	form.Direction, form.FileIDs = "电控", []string{file.ID}
	form.IllustrationURL, form.IllustrationAlt = "/question-bank/resistor.svg", "电阻示意图"
	form.StartsAt, form.DueAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339), time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339)
	problem := taskForTest(t, admin, form)
	locked := problemListedForTest(t, student, form.Direction, problem.ID)
	if !locked.Locked || locked.MechanicalProblem != nil || len(locked.FileIDs) != 0 || locked.IllustrationURL != "" || locked.IllustrationAlt != "" {
		t.Fatal("scheduled question leaked its contents")
	}
	student.request("POST", "/api/tasks/"+problem.ID+"/practice", map[string]any{"answers": []string{"1"}}, 409)
	downloadForTest(t, student, file.ID, "download", 403)
	admin.request("PATCH", "/api/tasks/"+problem.ID, map[string]string{"startsAt": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339), "dueAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}, 200)
	student.request("POST", "/api/tasks/"+problem.ID+"/practice", map[string]any{"answers": []string{"1"}}, 409)
	if err := a.store.saveProfile(Profile{UserID: "demo-admin", PhotoFileID: file.ID}); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"电控", "视觉算法", "硬件"} {
		form.Direction, form.StartsAt, form.DueAt = direction, "", ""
		admin.request("POST", "/api/tasks", form, 400)
	}
}

func TestQuestionIllustrationsRejectExternalAndUnsafePaths(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	for _, path := range []string{"https://example.test/question.svg", "//example.test/question.svg", "/question-bank/../secret.svg", "/question-bank/%2e%2e.svg", "/question-bank/.svg", "/question-bank/x.svg?tracking=1", "/question-bank/x.svg#fragment", "/question-bank/x/y.svg", " /question-bank/x.svg", "/question-bank/图.svg", "/question-bank/" + strings.Repeat("x", 101) + ".svg"} {
		form := mechanicalProblemForm()
		form.IllustrationURL, form.IllustrationAlt = path, "题目配图"
		admin.request("POST", "/api/tasks", form, 400)
	}
	for _, alt := range []string{"", " \t\n", strings.Repeat("x", 1001)} {
		form := mechanicalProblemForm()
		form.IllustrationURL, form.IllustrationAlt = "/question-bank/resistor.svg", alt
		admin.request("POST", "/api/tasks", form, 400)
	}
	orphan := mechanicalProblemForm()
	orphan.IllustrationAlt = "无图片的说明"
	admin.request("POST", "/api/tasks", orphan, 400)
	valid := mechanicalProblemForm()
	taskForTest(t, admin, valid) // Both optional illustration fields may be empty.
	valid.IllustrationURL, valid.IllustrationAlt = "/question-bank/rc-circuit-1.svg", "RC 电路"
	taskForTest(t, admin, valid)
}

func TestQuestionIllustrationEditingPreservesImageAndRejectsUnsafePath(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	form := mechanicalProblemForm()
	form.Direction = "硬件"
	form.IllustrationURL, form.IllustrationAlt = "/question-bank/resistor.svg", "电阻示意图"
	created := taskForTest(t, admin, form)
	path := "/api/tasks/" + created.ID
	// Editing the title without image fields must keep the existing illustration.
	updated := unmarshal[Task](t, admin.request("PATCH", path, map[string]any{"title": "电阻识别（更新）"}, 200)["item"])
	if updated.Title != "电阻识别（更新）" || updated.IllustrationURL != form.IllustrationURL || updated.IllustrationAlt != form.IllustrationAlt || updated.MechanicalProblem == nil {
		t.Fatal("editing an illustrated objective question lost its image or question")
	}
	// The editor submits both illustration fields with the other editable content.
	updated = unmarshal[Task](t, admin.request("PATCH", path, map[string]any{
		"description": "观察图中的元件，选择正确答案。", "illustrationUrl": form.IllustrationURL, "illustrationAlt": "电阻元件示意图",
	}, 200)["item"])
	if updated.IllustrationURL != form.IllustrationURL || updated.IllustrationAlt != "电阻元件示意图" {
		t.Fatal("explicit image fields were not accepted by question editing")
	}
	for _, imagePath := range []string{"https://example.test/image.svg", "/question-bank/../secret.svg"} {
		admin.request("PATCH", path, map[string]string{"illustrationUrl": imagePath, "illustrationAlt": "非法图片"}, 400)
	}
	stored, err := a.store.task(created.ID)
	if err != nil || stored.IllustrationURL != updated.IllustrationURL || stored.IllustrationAlt != updated.IllustrationAlt {
		t.Fatal("rejected image edit changed the saved question")
	}
}
