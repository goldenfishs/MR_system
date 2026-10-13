package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLearningKnowledgePrivacyAndRevocation(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	pdf := uploadForTest(t, c, "reference.pdf", []byte("%PDF-1.7\nlearning"), 201)
	c.login("student")
	downloadForTest(t, c, pdf.ID, "download", 403)
	c.request("POST", "/api/knowledge-base", map[string]any{"title": "资料", "kind": "pdf", "fileIds": []string{pdf.ID}}, 403)
	c.login("admin")
	resource := unmarshal[KnowledgeResource](t, c.request("POST", "/api/knowledge-base", map[string]any{"title": "机械建模参考", "description": "PDF资料", "direction": "机械", "kind": "pdf", "fileIds": []string{pdf.ID}}, 201)["resource"])
	c.login("student")
	items := unmarshal[[]KnowledgeResource](t, c.request("GET", "/api/knowledge-base?direction="+url.QueryEscape("机械"), nil, 200)["resources"])
	if len(items) != 1 || items[0].ID != resource.ID {
		t.Fatal("knowledge resource missing")
	}
	if len(unmarshal[[]KnowledgeResource](t, c.request("GET", "/api/knowledge-base?direction="+url.QueryEscape("运营"), nil, 200)["resources"])) != 0 {
		t.Fatal("knowledge direction filter failed")
	}
	downloadForTest(t, c, pdf.ID, "download", 200)
	preview := downloadForTest(t, c, pdf.ID, "preview", 200)
	if preview.Header().Get("Content-Type") != "application/pdf" || preview.Header().Get("X-Frame-Options") != "SAMEORIGIN" || preview.Header().Get("Content-Security-Policy") != "frame-ancestors 'self'" {
		t.Fatal("PDF preview headers missing")
	}
	download := downloadForTest(t, c, pdf.ID, "download", 200)
	if download.Header().Get("X-Frame-Options") != "DENY" || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("file download should remain an attachment and deny embedding")
	}
	c.request("PATCH", "/api/knowledge-base/"+resource.ID, map[string]string{"title": "forged"}, 403)
	c.login("mentor")
	c.request("DELETE", "/api/knowledge-base/"+resource.ID, nil, 403)
	c.login("admin")
	c.request("PATCH", "/api/knowledge-base/"+resource.ID, map[string]string{"title": "更新后的机械资料"}, 200)
	c.request("DELETE", "/api/knowledge-base/"+resource.ID, nil, 200)
	c.login("student")
	downloadForTest(t, c, pdf.ID, "download", 403)
	c.cookie = nil
	c.request("GET", "/api/knowledge-base", nil, 401)
	// Revoking the resource never deletes the owner's original file.
	if _, err := a.fileAsset(pdf.ID); err != nil {
		t.Fatal(err)
	}
}
func TestLearningDiscussionPrivateResolutionAndPublication(t *testing.T) {
	_, c := setup(t)
	c.login("student")
	discussion := unmarshal[Discussion](t, c.request("POST", "/api/discussions", map[string]string{"title": "机械装配疑问", "content": "如何检查轴承配合？", "direction": "机械"}, 201)["discussion"])
	c.request("PATCH", "/api/discussions/"+discussion.ID, map[string]bool{"published": true}, 403)
	c.login("interviewer")
	if len(unmarshal[[]Discussion](t, c.request("GET", "/api/discussions", nil, 200)["discussions"])) != 0 {
		t.Fatal("private question leaked to interviewer")
	}
	c.request("POST", "/api/discussions/"+discussion.ID+"/replies", map[string]string{"content": "forged"}, 404)
	c.login("mentor")
	c.request("PATCH", "/api/discussions/"+discussion.ID, map[string]string{"status": "resolved"}, 409)
	c.request("POST", "/api/discussions/"+discussion.ID+"/replies", map[string]string{"content": "先查公差带，并结合载荷和装配方式选择。"}, 201)
	c.request("PATCH", "/api/discussions/"+discussion.ID, map[string]string{"status": "resolved"}, 200)
	c.request("PATCH", "/api/discussions/"+discussion.ID, map[string]bool{"published": true}, 403)
	c.login("admin")
	c.request("PATCH", "/api/discussions/"+discussion.ID, map[string]bool{"published": true}, 200)
	c.login("interviewer")
	public := unmarshal[[]Discussion](t, c.request("GET", "/api/discussions?published=true", nil, 200)["discussions"])
	if len(public) != 1 || !public[0].Published || len(public[0].Replies) != 1 {
		t.Fatal("curated answer missing")
	}
	c.login("student")
	c.request("POST", "/api/discussions/"+discussion.ID+"/replies", map[string]string{"content": "unreviewed update"}, 409)
	c.login("admin")
	c.request("PATCH", "/api/discussions/"+discussion.ID, map[string]bool{"published": false}, 200)
	c.login("student")
	c.request("POST", "/api/discussions/"+discussion.ID+"/replies", map[string]string{"content": "谢谢，补充新的实验条件。"}, 201)
	c.login("interviewer")
	if len(unmarshal[[]Discussion](t, c.request("GET", "/api/discussions", nil, 200)["discussions"])) != 0 {
		t.Fatal("revoked publication remains visible")
	}
}
func TestLearningScheduledExamContentFileAndSubmissionRelease(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	file := uploadForTest(t, c, "secret-exam.pdf", []byte("%PDF-1.7\nexam-secret"), 201)
	form := taskForm()
	form.Title = "定时考试"
	form.Category = "exam"
	form.Kind = "document"
	form.SubmissionType = "files"
	form.Description = "SECRET_QUESTION_BODY"
	form.FileIDs = []string{file.ID}
	form.ResourcesURL = "https://example.test/secret-exam"
	form.StartsAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	form.DueAt = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	exam := taskForTest(t, c, form)
	c.login("student")
	listed := unmarshal[[]Task](t, c.request("GET", "/api/tasks?category=exam", nil, 200)["items"])
	if len(listed) != 1 || !listed[0].Locked || listed[0].Description != "" || listed[0].ResourcesURL != "" || len(listed[0].FileIDs) != 0 || listed[0].Judge != nil {
		t.Fatalf("unreleased exam content leaked: %+v", listed)
	}
	if len(unmarshal[[]Task](t, c.request("GET", "/api/tasks?search=SECRET_QUESTION_BODY", nil, 200)["items"])) != 0 {
		t.Fatal("search reveals locked exam body")
	}
	c.request("GET", "/api/files/"+file.ID, nil, 403)
	downloadForTest(t, c, file.ID, "download", 403)
	downloadForTest(t, c, file.ID, "preview", 403)
	c.request("POST", "/api/tasks/"+exam.ID+"/submissions", map[string]string{"content": "early"}, 409)
	c.request("GET", "/api/workspace-summary", nil, 200)
	c.login("interviewer")
	listed = unmarshal[[]Task](t, c.request("GET", "/api/tasks?category=exam", nil, 200)["items"])
	if len(listed) != 1 || !listed[0].Locked {
		t.Fatal("interviewer saw unreleased examination")
	}
	downloadForTest(t, c, file.ID, "download", 403)
	c.login("admin")
	c.request("PATCH", "/api/tasks/"+exam.ID, map[string]string{"startsAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}, 200)
	c.login("student")
	listed = unmarshal[[]Task](t, c.request("GET", "/api/tasks?category=exam", nil, 200)["items"])
	if len(listed) != 1 || listed[0].Locked || listed[0].Description != form.Description || len(listed[0].FileIDs) != 1 {
		t.Fatal("exam did not release at scheduled start")
	}
	downloadForTest(t, c, file.ID, "preview", 200)
	answer := uploadForTest(t, c, "answer.zip", []byte("project archive"), 201)
	c.request("POST", "/api/tasks/"+exam.ID+"/submissions", map[string]any{"content": "已完成", "fileIds": []string{answer.ID}}, 201)
	c.login("admin")
	c.request("PATCH", "/api/tasks/"+exam.ID, map[string]string{"description": "change after answer"}, 409)
	c.request("DELETE", "/api/tasks/"+exam.ID, nil, 409)
	current, err := a.store.task(exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.DueAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if err = a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(current), current.ID); err != nil {
		t.Fatal(err)
	}
	c.login("student")
	c.request("POST", "/api/tasks/"+exam.ID+"/submissions", map[string]any{"content": "late", "fileIds": []string{answer.ID}}, 409)
}
func TestLearningProblemStatisticsFiltersAndValidation(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	problem := taskForm()
	problem.Category = "problem"
	problem.Tags = []string{"C++", "基础"}
	problem.SubmissionType = "code"
	problem.Judge = &JudgeConfig{LanguageIDs: []int{54}, TestCases: []JudgeCase{{Input: "1 2", ExpectedOutput: "3"}, {Input: "10 20", ExpectedOutput: "30", Hidden: true}}}
	created := taskForTest(t, c, problem)
	problem.Direction = "运营"
	c.request("POST", "/api/tasks", problem, 400)
	problem.Direction = "机械"
	problem.Category = "assignment"
	c.request("POST", "/api/tasks", problem, 400)
	invalidExam := taskForm()
	invalidExam.Category = "exam"
	c.request("POST", "/api/tasks", invalidExam, 400)
	for i, state := range []string{"completed", "completed", "error", "queued"} {
		run := JudgeRun{ID: randomID(), TaskID: created.ID, UserID: "demo-student", Status: state, Total: 2, Passed: 1}
		if i == 0 {
			run.Passed = 2
		}
		if err := a.store.exec("INSERT INTO judge_runs(id,task_id,user_id,status,payload) VALUES(?,?,?,?,?)", run.ID, run.TaskID, run.UserID, run.Status, encoded(run)); err != nil {
			t.Fatal(err)
		}
	}
	c.login("student")
	items := unmarshal[[]Task](t, c.request("GET", "/api/tasks?category=problem&tag="+url.QueryEscape("C++")+"&direction="+url.QueryEscape("电控"), nil, 200)["items"])
	if len(items) != 1 || items[0].AttemptCount != 2 || items[0].AcceptedCount != 1 || !items[0].MyAccepted || len(items[0].Judge.TestCases) != 1 {
		t.Fatalf("problem statistics or hidden test handling: %+v", items)
	}
	if len(unmarshal[[]Task](t, c.request("GET", "/api/tasks?category=problem&tag=missing", nil, 200)["items"])) != 0 {
		t.Fatal("tag filtering failed")
	}
	c.login("admin")
	c.request("DELETE", "/api/tasks/"+created.ID, nil, 409)
}
func TestLearningUpdateTaskCanClearJudgeConfig(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	form := taskForm()
	form.SubmissionType = "code"
	form.Judge = &JudgeConfig{LanguageIDs: []int{54}, TestCases: []JudgeCase{{Input: "1 2", ExpectedOutput: "3"}}}
	task := taskForTest(t, c, form)

	kept := unmarshal[Task](t, c.request("PATCH", "/api/tasks/"+task.ID, map[string]any{"title": "更新后的代码任务"}, 200)["item"])
	if kept.Judge == nil || !kept.Judge.Enabled {
		t.Fatal("omitting judge should preserve the existing configuration")
	}

	updated := unmarshal[Task](t, c.request("PATCH", "/api/tasks/"+task.ID, map[string]any{
		"kind": "document", "submissionType": "files", "judge": nil,
	}, 200)["item"])
	if updated.Judge != nil || updated.Kind != "document" || updated.SubmissionType != "files" {
		t.Fatalf("explicit null should remove automatic judging: %+v", updated)
	}
	saved, err := a.store.task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Judge != nil || saved.Kind != "document" || saved.SubmissionType != "files" {
		t.Fatalf("updated task was not persisted: %+v", saved)
	}
}
func TestLearningBrandingMultisportAndMigration(t *testing.T) {
	a, c := setup(t)
	cfg := c.request("GET", "/api/public/config", nil, 200)
	if unmarshal[string](t, cfg["brandName"]) != "QUT机器人战队" || len(unmarshal[[]string](t, cfg["competitions"])) != 2 {
		t.Fatal("QUT public brand defaults missing")
	}
	c.login("admin")
	team := unmarshal[Team](t, c.request("POST", "/api/teams", map[string]any{"name": "沧溟（MOVE）", "competitions": []string{"RoboCon", "RoboMaster"}, "directions": []string{"机械", "电控"}}, 201)["item"])
	if len(team.Competitions) != 2 || team.Competition != "RoboCon" {
		t.Fatal("one team should support both competitions")
	}
	c.request("PATCH", "/api/settings", map[string]string{"brandName": "自定义品牌", "organization": "自定义组织", "logoUrl": "https://example.test/logo.png", "teamName": "别名", "secondaryTeamName": ""}, 200)
	c.request("PATCH", "/api/settings", map[string]string{"logoUrl": "javascript:alert(1)"}, 400)
	c.request("PATCH", "/api/settings", map[string]string{"logoUrl": "/api/files/private/preview"}, 400)
	if err := initialize(a.store, true); err != nil {
		t.Fatal(err)
	}
	config := c.request("GET", "/api/public/config", nil, 200)
	if unmarshal[string](t, config["brandName"]) != "自定义品牌" || unmarshal[string](t, config["organization"]) != "自定义组织" || unmarshal[string](t, config["secondaryTeamName"]) != "" {
		t.Fatal("startup overwrote admin branding")
	}
	// Reconstruct a pre-v4 settings record while preserving a custom organization.
	if err := a.store.exec("DELETE FROM metadata WHERE key='qut_branding_v4'"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.saveSettings(Settings{Organization: "保留既有组织", Season: "2027"}); err != nil {
		t.Fatal(err)
	}
	if err := initialize(a.store, true); err != nil {
		t.Fatal(err)
	}
	saved, err := a.store.settings()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Organization != "保留既有组织" || saved.TeamName != "沧溟" {
		t.Fatal("brand migration lost custom organization")
	}
	if err := a.store.exec("DELETE FROM metadata WHERE key='qut_branding_v4'"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.saveSettings(Settings{Organization: "竞跃机器人实验室", Season: "2027"}); err != nil {
		t.Fatal(err)
	}
	if err := initialize(a.store, true); err != nil {
		t.Fatal(err)
	}
	saved, err = a.store.settings()
	if err != nil || saved.Organization != "青岛理工大学机器人战队" {
		t.Fatal("known legacy organization was not migrated")
	}
	c.login("student")
	c.request("PATCH", "/api/settings", map[string]string{"teamName": "forged"}, 403)
}
func TestLearningPrivatePreviewsAndVideoRanges(t *testing.T) {
	_, c := setup(t)
	c.login("admin")
	photo := testPhoto()
	data, err := base64.StdEncoding.DecodeString(photo.ContentBase64)
	if err != nil {
		t.Fatal(err)
	}
	png := uploadForTest(t, c, "honor.png", data, 201)
	c.request("POST", "/api/records", map[string]any{"userId": "demo-student", "title": "入队通过", "kind": "certificate", "fileIds": []string{png.ID}}, 201)
	// Minimal MP4 file-type box. This only verifies storage/content-type/range serving, not video decoding.
	videoData := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	video := uploadForTest(t, c, "lesson.mp4", videoData, 201)
	c.request("POST", "/api/knowledge-base", map[string]any{"title": "视频参考", "kind": "video", "fileIds": []string{video.ID}}, 201)
	uploadForTest(t, c, "fake.mp4", []byte("not a video"), 400)
	c.login("student")
	downloadForTest(t, c, png.ID, "preview", 200)
	request := httptest.NewRequest("GET", "/api/files/"+video.ID+"/preview", nil)
	request.AddCookie(c.cookie)
	request.Header.Set("Range", "bytes=0-7")
	recorder := httptest.NewRecorder()
	c.handler.ServeHTTP(recorder, request)
	if recorder.Code != 206 || recorder.Body.Len() != 8 || recorder.Header().Get("Content-Type") != "video/mp4" || recorder.Header().Get("X-Frame-Options") != "SAMEORIGIN" || !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("private video range response failed")
	}
	c.login("interviewer")
	downloadForTest(t, c, png.ID, "preview", 403)
}

// Reused against real PostgreSQL in CI; intentionally exercises relational storage
// and JSON payload persistence without relying on SQLite-specific SQL.
func exerciseLearningPostgres(t *testing.T, c *testClient) {
	t.Helper()
	c.login("admin")
	resource := unmarshal[KnowledgeResource](t, c.request("POST", "/api/knowledge-base", map[string]any{"title": "PG知识资料", "kind": "video", "direction": "机械", "url": "https://example.test/lesson.mp4"}, 201)["resource"])
	c.request("PATCH", "/api/knowledge-base/"+resource.ID, map[string]string{"description": "数据库持久化"}, 200)
	c.login("student")
	question := unmarshal[Discussion](t, c.request("POST", "/api/discussions", map[string]string{"title": "PG测试问题", "content": "知识库资料如何使用？", "direction": "机械"}, 201)["discussion"])
	c.login("admin")
	c.request("POST", "/api/discussions/"+question.ID+"/replies", map[string]string{"content": "通过资料页打开。"}, 201)
	c.request("PATCH", "/api/discussions/"+question.ID, map[string]any{"status": "resolved", "published": true}, 200)
	output := c.request("GET", "/api/discussions?published=true", nil, 200)
	raw, _ := json.Marshal(output)
	if !strings.Contains(string(raw), question.ID) {
		t.Fatal("PostgreSQL published question missing")
	}
	c.request("DELETE", "/api/knowledge-base/"+resource.ID, nil, 200)
}

func TestBrandLogoUploadIsSanitizedAndOnlyCurrentLogoPublic(t *testing.T) {
	_, c := setup(t)
	c.login("student")
	c.request("POST", "/api/settings/logo", testPhoto(), 403)
	c.login("admin")
	photo := testPhoto()
	raw, err := base64.StdEncoding.DecodeString(photo.ContentBase64)
	if err != nil {
		t.Fatal(err)
	}
	photo.ContentBase64 = base64.StdEncoding.EncodeToString(append(raw, []byte("PRIVATE_IMAGE_METADATA")...))
	settings := unmarshal[Settings](t, c.request("POST", "/api/settings/logo", photo, 200)["settings"])
	savedCookie := c.cookie
	c.cookie = nil
	request := httptest.NewRequest("GET", settings.LogoURL, nil)
	response := httptest.NewRecorder()
	c.handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("Content-Type") != "image/png" || strings.Contains(response.Body.String(), "PRIVATE_IMAGE_METADATA") {
		t.Fatal("public logo was unavailable or not sanitized")
	}
	c.cookie = savedCookie
	private := uploadForTest(t, c, "private-photo.png", raw, 201)
	c.request("PATCH", "/api/settings", map[string]string{"logoUrl": "/api/public/logo/" + private.ID}, 400)
	current := unmarshal[Settings](t, c.request("POST", "/api/settings/logo", testPhoto(), 200)["settings"])
	c.cookie = nil
	c.request("GET", settings.LogoURL, nil, 404)
	c.request("GET", "/api/public/logo/"+private.ID, nil, 404)
	request = httptest.NewRequest("GET", current.LogoURL, nil)
	response = httptest.NewRecorder()
	c.handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("replacement logo not public")
	}
}
