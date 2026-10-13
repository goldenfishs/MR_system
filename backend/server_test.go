package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testClient struct {
	t          *testing.T
	handler    http.Handler
	cookie     *http.Cookie
	mailer     *testMailer
	lastHeader http.Header
}

func setup(t *testing.T) (*App, *testClient) {
	t.Helper()
	s, err := openStore("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	if err = initialize(s, true); err != nil {
		t.Fatal(err)
	}
	mailer := &testMailer{}
	a := newApp(s, Config{Demo: true, WebDir: t.TempDir(), Mailer: mailer})
	return a, &testClient{t: t, handler: a.Handler(), mailer: mailer}
}
func (c *testClient) request(method, path string, body any, status int) map[string]json.RawMessage {
	c.t.Helper()
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, &b)
	r.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	c.lastHeader = w.Header().Clone()
	if w.Code != status {
		c.t.Fatalf("%s %s: got %d, expected %d: %s", method, path, w.Code, status, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "robot_hub_session" {
			c.cookie = cookie
		}
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		c.t.Fatal(err)
	}
	return result
}
func (c *testClient) login(role string) {
	password := map[string]string{"admin": "DemoAdmin!2026", "mentor": "DemoMentor!2026", "student": "DemoStudent!2026", "interviewer": "DemoInterview!2026"}[role]
	c.request("POST", "/api/auth/login", map[string]string{"email": role + "@demo.local", "password": password}, 200)
}
func unmarshal[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestAuthenticationAndOrigin(t *testing.T) {
	_, c := setup(t)
	c.request("GET", "/api/auth/me", nil, 401)
	c.request("POST", "/api/auth/login", map[string]string{"email": "admin@demo.local", "password": "wrong"}, 401)
	c.login("admin")
	if !c.cookie.HttpOnly || c.cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("session must be HttpOnly and SameSite=Lax")
	}
	c.request("GET", "/api/auth/me", nil, 200)
	r := httptest.NewRequest("PATCH", "/api/settings", strings.NewReader(`{"organization":"Hijacked"}`))
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(c.cookie)
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin mutation allowed: %d", w.Code)
	}
	c.request("POST", "/api/auth/logout", nil, 200)
	c.request("GET", "/api/auth/me", nil, 401)
}
func TestRegistrationCannotEscalate(t *testing.T) {
	_, c := setup(t)
	c.request("POST", "/api/auth/register", map[string]string{"name": "新人", "email": "new@example.com", "password": "LongPassword!123", "role": "admin"}, 400)
	result := c.register("新人", "new@example.com", "LongPassword!123")
	u := unmarshal[User](t, result["user"])
	if u.Role != "freshman" {
		t.Fatal("registration elevated role")
	}
	c.request("PATCH", "/api/settings", map[string]bool{"applicationOpen": false}, 403)
	c.request("GET", "/api/members", nil, 403)
	items := unmarshal[[]Applicant](t, c.request("GET", "/api/applicants", nil, 200)["items"])
	if len(items) != 0 {
		t.Fatal("new user can see other applications")
	}
}
func TestApplicantPrivacyAndAssignment(t *testing.T) {
	_, c := setup(t)
	c.login("student")
	items := unmarshal[[]Applicant](t, c.request("GET", "/api/applicants", nil, 200)["items"])
	if len(items) != 1 || items[0].UserID != "demo-student" || items[0].Notes != "" || items[0].Score != nil {
		t.Fatalf("student received private application data: %+v", items)
	}
	id := items[0].ID
	c.request("PATCH", "/api/applicants/"+id, map[string]string{"status": "accepted"}, 403)
	c.login("interviewer")
	assigned := unmarshal[[]Applicant](t, c.request("GET", "/api/applicants", nil, 200)["items"])
	if len(assigned) != 3 {
		t.Fatalf("expected 3 mechanical applicants, got %d", len(assigned))
	}
	c.request("PATCH", "/api/applicants/"+id, map[string]int{"score": 99}, 403)
	c.request("PATCH", "/api/applicants/"+assigned[0].ID, map[string]string{"status": "accepted"}, 200)
	c.request("PATCH", "/api/applicants/"+assigned[0].ID, map[string]any{"score": 89, "notes": "表达清晰"}, 200)
	c.request("PATCH", "/api/applicants/"+assigned[0].ID, map[string]int{"score": 101}, 400)
}
func TestApplicationIdentityAndDeadline(t *testing.T) {
	a, c := setup(t)
	c.register("测试学员", "learner@example.com", "LongPassword!123")
	me := unmarshal[User](t, c.request("GET", "/api/auth/me", nil, 200)["user"])
	form := map[string]any{"userId": "demo-admin", "name": "测试学员", "email": "admin@demo.local", "phone": "13800009999", "university": "测试大学", "major": "自动化", "year": "大一", "team": "RoboMaster", "direction": "电控", "motivation": "学习控制系统", "status": "accepted", "notes": "forged", "score": 100}
	ap := unmarshal[Applicant](t, c.request("POST", "/api/applicants", form, 200)["item"])
	if ap.UserID != me.ID || ap.Email != me.Email || ap.Status != "submitted" || ap.Score != nil || ap.Notes != "" {
		t.Fatalf("application identity/state not server controlled: %+v", ap)
	}
	form["motivation"] = "补充说明"
	updated := unmarshal[Applicant](t, c.request("POST", "/api/applicants", form, 200)["item"])
	if updated.ID != ap.ID {
		t.Fatal("update created duplicate application")
	}
	s, _ := a.store.settings()
	s.ApplicationDeadline = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := a.store.saveSettings(s); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/applicants", form, 409)
}
func TestSubmissionVersionsAndGrading(t *testing.T) {
	_, c := setup(t)
	c.login("student")
	tasks := unmarshal[[]Task](t, c.request("GET", "/api/tasks", nil, 200)["items"])
	var task Task
	for _, v := range tasks {
		if v.Kind == "code" && v.Direction == "电控" {
			task = v
		}
	}
	if task.ID == "" {
		t.Fatal("expected code task")
	}
	first := unmarshal[Submission](t, c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{"content": "第二版实现"}, 201)["item"])
	second := unmarshal[Submission](t, c.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{"content": "第三版实现"}, 201)["item"])
	if second.Version != first.Version+1 {
		t.Fatal("version not incremented")
	}
	c.request("PATCH", "/api/submissions/"+second.ID, map[string]any{"status": "approved", "score": 90}, 403)
	c.login("mentor")
	c.request("PATCH", "/api/submissions/"+first.ID, map[string]any{"status": "approved", "score": 90}, 409)
	c.request("PATCH", "/api/submissions/"+second.ID, map[string]any{"status": "approved", "score": task.MaxScore + 1}, 400)
	c.request("PATCH", "/api/submissions/"+second.ID, map[string]any{"status": "approved"}, 400)
	c.request("PATCH", "/api/submissions/"+second.ID, map[string]any{"status": "changes_requested", "score": 70, "feedback": ""}, 400)
	graded := unmarshal[Submission](t, c.request("PATCH", "/api/submissions/"+second.ID, map[string]any{"status": "approved", "score": 94, "feedback": "验证通过"}, 200)["item"])
	if graded.ReviewedBy != "demo-mentor" || graded.Score == nil || *graded.Score != 94 {
		t.Fatal("grade not persisted")
	}
	c.login("student")
	tasks = unmarshal[[]Task](t, c.request("GET", "/api/tasks", nil, 200)["items"])
	for _, v := range tasks {
		if v.ID == task.ID && (v.MySubmission == nil || v.MySubmission.ID != second.ID || v.MySubmission.Status != "approved") {
			t.Fatal("latest submission selection incorrect")
		}
	}
	history := unmarshal[[]Submission](t, c.request("GET", "/api/tasks/"+task.ID+"/submissions", nil, 200)["items"])
	if len(history) != 3 {
		t.Fatalf("immutable history lost: %d", len(history))
	}
	for _, s := range history {
		if s.ID == first.ID && s.Status != "pending" {
			t.Fatal("old submission modified")
		}
	}
}
func TestSubmissionAdmissionScopeAndDeadline(t *testing.T) {
	a, c := setup(t)
	c.login("student")
	all, err := readAll[Task](a.store, "SELECT payload FROM tasks")
	if err != nil {
		t.Fatal(err)
	}
	var own, other Task
	for _, v := range all {
		if v.Direction == "电控" {
			own = v
		}
		if v.Direction == "机械" {
			other = v
		}
	}
	c.request("POST", "/api/tasks/"+other.ID+"/submissions", map[string]string{"content": "test"}, 403)
	own.DueAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err = a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(own), own.ID); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/tasks/"+own.ID+"/submissions", map[string]string{"content": "late"}, 409)
	c.register("未录取学生", "unaccepted@example.com", "LongPassword!123")
	c.request("POST", "/api/tasks/"+other.ID+"/submissions", map[string]string{"content": "test"}, 403)
	c.request("GET", "/api/tasks/"+own.ID+"/submissions", nil, 403)
}
func TestSettingsTaskValidationAndDashboard(t *testing.T) {
	_, c := setup(t)
	c.login("admin")
	c.request("PATCH", "/api/settings", map[string]string{"hydroUrl": "javascript:alert(1)"}, 400)
	c.request("PATCH", "/api/settings", map[string]string{"applicationDeadline": "tomorrow"}, 400)
	c.request("POST", "/api/tasks", map[string]any{"title": "任务", "description": "说明", "kind": "code", "difficulty": "入门", "maxScore": -1}, 400)
	c.request("POST", "/api/tasks", map[string]any{"title": "安全任务", "description": "说明", "kind": "code", "difficulty": "入门", "maxScore": 100, "resourcesUrl": "https://example.com"}, 201)
	stats := unmarshal[map[string]int](t, c.request("GET", "/api/dashboard", nil, 200)["stats"])
	if stats["applicants"] != 10 || stats["activeTasks"] != 7 {
		t.Fatalf("unexpected dashboard: %v", stats)
	}
	c.login("student")
	stats = unmarshal[map[string]int](t, c.request("GET", "/api/dashboard", nil, 200)["stats"])
	if stats["applicants"] != 1 || stats["members"] != 1 {
		t.Fatal("student dashboard not scoped")
	}
}
func TestDemoGuardAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = initialize(s, true); err != nil {
		t.Fatal(err)
	}
	if err = initialize(s, false); err == nil {
		t.Fatal("public demo credentials allowed in production")
	}
	s.db.Close()
	s, err = openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	if err = initialize(s, true); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.row("SELECT COUNT(*) FROM applicants").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatal("restart duplicated seeded data")
	}
}
func TestStaticFilesCannotEscapeRoot(t *testing.T) {
	a, c := setup(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(a.config.WebDir, "secret.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.config.WebDir, "index.html"), []byte("<html>app</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	for path, status := range map[string]int{"/secret.txt": 404, "/workspace/tasks": 200, "/missing.js": 404} {
		w := httptest.NewRecorder()
		c.handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status {
			t.Fatal(fmt.Sprintf("%s: %d", path, w.Code))
		}
		if strings.Contains(w.Body.String(), "private") {
			t.Fatal("static file escaped WEB_DIR")
		}
	}
}

func TestAdminCreatesStaffAndAdmitsExistingIdentity(t *testing.T) {
	_, c := setup(t)
	c.login("student")
	c.request("POST", "/api/members", map[string]string{"name": "导师", "email": "teacher@example.com", "password": "StaffPassword!123", "role": "mentor"}, 403)
	c.login("admin")
	c.request("POST", "/api/members", map[string]string{"name": "导师", "email": "teacher@example.com", "password": "StaffPassword!123", "role": "invalid"}, 400)
	created := unmarshal[User](t, c.request("POST", "/api/members", map[string]any{"name": "导师", "email": "teacher@example.com", "password": "StaffPassword!123", "role": "group_admin", "team": "ROBOCON", "direction": "机械", "groupGrants": []GroupGrant{{"机械", []string{"members"}}}}, 201)["item"])
	if created.Role != "group_admin" {
		t.Fatal("wrong staff role")
	}
	c.request("POST", "/api/members", map[string]string{"name": "导师", "email": "teacher@example.com", "password": "StaffPassword!123", "role": "group_admin"}, 409)
	list := unmarshal[[]Applicant](t, c.request("GET", "/api/applicants", nil, 200)["items"])
	var ap Applicant
	for _, v := range list {
		if v.Status == "submitted" {
			ap = v
			break
		}
	}
	accepted := unmarshal[Applicant](t, c.request("PATCH", "/api/applicants/"+ap.ID, map[string]string{"status": "accepted"}, 200)["item"])
	if accepted.UserID != ap.UserID {
		t.Fatal("admission created a different identity")
	}
	members := unmarshal[[]User](t, c.request("GET", "/api/members", nil, 200)["items"])
	found := false
	for _, v := range members {
		if v.ID == ap.UserID {
			found = true
			if v.Team != ap.Team || v.Direction != ap.Direction {
				t.Fatal("member scope not updated")
			}
		}
	}
	if !found {
		t.Fatal("admitted user not a member")
	}
	c.request("POST", "/api/auth/login", map[string]string{"email": "teacher@example.com", "password": "StaffPassword!123"}, 200)
	c.request("GET", "/api/members", nil, 200)
	c.request("PATCH", "/api/settings", map[string]string{"organization": "不能修改"}, 403)
}
func TestProductionRequiresBootstrap(t *testing.T) {
	s, err := openStore("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	t.Setenv("ADMIN_EMAIL", "")
	t.Setenv("ADMIN_PASSWORD", "")
	if err = initialize(s, false); err == nil {
		t.Fatal("production bootstrapped without credentials")
	}
	t.Setenv("ADMIN_EMAIL", "owner@example.com")
	t.Setenv("ADMIN_PASSWORD", "StrongPassword!2026")
	if err = initialize(s, false); err != nil {
		t.Fatal(err)
	}
	users, err := readAll[User](s, "SELECT payload FROM users")
	if err != nil || len(users) != 1 || users[0].Role != "admin" {
		t.Fatal("bootstrap admin missing")
	}
	t.Setenv("ADMIN_PASSWORD", "")
	if err = initialize(s, false); err != nil {
		t.Fatal("existing production database unnecessarily requires bootstrap credentials")
	}
}
