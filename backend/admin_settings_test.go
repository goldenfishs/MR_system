package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func mailSettingsFixture() map[string]any {
	return map[string]any{"enabled": true, "host": "smtp.example.test", "port": 465, "username": "robot@example.test", "password": "test-only-authorization-code", "from": "robot@example.test", "fromName": "机器人战队", "security": "tls"}
}

func TestAdminMailSettingsPermissionsAndRedaction(t *testing.T) {
	a, c := setup(t)
	c.request("GET", "/api/settings/mail-service", nil, 401)
	c.request("PATCH", "/api/settings/mail-service", mailSettingsFixture(), 401)
	c.login("student")
	c.request("GET", "/api/settings/mail-service", nil, 403)
	c.request("PATCH", "/api/settings/mail-service", mailSettingsFixture(), 403)
	c.login("mentor")
	c.request("GET", "/api/settings/mail-service", nil, 403)
	c.request("PATCH", "/api/settings/mail-service", mailSettingsFixture(), 403)
	c.login("admin")
	result := c.request("PATCH", "/api/settings/mail-service", mailSettingsFixture(), 200)
	for _, response := range []map[string]json.RawMessage{result, c.request("GET", "/api/settings/mail-service", nil, 200)} {
		if strings.Contains(encoded(response), "test-only-authorization-code") || response["password"] != nil || !unmarshal[bool](t, response["passwordSet"]) {
			t.Fatal("SMTP credentials leaked or missing password state")
		}
	}
	if !unmarshal[bool](t, result["configured"]) || !unmarshal[bool](t, result["supportsBulk"]) || unmarshal[string](t, result["source"]) != "database" {
		t.Fatal("mail service did not become active")
	}
	var stored string
	if err := a.store.row("SELECT payload FROM settings WHERE id='mail-service'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "test-only-authorization-code") || !strings.Contains(stored, "passwordCiphertext") {
		t.Fatal("mail secret not encrypted at rest")
	}
	keyInfo, err := os.Stat(filepath.Join(filepath.Dir(a.store.fileDir), "mail-settings.key"))
	if err != nil || keyInfo.Mode().Perm() != 0600 {
		t.Fatal("mail key must be owner readable only")
	}
	settings := c.request("GET", "/api/settings", nil, 200)
	public := c.request("GET", "/api/public/config", nil, 200)
	if strings.Contains(encoded(settings), "smtp.example.test") || strings.Contains(encoded(public), "smtp.example.test") {
		t.Fatal("private service configuration included in public settings")
	}
	if len(c.mailer.messages) != 0 {
		t.Fatal("saving settings sent a message")
	}
}

func TestAdminMailSettingsPersistAndPreserveBlankSecret(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	c.request("PATCH", "/api/settings/mail-service", mailSettingsFixture(), 200)
	c.request("PATCH", "/api/settings/mail-service", map[string]any{"password": "", "fromName": "新名称"}, 200)
	if a.currentMailService().config.Password != "test-only-authorization-code" {
		t.Fatal("blank password replaced existing secret")
	}
	restarted := newApp(a.store, Config{Demo: true})
	if err := restarted.loadStoredMailService(); err != nil {
		t.Fatal(err)
	}
	snapshot := restarted.currentMailService()
	if snapshot.config.Password != "test-only-authorization-code" || snapshot.config.FromName != "新名称" || snapshot.verification == nil || snapshot.notice == nil {
		t.Fatal("stored settings were not restored")
	}
	c.request("PATCH", "/api/settings/mail-service", map[string]any{"enabled": false}, 200)
	if a.currentMailService().verification != nil || a.currentMailService().notice != nil {
		t.Fatal("disabled service still active")
	}
	if unmarshal[bool](t, c.request("GET", "/api/public/config", nil, 200)["registrationEnabled"]) {
		t.Fatal("registration available while mail disabled")
	}
	c.request("POST", "/api/auth/send-verification-code", map[string]string{"email": "new@example.test"}, 503)
	if err := restarted.loadStoredMailService(); err != nil {
		t.Fatal(err)
	}
	if restarted.currentMailService().verification != nil {
		t.Fatal("disabled state not persisted")
	}
	c.request("PATCH", "/api/settings/mail-service", map[string]any{"clearPassword": true, "username": ""}, 200)
	if a.currentMailService().config.Password != "" {
		t.Fatal("explicit clear did not remove secret")
	}
}

func TestAdminMailSettingsValidationIsAtomic(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	c.request("PATCH", "/api/settings/mail-service", mailSettingsFixture(), 200)
	for _, invalid := range []map[string]any{
		{"host": "https://smtp.example.test"}, {"port": 0}, {"security": "none"},
		{"from": "invalid"}, {"fromName": "invalid\r\nBcc: other@example.test"},
		{"username": ""}, {"clearPassword": true}, {"password": strings.Repeat("x", 4097)},
	} {
		c.request("PATCH", "/api/settings/mail-service", invalid, 400)
	}
	snapshot := a.currentMailService()
	if snapshot.config.Host != "smtp.example.test" || snapshot.config.Password != "test-only-authorization-code" || !snapshot.enabled {
		t.Fatal("invalid update changed active config")
	}
	restarted := newApp(a.store, Config{Demo: true})
	if err := restarted.loadStoredMailService(); err != nil {
		t.Fatal(err)
	}
	if restarted.currentMailService().config.Password != snapshot.config.Password {
		t.Fatal("invalid update altered persistent config")
	}
}

func TestAdminMailSettingsEnvironmentFallbackAndTamper(t *testing.T) {
	a, c := setup(t)
	smtp, err := newSMTPMailer(SMTPConfig{Host: "smtp.initial.test", Port: 587, Username: "initial@example.test", Password: "environment-secret", From: "initial@example.test", Security: "starttls"})
	if err != nil {
		t.Fatal(err)
	}
	a.config.Mailer, a.config.NoticeMailer = smtp, smtp
	if err = a.loadStoredMailService(); err != nil {
		t.Fatal(err)
	}
	c.login("admin")
	initial := c.request("GET", "/api/settings/mail-service", nil, 200)
	if unmarshal[string](t, initial["source"]) != "environment" || unmarshal[string](t, initial["host"]) != "smtp.initial.test" {
		t.Fatal("initial SMTP config not editable")
	}
	c.request("PATCH", "/api/settings/mail-service", map[string]any{"fromName": "战队", "password": ""}, 200)
	if a.currentMailService().config.Password != "environment-secret" {
		t.Fatal("empty secret lost env credential")
	}
	keyPath := filepath.Join(filepath.Dir(a.store.fileDir), "mail-settings.key")
	if err = os.WriteFile(keyPath, []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	if err = newApp(a.store, Config{}).loadStoredMailService(); err == nil {
		t.Fatal("corrupted key should fail rather than silently enable environment mail")
	}
}

func TestAdminMailSettingsConcurrentReads(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	c.request("PATCH", "/api/settings/mail-service", mailSettingsFixture(), 200)
	var readers sync.WaitGroup
	readers.Add(3)
	for range 3 {
		go func() {
			defer readers.Done()
			for range 100 {
				_ = a.currentMailService()
			}
		}()
	}
	for i := range 5 {
		c.request("PATCH", "/api/settings/mail-service", map[string]any{"enabled": i%2 == 0}, 200)
	}
	readers.Wait()
	// Confirm public endpoint still returns a coherent state after concurrent updates.
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/public/config", nil))
	if w.Code != 200 {
		t.Fatal("public config failed")
	}
}

func TestAdminMailSettingsInFlightCampaignKeepsSnapshot(t *testing.T) {
	a, c := setup(t)
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	mailer := &noticeFixture{block: release, started: started}
	a.config.NoticeMailer = mailer
	c.login("admin")
	campaign := unmarshal[MailCampaign](t, c.request("POST", "/api/mail-campaigns", map[string]any{"subject": "本地快照测试", "body": "仅使用内存邮件替身", "userIds": []string{"demo-student", "demo-mentor"}}, 201)["item"])
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 202)
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("fixture mailer did not start")
	}
	c.request("PATCH", "/api/settings/mail-service", map[string]any{"enabled": false}, 200)
	c.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 503)
	close(release)
	item := waitCampaign(t, c, campaign.ID)
	if item.Status != "completed" || mailer.count("student@demo.local") != 1 || mailer.count("mentor@demo.local") != 1 {
		t.Fatal("in-flight campaign did not retain its original sender")
	}
}

func TestSiteDescriptionAdminAndPublicDisplay(t *testing.T) {
	_, c := setup(t)
	c.login("student")
	c.request("PATCH", "/api/settings", map[string]string{"description": "invalid"}, 403)
	c.login("admin")
	c.request("PATCH", "/api/settings", map[string]string{"description": "  同心造机器人  "}, 200)
	public := c.request("GET", "/api/public/config", nil, 200)
	if unmarshal[string](t, public["description"]) != "同心造机器人" {
		t.Fatal("site description missing from public config")
	}
	c.request("PATCH", "/api/settings", map[string]string{"description": strings.Repeat("x", 1501)}, 400)
}
