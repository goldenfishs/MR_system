package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAssignmentResubmissionBeforeConfirmationAndReturnedRedo(t *testing.T) {
	a, admin, student, task := autoAssignmentForTest(t, true)
	answers := autoAssignmentAnswers(true)
	save := func() Submission {
		return unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 200)["item"])
	}
	finalize := func() Submission {
		return unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)["item"])
	}
	save()
	original := finalize()
	if original.Status != "pending" || original.Answers[0].Score == nil {
		t.Fatal("expected partially auto-reviewed pending version")
	}
	replacement := save()
	if replacement.Version != 2 || replacement.ID == original.ID || replacement.Score != nil || replacement.Answers[0].Score != nil || replacement.Answers[0].AutoGraded {
		t.Fatal("replacement reused history or grading")
	}
	preserved, err := readOne[Submission](a.store, "SELECT payload FROM submissions WHERE id=?", original.ID)
	if err != nil || encoded(preserved) != encoded(original) {
		t.Fatal("replacement modified original submission", err)
	}
	review := map[string]any{"questionReviews": []map[string]any{{"questionId": "short", "status": "approved", "score": 10, "feedback": "已确认"}}}
	admin.request("PATCH", "/api/submissions/"+original.ID, review, 409)
	admin.request("PATCH", "/api/submissions/"+original.ID, map[string]any{"returnForRedo": true, "feedback": "过期版本"}, 409)
	admin.request("PATCH", "/api/submissions/"+replacement.ID, map[string]any{"returnForRedo": true, "feedback": "草稿"}, 409)
	replacement = finalize()
	approved := unmarshal[Submission](t, admin.request("PATCH", "/api/submissions/"+replacement.ID, review, 200)["item"])
	if approved.Status != "approved" {
		t.Fatal("review did not confirm all answers")
	}
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 409)
	student.request("PATCH", "/api/submissions/"+approved.ID, map[string]any{"returnForRedo": true, "feedback": "自行解锁"}, 403)
	admin.request("PATCH", "/api/submissions/"+approved.ID, map[string]any{"returnForRedo": true, "feedback": "  "}, 400)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "电控", Permissions: []string{"tasks"}}})
	wrong := &testClient{t: t, handler: admin.handler}
	wrong.login("mentor")
	wrong.request("PATCH", "/api/submissions/"+approved.ID, map[string]any{"returnForRedo": true, "feedback": "其他组"}, 403)
	task, err = a.store.task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task.DueAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err = a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(task), task.ID); err != nil {
		t.Fatal(err)
	}
	returned := unmarshal[Submission](t, admin.request("PATCH", "/api/submissions/"+approved.ID, map[string]any{"returnForRedo": true, "feedback": "请补充说明后重做"}, 200)["item"])
	if returned.Status != "changes_requested" || returned.Feedback != "请补充说明后重做" || returned.Score == nil || *returned.Score != *approved.Score {
		t.Fatal("return lost previous grading or reason")
	}
	found := false
	for _, item := range unmarshal[[]Task](t, student.request("GET", "/api/tasks", nil, 200)["items"]) {
		if item.ID == task.ID {
			found = item.CanSubmit
		}
	}
	if !found {
		t.Fatal("returned overdue task was not marked editable")
	}
	redo := save()
	if !redo.RedoAllowed || redo.Version != 3 {
		t.Fatal("returned draft must retain deadline permission")
	}
	savedAgain := save()
	if savedAgain.ID != redo.ID || !savedAgain.RedoAllowed {
		t.Fatal("saving redo lost allowance")
	}
	finished := finalize()
	if finished.RedoAllowed || finished.Status != "pending" {
		t.Fatal("finalization did not consume redo allowance")
	}
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": answers}, 409)
}

func TestPlainAssignmentLockAndPracticeRetries(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	form := taskForm()
	form.Kind = "document"
	form.Category = "assignment"
	form.SubmissionType = ""
	form.Judge = nil
	task := taskForTest(t, admin, form)
	body := map[string]any{"content": "成果说明"}
	first := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submissions", body, 201)["item"])
	second := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submissions", body, 201)["item"])
	if second.Version != first.Version+1 {
		t.Fatal("pending replacement lacks new version")
	}
	grade := map[string]any{"status": "approved", "score": 90, "feedback": "确认"}
	admin.request("PATCH", "/api/submissions/"+first.ID, grade, 409)
	admin.request("PATCH", "/api/submissions/"+second.ID, grade, 200)
	student.request("POST", "/api/tasks/"+task.ID+"/submissions", body, 409)
	task, _ = a.store.task(task.ID)
	task.DueAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(task), task.ID); err != nil {
		t.Fatal(err)
	}
	admin.request("PATCH", "/api/submissions/"+second.ID, map[string]any{"returnForRedo": true, "feedback": "补交修改"}, 200)
	third := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submissions", body, 201)["item"])
	if third.Version != 3 || third.Score != nil || third.Status != "pending" {
		t.Fatal("redo failed to reset grading")
	}
	student.request("POST", "/api/tasks/"+task.ID+"/submissions", body, 409)
	// Free practice does not acquire assignment completion locks.
	practice := taskForTest(t, admin, form)
	practice.Category = "problem"
	if err := a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(practice), practice.ID); err != nil {
		t.Fatal(err)
	}
	attempt := unmarshal[Submission](t, student.request("POST", "/api/tasks/"+practice.ID+"/submissions", body, 201)["item"])
	admin.request("PATCH", "/api/submissions/"+attempt.ID, grade, 200)
	student.request("POST", "/api/tasks/"+practice.ID+"/submissions", body, 201)
}

// The same race check runs with independent PostgreSQL stores in integration CI.
func exerciseSubmissionReviewRace(t *testing.T, a *App, other *App, questions bool) {
	t.Helper()
	admin := &testClient{t: t, handler: a.Handler()}
	admin.login("admin")
	student := &testClient{t: t, handler: other.Handler()}
	student.login("student")
	form := taskForm()
	form.Kind = "document"
	form.Category = "assignment"
	form.SubmissionType = ""
	form.Judge = nil
	if questions {
		form.Questions = []TaskQuestion{{ID: "short", Title: "说明", Description: "给出说明", Type: "short_answer", MaxScore: 100}}
	}
	task := taskForTest(t, admin, form)
	var original Submission
	if questions {
		student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": []SubmissionAnswer{{QuestionID: "short", Response: "version1"}}}, 200)
		original = unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)["item"])
	} else {
		original = unmarshal[Submission](t, student.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{"content": "version1"}, 201)["item"])
	}
	type result struct {
		index, code int
		body        string
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for i, client := range []*testClient{student, admin} {
		go func(i int, c *testClient) {
			<-start
			method, path, body := "POST", "/api/tasks/"+task.ID+"/submissions", map[string]any{"content": "version2"}
			if i == 1 {
				method, path, body = "PATCH", "/api/submissions/"+original.ID, map[string]any{"status": "approved", "score": 90, "feedback": "确认"}
			}
			if questions {
				if i == 0 {
					path, body = "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": []SubmissionAnswer{{QuestionID: "short", Response: "version2"}}}
				} else {
					body = map[string]any{"questionReviews": []map[string]any{{"questionId": "short", "status": "approved", "score": 90, "feedback": "确认"}}}
				}
			}
			data, _ := json.Marshal(body)
			r := httptest.NewRequest(method, path, bytes.NewReader(data))
			r.AddCookie(c.cookie)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			c.handler.ServeHTTP(w, r)
			results <- result{i, w.Code, w.Body.String()}
		}(i, client)
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		r := <-results
		if (r.index == 0 && ((!questions && r.code == 201) || (questions && r.code == 200))) || (r.index == 1 && r.code == 200) {
			successes++
		} else if r.code == 409 {
			conflicts++
		} else {
			t.Fatalf("unexpected race response %d %s", r.code, r.body)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("race accepted stale review/replacement: success=%d conflict=%d", successes, conflicts)
	}
}
func TestAssignmentConfirmationResubmissionRace(t *testing.T) {
	a, _ := setup(t)
	exerciseSubmissionReviewRace(t, a, a, false)
	exerciseSubmissionReviewRace(t, a, a, true)
}
func exerciseSubmissionResubmissionPostgres(t *testing.T, databaseURL string, a *App) {
	t.Helper()
	s, err := openStore(databaseURL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	other := newApp(s, Config{Demo: true, WebDir: t.TempDir(), Mailer: &testMailer{}})
	exerciseSubmissionReviewRace(t, a, other, false)
	exerciseSubmissionReviewRace(t, a, other, true)
}
