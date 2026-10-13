package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type testMail struct{ email, code string }
type testMailer struct {
	mu       sync.Mutex
	messages []testMail
	err      error
}

func (m *testMailer) SendVerification(_ context.Context, email, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, testMail{email, code})
	return m.err
}
func (m *testMailer) last(t *testing.T) testMail {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		t.Fatal("expected a delivered email")
	}
	return m.messages[len(m.messages)-1]
}
func (c *testClient) verification(email string) string {
	c.t.Helper()
	response := c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": email}, 200)
	if unmarshal[int](c.t, response["cooldownSeconds"]) != 60 || unmarshal[int](c.t, response["expiresInSeconds"]) != 600 {
		c.t.Fatal("incorrect delivery timing contract")
	}
	sent := c.mailer.last(c.t)
	if !validVerificationCode(sent.code) {
		c.t.Fatal("verification code is not six decimal digits")
	}
	if strings.Contains(encoded(response), sent.code) {
		c.t.Fatal("HTTP response leaked the verification code")
	}
	return sent.code
}
func (c *testClient) register(name, email, password string) map[string]json.RawMessage {
	c.t.Helper()
	code := c.verification(email)
	return c.request("POST", "/api/auth/register", map[string]string{"name": name, "email": email, "password": password, "verificationCode": code}, 201)
}
func emailSetup(t *testing.T) (*App, *testClient) {
	t.Helper()
	s, err := openStore("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	if err = s.saveSettings(Settings{Organization: "测试系统", Season: "2026 赛季"}); err != nil {
		t.Fatal(err)
	}
	mailer := &testMailer{}
	a := newApp(s, Config{Demo: true, Mailer: mailer, EmailFrom: "sender@example.test", EmailSecurity: "starttls", WebDir: t.TempDir()})
	return a, &testClient{t: t, handler: a.Handler(), mailer: mailer}
}
func registrationBody(email, code string) map[string]string {
	return map[string]string{"name": "验证学员", "email": email, "password": "StrongPassword!2026", "verificationCode": code}
}
func requireRetry(t *testing.T, c *testClient, result map[string]json.RawMessage) {
	t.Helper()
	seconds := unmarshal[int](t, result["retryAfterSeconds"])
	if seconds < 1 || c.lastHeader.Get("Retry-After") != strconv.Itoa(seconds) {
		t.Fatal("rate limit must include matching positive Retry-After header and JSON value")
	}
}
func TestEmailVerificationRegistrationAndReplay(t *testing.T) {
	a, c := emailSetup(t)
	public := c.request("GET", "/api/public/config", nil, 200)
	if !unmarshal[bool](t, public["emailVerificationRequired"]) || !unmarshal[bool](t, public["registrationEnabled"]) {
		t.Fatal("email registration configuration missing")
	}
	c.request("POST", "/api/auth/register", registrationBody("verified@example.com", ""), 400)
	code := c.verification(" VERIFIED@example.com ")
	sent := c.mailer.last(t)
	if sent.email != "verified@example.com" {
		t.Fatal("recipient was not normalized")
	}
	var stored string
	if err := a.store.row("SELECT code_hash FROM email_verifications WHERE email=?", sent.email).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == code || bcrypt.CompareHashAndPassword([]byte(stored), []byte(code)) != nil {
		t.Fatal("code was not bcrypt hashed")
	}
	c.request("POST", "/api/auth/register", registrationBody("different@example.com", code), 400)
	user := unmarshal[User](t, c.request("POST", "/api/auth/register", registrationBody("VERIFIED@example.com", code), 201)["user"])
	if user.EmailVerifiedAt == "" || user.Role != "student" || user.Email != "verified@example.com" {
		t.Fatal("registration did not verify the bound student identity")
	}
	if _, err := time.Parse(time.RFC3339, user.EmailVerifiedAt); err != nil {
		t.Fatal("invalid email verification timestamp")
	}
	c.request("GET", "/api/auth/me", nil, 200)
	c.request("POST", "/api/auth/logout", nil, 200)
	c.request("POST", "/api/auth/register", registrationBody(user.Email, code), 400)
	c.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": "StrongPassword!2026"}, 200)
	var remaining int
	if err := a.store.row("SELECT COUNT(*) FROM email_verifications WHERE email=?", user.Email).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("consumed code remains stored")
	}
}
func TestEmailVerificationExpiryResendAndLockout(t *testing.T) {
	a, c := emailSetup(t)
	email := "retry@example.com"
	first := c.verification(email)
	requireRetry(t, c, c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": email}, 429))
	if err := a.store.exec("UPDATE email_verifications SET last_sent_at=last_sent_at-61 WHERE email=?", email); err != nil {
		t.Fatal(err)
	}
	second := c.verification(email)
	// Random collisions are valid but exceptionally rare. Compare against a known
	// incorrect code to avoid a flaky test while still proving the hash rotated.
	var hash string
	if err := a.store.row("SELECT code_hash FROM email_verifications WHERE email=?", email).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if first != second {
		c.request("POST", "/api/auth/register", registrationBody(email, first), 400)
	}
	wrong := "000000"
	if second == wrong {
		wrong = "000001"
	}
	var already int
	if err := a.store.row("SELECT attempts FROM email_verifications WHERE email=?", email).Scan(&already); err != nil {
		t.Fatal(err)
	}
	for i := already; i < 5; i++ {
		status := 400
		if i == 4 {
			status = 429
		}
		result := c.request("POST", "/api/auth/register", registrationBody(email, wrong), status)
		if status == 429 {
			requireRetry(t, c, result)
		}
	}
	requireRetry(t, c, c.request("POST", "/api/auth/register", registrationBody(email, second), 429))
	if err := a.store.exec("UPDATE email_verifications SET last_sent_at=last_sent_at-61 WHERE email=?", email); err != nil {
		t.Fatal(err)
	}
	third := c.verification(email)
	if err := a.store.exec("UPDATE email_verifications SET expires_at=? WHERE email=?", time.Now().Unix()-1, email); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/auth/register", registrationBody(email, third), 400)
}
func TestEmailDeliveryFailureAndUnavailable(t *testing.T) {
	a, c := emailSetup(t)
	email := "failed@example.com"
	first := c.verification(email)
	if err := a.store.exec("UPDATE email_verifications SET last_sent_at=last_sent_at-61 WHERE email=?", email); err != nil {
		t.Fatal(err)
	}
	c.mailer.err = errors.New("sensitive SMTP response must not be exposed")
	result := c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": email}, 503)
	if strings.Contains(encoded(result), "sensitive") {
		t.Fatal("SMTP error leaked")
	}
	c.request("POST", "/api/auth/register", registrationBody(email, first), 400)
	c.request("POST", "/api/auth/register", registrationBody(email, c.mailer.last(t).code), 400)
	requireRetry(t, c, c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": email}, 429))
	a.config.Mailer = nil
	public := c.request("GET", "/api/public/config", nil, 200)
	if unmarshal[bool](t, public["registrationEnabled"]) {
		t.Fatal("unconfigured mail service enables registration")
	}
	c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": "valid@example.com"}, 503)
	c.request("POST", "/api/auth/register", registrationBody("valid@example.com", "123456"), 503)
}
func TestVerificationRatesPersistAcrossAppRestart(t *testing.T) {
	a, c := emailSetup(t)
	email := "limited@example.com"
	for i := 0; i < 5; i++ {
		c.verification(email)
		if err := a.store.exec("UPDATE email_verifications SET last_sent_at=last_sent_at-61 WHERE email=?", email); err != nil {
			t.Fatal(err)
		}
	}
	c.handler = newApp(a.store, a.config).Handler()
	requireRetry(t, c, c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": email}, 429))
	for i := 0; i < 5; i++ {
		c.verification(fmt.Sprintf("network%d@example.com", i))
	}
	requireRetry(t, c, c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": "network-over@example.com"}, 429))
}
func TestVerificationAndSessionAtomicRollback(t *testing.T) {
	a, c := emailSetup(t)
	email := "atomic@example.com"
	code := c.verification(email)
	if err := a.store.exec(`CREATE TRIGGER fail_new_session BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT, 'test session failure'); END`); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/auth/register", registrationBody(email, code), 500)
	var users int
	if err := a.store.row("SELECT COUNT(*) FROM users WHERE email=?", email).Scan(&users); err != nil || users != 0 {
		t.Fatal("failed registration committed an account")
	}
	if c.cookie != nil {
		t.Fatal("failed registration emitted a session cookie")
	}
	if err := a.store.exec("DROP TRIGGER fail_new_session"); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/auth/register", registrationBody(email, code), 201)
}
func TestConcurrentRegistrationConsumesCodeOnce(t *testing.T) {
	_, c := emailSetup(t)
	email := "concurrent@example.com"
	code := c.verification(email)
	statuses := make(chan int, 2)
	body := encoded(registrationBody(email, code))
	for i := 0; i < 2; i++ {
		go func() {
			w := httptest.NewRecorder()
			c.handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(body)))
			statuses <- w.Code
		}()
	}
	first, second := <-statuses, <-statuses
	if !((first == 201 && second == 400) || (first == 400 && second == 201)) {
		t.Fatalf("single-use challenge statuses: %d, %d", first, second)
	}
}
func TestEmailSettingsVisibilityAndLegacyLogin(t *testing.T) {
	a, c := setup(t)
	c.request("GET", "/api/settings/email", nil, 401)
	c.login("student")
	c.request("GET", "/api/settings/email", nil, 403)
	legacy := unmarshal[User](t, c.request("GET", "/api/auth/me", nil, 200)["user"])
	if legacy.EmailVerifiedAt != "" {
		t.Fatal("legacy account was falsely marked verified")
	}
	c.login("admin")
	a.config.EmailFrom = "sender@example.com"
	a.config.EmailSecurity = "tls"
	result := c.request("GET", "/api/settings/email", nil, 200)
	if len(result) != 3 || !unmarshal[bool](t, result["configured"]) || unmarshal[string](t, result["from"]) != "sender@example.com" {
		t.Fatal("unsafe or missing SMTP status fields")
	}
}
func TestEmailVerificationSurvivesDatabaseReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	mailer := &testMailer{}
	cfg := Config{Demo: true, Mailer: mailer, WebDir: t.TempDir()}
	c := &testClient{t: t, handler: newApp(s, cfg).Handler(), mailer: mailer}
	email := "persistent@example.com"
	code := c.verification(email)
	if err = s.db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	c.handler = newApp(s, cfg).Handler()
	requireRetry(t, c, c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": email}, 429))
	c.request("POST", "/api/auth/register", registrationBody(email, code), 201)
	var version int
	if err = s.row("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 2 {
		t.Fatal("email migration v2 missing")
	}
}

func TestEmailMigrationUpgradesExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := createUser(s, "原有用户", "legacy@example.com", "ExistingPassword!2026", "student", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct a v1 schema with its original account payload intact.
	for _, query := range []string{"DROP TABLE email_verifications", "DROP TABLE verification_send_events", "DELETE FROM schema_migrations WHERE version=2"} {
		if err = s.exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	c := &testClient{t: t, handler: newApp(s, Config{WebDir: t.TempDir()}).Handler()}
	c.request("POST", "/api/auth/login", map[string]string{"email": legacy.Email, "password": "ExistingPassword!2026"}, 200)
	user := unmarshal[User](t, c.request("GET", "/api/auth/me", nil, 200)["user"])
	if user.ID != legacy.ID || user.EmailVerifiedAt != "" {
		t.Fatal("migration changed or falsely verified a legacy account")
	}
	var count int
	if err = s.row("SELECT COUNT(*) FROM email_verifications").Scan(&count); err != nil {
		t.Fatal("verification table missing after upgrade")
	}
	if err = s.row("SELECT COUNT(*) FROM schema_migrations WHERE version=2").Scan(&count); err != nil || count != 1 {
		t.Fatal("migration v2 was not applied exactly once")
	}
}
func TestRegisteredAddressDoesNotReceiveRegistrationCode(t *testing.T) {
	a, c := emailSetup(t)
	_, err := createUser(a.store, "原有用户", "existing@example.com", "ExistingPassword!2026", "student", "", "")
	if err != nil {
		t.Fatal(err)
	}
	result := c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": "existing@example.com"}, 200)
	if !strings.Contains(unmarshal[string](t, result["message"]), "若该邮箱可用于注册") || len(c.mailer.messages) != 0 {
		t.Fatal("registration sender exposed an existing account or mailed it a new code")
	}
	c.request("POST", "/api/auth/register", registrationBody("existing@example.com", "123456"), 400)
}

type pendingDelivery struct {
	email   string
	code    string
	release chan error
}
type blockingMailer struct{ calls chan pendingDelivery }

func (m *blockingMailer) SendVerification(ctx context.Context, email, code string) error {
	pending := pendingDelivery{email: email, code: code, release: make(chan error, 1)}
	select {
	case m.calls <- pending:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-pending.release:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func awaitDelivery(t *testing.T, m *blockingMailer) pendingDelivery {
	t.Helper()
	select {
	case delivery := <-m.calls:
		t.Cleanup(func() {
			select {
			case delivery.release <- nil:
			default:
			}
		})
		return delivery
	case <-time.After(5 * time.Second):
		t.Fatal("SMTP send did not reach the mailer")
		return pendingDelivery{}
	}
}
func asyncRequest(handler http.Handler, method, path string, body any, cookie *http.Cookie) <-chan *httptest.ResponseRecorder {
	responses := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(method, path, strings.NewReader(encoded(body)))
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		responses <- response
	}()
	return responses
}
func awaitStatus(t *testing.T, responses <-chan *httptest.ResponseRecorder, status int) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-responses:
		if response.Code != status {
			t.Fatalf("got HTTP %d, expected %d: %s", response.Code, status, response.Body.String())
		}
		return response
	case <-time.After(5 * time.Second):
		t.Fatal("request blocked on in-flight SMTP delivery")
		return nil
	}
}
func TestSlowSMTPDoesNotBlockLoginOrWrites(t *testing.T) {
	a, c := emailSetup(t)
	if _, err := createUser(a.store, "管理员", "owner@example.com", "OwnerPassword!2026", "admin", "", ""); err != nil {
		t.Fatal(err)
	}
	mailer := &blockingMailer{calls: make(chan pendingDelivery, 2)}
	a.config.Mailer = mailer
	sending := asyncRequest(c.handler, "POST", "/api/auth/send-verification-code", map[string]string{"email": "pending@example.com"}, nil)
	pending := awaitDelivery(t, mailer)
	// Even the correct code cannot be consumed before SMTP accepts the message.
	awaitStatus(t, asyncRequest(c.handler, "POST", "/api/auth/register", registrationBody(pending.email, pending.code), nil), 400)
	login := awaitStatus(t, asyncRequest(c.handler, "POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": "OwnerPassword!2026"}, nil), 200)
	var cookie *http.Cookie
	for _, item := range login.Result().Cookies() {
		if item.Name == "robot_hub_session" {
			cookie = item
		}
	}
	if cookie == nil {
		t.Fatal("login did not create a session")
	}
	awaitStatus(t, asyncRequest(c.handler, "PATCH", "/api/settings", map[string]string{"organization": "邮件等待期间保存成功"}, cookie), 200)
	pending.release <- nil
	awaitStatus(t, sending, 200)
	c.request("POST", "/api/auth/register", registrationBody(pending.email, pending.code), 201)
}
func TestOlderSMTPCompletionCannotActivateReplacement(t *testing.T) {
	a, c := emailSetup(t)
	mailer := &blockingMailer{calls: make(chan pendingDelivery, 2)}
	a.config.Mailer = mailer
	email := "overlap@example.com"
	firstResponse := asyncRequest(c.handler, "POST", "/api/auth/send-verification-code", map[string]string{"email": email}, nil)
	first := awaitDelivery(t, mailer)
	if err := a.store.exec("UPDATE email_verifications SET last_sent_at=last_sent_at-61 WHERE email=?", email); err != nil {
		t.Fatal(err)
	}
	secondResponse := asyncRequest(c.handler, "POST", "/api/auth/send-verification-code", map[string]string{"email": email}, nil)
	second := awaitDelivery(t, mailer)
	first.release <- nil
	awaitStatus(t, firstResponse, 409)
	c.request("POST", "/api/auth/register", registrationBody(email, second.code), 400)
	var delivered int
	if err := a.store.row("SELECT delivered FROM email_verifications WHERE email=?", email).Scan(&delivered); err != nil || delivered != 0 {
		t.Fatal("older SMTP send activated a newer pending challenge")
	}
	second.release <- nil
	awaitStatus(t, secondResponse, 200)
	c.request("POST", "/api/auth/register", registrationBody(email, second.code), 201)
}
