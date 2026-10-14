package main

import (
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"
)

// TestPostgreSQLPersistence runs only with an explicitly configured test database.
// Every table lives in a fresh schema; existing application schemas are untouched.
func TestPostgreSQLPersistence(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		t.Fatal("TEST_DATABASE_URL must be a PostgreSQL connection URI")
	}

	control, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	control.SetMaxOpenConns(1)
	t.Cleanup(func() { control.Close() })
	if err = control.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	// randomID returns only hexadecimal characters, so this identifier cannot
	// contain SQL syntax. Never substitute an externally supplied schema name.
	schema := "mr_system_test_" + randomID()
	if _, err = control.ExecContext(t.Context(), `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := control.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`); err != nil {
			t.Errorf("remove isolated PostgreSQL test schema: %v", err)
		}
	})
	params := parsed.Query()
	params.Set("search_path", schema)
	parsed.RawQuery = params.Encode()
	isolatedURL := parsed.String()

	store, err := openStore(isolatedURL, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.db.Close() })
	if err = initialize(store, true); err != nil {
		t.Fatalf("transactional demo initialization: %v", err)
	}
	if err = initialize(store, true); err != nil {
		t.Fatalf("idempotent initialization: %v", err)
	}
	var count int
	if err = store.row("SELECT COUNT(*) FROM applicants").Scan(&count); err != nil || count != 10 {
		t.Fatalf("demo initialization duplicated or omitted applicants: count=%d err=%v", count, err)
	}

	mailer := &testMailer{}
	uploadDir := t.TempDir()
	app := newApp(store, Config{Demo: true, WebDir: t.TempDir(), Mailer: mailer, UploadDir: uploadDir})
	client := &testClient{t: t, handler: app.Handler(), mailer: mailer}
	client.login("admin")
	client.request("GET", "/api/auth/me", nil, 200)

	email := "pg-" + randomID() + "@example.test"
	password := "PostgresTestPassword!2026"
	registered := unmarshal[User](t, client.register(email, password)["user"])
	initialProfile := unmarshal[Profile](t, client.request("GET", "/api/profile", nil, 200)["profile"])
	if initialProfile.Complete || initialProfile.Name != "" || initialProfile.PhotoFileID != "" {
		t.Fatal("PostgreSQL registration unexpectedly completed personal profile")
	}
	completeProfile(t, client)
	application := unmarshal[Applicant](t, client.request("POST", "/api/applicants", map[string]any{
		"name": "数据库验证学员", "phone": "13800008888", "university": "虚构测试大学",
		"major": "自动化", "year": "大一", "team": "RoboMaster", "direction": "电控",
		"motivation": "验证 PostgreSQL 持久化与中文数据", "experience": "测试记录",
	}, 200)["item"])
	if registered.EmailVerifiedAt == "" {
		t.Fatal("PostgreSQL registration did not record verified email")
	}
	if application.UserID != registered.ID {
		t.Fatal("application did not preserve registered identity")
	}
	// Exercises PostgreSQL ON CONFLICT when the same student updates a form.
	updated := unmarshal[Applicant](t, client.request("POST", "/api/applicants", map[string]any{
		"name": "数据库验证学员", "phone": "13800008888", "university": "虚构测试大学",
		"major": "自动化", "year": "大一", "team": "RoboMaster", "direction": "电控",
		"motivation": "补充报名说明", "experience": "更新后的测试记录",
	}, 200)["item"])
	if updated.ID != application.ID || updated.Motivation != "补充报名说明" {
		t.Fatal("application upsert did not update the existing record")
	}

	client.login("admin")
	client.request("PATCH", "/api/applicants/"+application.ID, map[string]any{
		"status": "accepted", "score": 92, "notes": "PostgreSQL admission transaction verified",
	}, 200)
	task := unmarshal[Task](t, client.request("POST", "/api/tasks", map[string]any{
		"title": "数据库集成任务", "description": "验证任务提交与审核 SQL 流程",
		"team": "RoboMaster", "direction": "电控", "kind": "code", "difficulty": "入门",
		"dueAt": time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339), "maxScore": 100, "required": true,
	}, 201)["item"])
	client.request("POST", "/api/auth/login", map[string]string{"email": email, "password": password}, 200)
	first := unmarshal[Submission](t, client.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{
		"content": "第一版实现",
	}, 201)["item"])
	latest := unmarshal[Submission](t, client.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{
		"content": "第二版实现，补充验证记录", "repoUrl": "https://example.test/robot/project",
	}, 201)["item"])
	if first.Version != 1 || latest.Version != 2 {
		t.Fatal("PostgreSQL submission versions did not increment")
	}
	client.login("mentor")
	client.request("PATCH", "/api/submissions/"+first.ID, map[string]any{"status": "approved", "score": 90}, 409)
	client.request("PATCH", "/api/submissions/"+latest.ID, map[string]any{
		"status": "approved", "score": 96, "feedback": "数据库集成验证通过",
	}, 200)

	// Exercise v3 relational writes and file metadata with real PostgreSQL before
	// reopening: a published team, admission, file-backed work and award grants.
	client.login("admin")
	progress := unmarshal[[]TrainingProgress](t, client.request("GET", "/api/training-progress?direction="+url.QueryEscape("电控"), nil, 200)["items"])
	for _, p := range progress {
		if p.UserID != registered.ID {
			continue
		}
		for _, stage := range p.Stages {
			client.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{p.ID}, "stageId": stage.ID, "status": "passed", "note": "PostgreSQL 阶段持久化验证"}, 200)
		}
	}
	team := unmarshal[Team](t, client.request("POST", "/api/teams", map[string]any{
		"name": "PostgreSQL 校赛队", "competition": "ROBOCON", "directions": []string{"机械"},
	}, 201)["item"])
	certificate := uploadForTest(t, client, "pg-certificate.pdf", []byte("%PDF-1.7\ncertificate"), 201)
	material := uploadForTest(t, client, "assembly.md", []byte("机械装配任务材料"), 201)
	client.request("POST", "/api/auth/login", map[string]string{"email": email, "password": password}, 200)
	resume := uploadForTest(t, client, "pg-resume.pdf", []byte("%PDF-1.7\nresume"), 201)
	teamApplication := unmarshal[TeamApplication](t, client.request("POST", "/api/teams/"+team.ID+"/applications", map[string]string{
		"direction": "机械", "resumeFileId": resume.ID, "motivation": "验证新团队入队流程",
	}, 201)["item"])
	// Repeated application writes exercise the unique team/user upsert.
	client.request("POST", "/api/teams/"+team.ID+"/applications", map[string]string{
		"direction": "机械", "resumeFileId": resume.ID, "motivation": "补充团队报名说明",
	}, 201)
	client.login("admin")
	for i := 0; i < 2; i++ {
		client.request("PATCH", "/api/team-applications/"+teamApplication.ID, map[string]string{"status": "accepted"}, 200)
	}
	fileTask := unmarshal[Task](t, client.request("POST", "/api/tasks", map[string]any{
		"title": "PostgreSQL 文件任务", "description": "上传机械成果", "teamId": team.ID, "direction": "机械",
		"kind": "document", "difficulty": "入门", "maxScore": 100, "submissionType": "project", "fileIds": []string{material.ID},
	}, 201)["item"])
	award := unmarshal[Record](t, client.request("POST", "/api/records", map[string]any{
		"userId": registered.ID, "title": "数据库集成完成奖状", "kind": "certificate", "fileIds": []string{certificate.ID},
	}, 201)["item"])
	client.request("POST", "/api/auth/login", map[string]string{"email": email, "password": password}, 200)
	project := uploadForTest(t, client, "pg-project.zip", []byte("opaque engineering project"), 201)
	fileSubmission := unmarshal[Submission](t, client.request("POST", "/api/tasks/"+fileTask.ID+"/submissions", map[string]any{
		"content": "机械项目成果", "fileIds": []string{project.ID},
	}, 201)["item"])
	downloadForTest(t, client, material.ID, "download", 200)
	client.login("mentor")

	// Reopen the SQL connection and application instance, retaining the original
	// session cookie to prove both business records and sessions are persistent.
	if err = store.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(isolatedURL, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.db.Close() })
	if err = initialize(reopened, true); err != nil {
		t.Fatalf("initialize after reconnect: %v", err)
	}
	client.handler = newApp(reopened, Config{Demo: true, WebDir: t.TempDir(), Mailer: mailer, UploadDir: uploadDir}).Handler()
	client.request("GET", "/api/auth/me", nil, 200)
	history := unmarshal[[]Submission](t, client.request("GET", "/api/tasks/"+task.ID+"/submissions", nil, 200)["items"])
	if len(history) != 2 || history[0].ID != latest.ID || history[0].Status != "approved" || history[0].Score == nil || *history[0].Score != 96 || history[0].Feedback != "数据库集成验证通过" {
		t.Fatalf("reviewed submission history did not survive reconnect: %+v", history)
	}
	if history[1].ID != first.ID || history[1].Status != "pending" {
		t.Fatal("older immutable submission was modified")
	}
	client.request("POST", "/api/auth/login", map[string]string{"email": email, "password": password}, 200)
	me := unmarshal[User](t, client.request("GET", "/api/auth/me", nil, 200)["user"])
	if me.ID != registered.ID || me.Team != "RoboMaster" || me.Direction != "电控" || me.MemberType != "reserve" || me.EmailVerifiedAt != registered.EmailVerifiedAt {
		t.Fatal("admission transaction did not persist member identity and scope")
	}
	own := unmarshal[[]Applicant](t, client.request("GET", "/api/applicants", nil, 200)["items"])
	if len(own) != 1 || own[0].ID != application.ID || own[0].Status != "accepted" || own[0].Notes != "" || own[0].Score != nil {
		t.Fatal("application persistence or student privacy failed after reconnect")
	}
	memberships := unmarshal[[]Membership](t, client.request("GET", "/api/memberships", nil, 200)["items"])
	if len(memberships) != 1 || memberships[0].TeamID != team.ID || memberships[0].Direction != "机械" {
		t.Fatalf("PostgreSQL team membership persistence failed: %+v", memberships)
	}
	records := unmarshal[[]Record](t, client.request("GET", "/api/records", nil, 200)["items"])
	if len(records) != 1 || records[0].ID != award.ID {
		t.Fatal("PostgreSQL award record not persisted")
	}
	downloadForTest(t, client, certificate.ID, "download", 200)
	downloadForTest(t, client, material.ID, "download", 200)
	fileHistory := unmarshal[[]Submission](t, client.request("GET", "/api/tasks/"+fileTask.ID+"/submissions", nil, 200)["items"])
	if len(fileHistory) != 1 || fileHistory[0].ID != fileSubmission.ID || len(fileHistory[0].FileIDs) != 1 || fileHistory[0].FileIDs[0] != project.ID {
		t.Fatal("PostgreSQL file-backed submission not persisted")
	}
	profile := unmarshal[Profile](t, client.request("GET", "/api/profile", nil, 200)["profile"])
	if !profile.Complete || profile.StudentNumber == "" {
		t.Fatal("PostgreSQL completed profile not persisted")
	}
	downloadForTest(t, client, profile.PhotoFileID, "preview", 200)
	if err = reopened.row("SELECT COUNT(*) FROM applicants").Scan(&count); err != nil || count != 11 {
		t.Fatalf("reinitialization changed application count: count=%d err=%v", count, err)
	}
	exerciseLearningPostgres(t, client)
}
