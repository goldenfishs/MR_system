package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func resetBody(email, code, password string) map[string]string {
	return map[string]string{"email": email, "verificationCode": code, "password": password}
}
func resetCodeFor(t *testing.T, a *App, c *testClient, email string) string {
	t.Helper()
	response := c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": email}, 200)
	waitResetIdle(t, a)
	code := c.mailer.last(t).code
	if strings.Contains(encoded(response), code) {
		t.Fatal("code leaked in response")
	}
	return code
}
func waitResetIdle(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(a.resetSlots) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("reset worker did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestPasswordResetSessionsReplayAndPolicy(t *testing.T) {
	a, c := emailSetup(t)
	u := unmarshal[User](t, c.register("reset@example.com", "123456")["user"])
	other := &testClient{t: t, handler: c.handler}
	other.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": "123456"}, 200)
	code := resetCodeFor(t, a, c, " RESET@example.com ")
	c.request("POST", "/api/auth/password-reset/confirm", resetBody(u.Email, code, "12345"), 400)
	c.request("POST", "/api/auth/password-reset/confirm", resetBody(u.Email, code, "密码只有六个"), 200)
	c.request("GET", "/api/auth/me", nil, 401)
	other.request("GET", "/api/auth/me", nil, 401)
	c.request("POST", "/api/auth/password-reset/confirm", resetBody(u.Email, code, "654321"), 400)
	c.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": "123456"}, 401)
	c.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": "密码只有六个"}, 200)
	var remaining int
	if err := a.store.row("SELECT COUNT(*) FROM sessions WHERE user_id=?", u.ID).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("session count %d: %v", remaining, err)
	}
	// Password reset does not change permissions, enrollment, or account status.
	after, err := a.store.user(u.ID)
	if err != nil || encoded(u) != encoded(after) {
		t.Fatalf("user changed: %v", err)
	}
}
func TestPasswordResetExpiryLockoutAndIsolation(t *testing.T) {
	a, c := emailSetup(t)
	u := unmarshal[User](t, c.register("locked@example.com", "123456")["user"])
	code := resetCodeFor(t, a, c, u.Email)
	c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": u.Email}, 200)
	wrong := "000000"
	if code == wrong {
		wrong = "999999"
	}
	for i := 0; i < 5; i++ {
		status := 400
		if i == 4 {
			status = 429
		}
		c.request("POST", "/api/auth/password-reset/confirm", resetBody(u.Email, wrong, "654321"), status)
	}
	c.request("POST", "/api/auth/password-reset/confirm", resetBody(u.Email, code, "654321"), 429)
	if err := a.store.exec("UPDATE password_reset_codes SET last_sent_at=last_sent_at-61 WHERE email=?", u.Email); err != nil {
		t.Fatal(err)
	}
	second := resetCodeFor(t, a, c, u.Email)
	c.request("POST", "/api/auth/password-reset/confirm", resetBody(u.Email, code, "654321"), 400)
	if err := a.store.exec("UPDATE password_reset_codes SET expires_at=? WHERE email=?", time.Now().Unix()-1, u.Email); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/auth/password-reset/confirm", resetBody(u.Email, second, "654321"), 400)
}
func TestPasswordResetEnumerationLimitsAndFailure(t *testing.T) {
	a, c := emailSetup(t)
	if _, err := createUser(a.store, "test", "exists@example.com", "123456", "freshman", "", ""); err != nil {
		t.Fatal(err)
	}
	existing := c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": "exists@example.com"}, 200)
	waitResetIdle(t, a)
	unknown := c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": "absent@example.com"}, 200)
	if encoded(existing) != encoded(unknown) || len(c.mailer.messages) != 1 {
		t.Fatal("account enumeration or unknown mailbox delivery")
	}
	requireRetry(t, c, c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": "absent@example.com"}, 429))
	requireRetry(t, c, c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": "exists@example.com"}, 429))
	for i := 0; i < 4; i++ {
		if err := a.store.exec("UPDATE password_reset_codes SET last_sent_at=last_sent_at-61 WHERE email=?", "exists@example.com"); err != nil {
			t.Fatal(err)
		}
		c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": "exists@example.com"}, 200)
		waitResetIdle(t, a)
	}
	a.store.exec("UPDATE password_reset_codes SET last_sent_at=last_sent_at-61 WHERE email=?", "exists@example.com")
	c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": "exists@example.com"}, 429)
	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"} {
		c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": email}, 200)
	}
	c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": "e@example.com"}, 429)
	waitResetIdle(t, a)
	// Failed SMTP must never activate a reset code or disclose existence.
	a.store.exec("DELETE FROM password_reset_send_events")
	a.store.exec("DELETE FROM password_reset_codes")
	c.mailer.err = errors.New("sensitive SMTP content")
	failed := c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": "exists@example.com"}, 200)
	waitResetIdle(t, a)
	if encoded(failed) != encoded(unknown) {
		t.Fatal("SMTP error disclosed account existence")
	}
	c.request("POST", "/api/auth/password-reset/confirm", resetBody("exists@example.com", c.mailer.last(t).code, "654321"), 400)
}
func TestAdminPasswordAuthorizationAndPendingReset(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler, mailer: admin.mailer}
	student.login("student")
	mentor := &testClient{t: t, handler: admin.handler}
	mentor.login("mentor")
	anonymous := &testClient{t: t, handler: admin.handler}
	path := "/api/members/demo-student/password"
	anonymous.request("PUT", path, map[string]string{"password": "654321"}, 401)
	student.request("PUT", path, map[string]string{"password": "654321"}, 403)
	mentor.request("PUT", path, map[string]string{"password": "654321"}, 403)
	code := resetCodeFor(t, a, student, "student@demo.local")
	admin.request("PUT", path, map[string]string{"password": "12345"}, 400)
	admin.request("PUT", path, map[string]string{"password": "654321"}, 200)
	student.request("GET", "/api/auth/me", nil, 401)
	student.request("POST", "/api/auth/password-reset/confirm", resetBody("student@demo.local", code, "111111"), 400)
	student.request("POST", "/api/auth/login", map[string]string{"email": "student@demo.local", "password": "654321"}, 200)
	admin.request("PUT", "/api/members/demo-admin/password", map[string]string{"password": "654321"}, 400)
	admin.request("PUT", "/api/members/demo-admin/password", map[string]string{"password": "654321", "currentPassword": "DemoAdmin!2026"}, 200)
	admin.request("GET", "/api/auth/me", nil, 401)
	admin.request("POST", "/api/auth/login", map[string]string{"email": "admin@demo.local", "password": "654321"}, 200)
	admin.request("POST", "/api/users/demo-student/deactivate", map[string]string{"reason": "test"}, 200)
	admin.request("PUT", path, map[string]string{"password": "111111"}, 200)
	student.request("POST", "/api/auth/login", map[string]string{"email": "student@demo.local", "password": "111111"}, 403)
	var count int
	if err := a.store.row("SELECT COUNT(*) FROM activities WHERE payload LIKE ?", "%654321%").Scan(&count); err != nil || count != 0 {
		t.Fatal("password leaked into activity")
	}
}
func TestPasswordLengthOnlyAndLegacyCompatibility(t *testing.T) {
	a, c := emailSetup(t)
	for _, password := range []string{"123456", "abcdef", "      ", "中文密码六字", strings.Repeat("x", 73), strings.Repeat("密", 60)} {
		hash, err := hashPassword(password)
		if err != nil {
			t.Fatal(err)
		}
		if comparePassword(string(hash), password) != nil || comparePassword(string(hash), password+"z") == nil {
			t.Fatal("password mismatch or truncation")
		}
	}
	long := strings.Repeat("p", 72) + "extra"
	c.register("long@example.com", long)
	c.request("POST", "/api/auth/login", map[string]string{"email": "long@example.com", "password": long}, 200)
	c.request("POST", "/api/auth/login", map[string]string{"email": "long@example.com", "password": long[:72] + "other"}, 401)
	admin, err := createUser(a.store, "admin", "admin@example.com", "123456", "admin", "", "")
	if err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/auth/login", map[string]string{"email": admin.Email, "password": "123456"}, 200)
	c.request("POST", "/api/members", map[string]string{"name": "six", "email": "six@example.com", "password": "123456", "role": "freshman"}, 201)
}
func (m *blockingMailer) SendPasswordReset(ctx context.Context, email, code string) error {
	return m.SendVerification(ctx, email, code)
}
func TestPasswordResetPendingDeliveryCannotUndoAdminChange(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	mailer := &blockingMailer{calls: make(chan pendingDelivery, 2)}
	a.config.Mailer = mailer
	pendingResponse := asyncRequest(c.handler, "POST", "/api/auth/password-reset/request", map[string]string{"email": "student@demo.local"}, nil)
	pending := awaitDelivery(t, mailer)
	awaitStatus(t, pendingResponse, 200)
	awaitStatus(t, asyncRequest(c.handler, "POST", "/api/auth/password-reset/confirm", resetBody(pending.email, pending.code, "123456"), nil), 400)
	c.request("PUT", "/api/members/demo-student/password", map[string]string{"password": "654321"}, 200)
	pending.release <- nil
	waitResetIdle(t, a)
	c.request("POST", "/api/auth/password-reset/confirm", resetBody(pending.email, pending.code, "123456"), 400)
}

func TestPasswordResetRollbackAndConcurrentConsumption(t *testing.T) {
	a, c := emailSetup(t)
	u := unmarshal[User](t, c.register("atomic@example.com", "123456")["user"])
	code := resetCodeFor(t, a, c, u.Email)
	if err := a.store.exec(`CREATE TRIGGER block_password_sessions BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/auth/password-reset/confirm", resetBody(u.Email, code, "654321"), 500)
	var hash string
	if err := a.store.row("SELECT password_hash FROM users WHERE id=?", u.ID).Scan(&hash); err != nil || comparePassword(hash, "123456") != nil {
		t.Fatal("failed session revocation committed password")
	}
	c.request("GET", "/api/auth/me", nil, 200)
	if err := a.store.exec("DROP TRIGGER block_password_sessions"); err != nil {
		t.Fatal(err)
	}
	body := resetBody(u.Email, code, "654321")
	first := asyncRequest(c.handler, "POST", "/api/auth/password-reset/confirm", body, nil)
	second := asyncRequest(c.handler, "POST", "/api/auth/password-reset/confirm", body, nil)
	outcomes := []int{(<-first).Code, (<-second).Code}
	if !(outcomes[0] == 200 && outcomes[1] == 400 || outcomes[0] == 400 && outcomes[1] == 200) {
		t.Fatalf("code consumed twice: %v", outcomes)
	}
	c.request("GET", "/api/auth/me", nil, 401)
}
func TestPasswordResetMigrationPreservesLegacyData(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	u, err := createUser(s, "legacy", "legacy-reset@example.com", "123456", "freshman", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DROP TABLE password_reset_codes", "DROP TABLE password_reset_send_events", "DELETE FROM schema_migrations WHERE version=7"} {
		if err = s.exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.db.Close()
	s, err = openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	actual, err := s.user(u.ID)
	if err != nil || encoded(actual) != encoded(u) {
		t.Fatal("migration changed legacy user")
	}
	for _, table := range []string{"password_reset_codes", "password_reset_send_events"} {
		var count int
		if err = s.row("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("missing or nonempty reset table: %v", err)
		}
	}
	a := newApp(s, Config{WebDir: t.TempDir()})
	c := &testClient{t: t, handler: a.Handler()}
	c.request("POST", "/api/auth/password-reset/request", map[string]string{"email": u.Email}, 503)
	c.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": "123456"}, 200)
}
