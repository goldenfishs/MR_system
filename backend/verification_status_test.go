package main

import "testing"

func TestVerificationStatusReflectsResetWithoutSending(t *testing.T) {
	a, c := emailSetup(t)
	email := "status@example.com"
	body := map[string]string{"email": email}
	for i := 0; i < 5; i++ {
		c.verification(email)
		if err := a.store.exec("UPDATE email_verifications SET last_sent_at=last_sent_at-61 WHERE email=?", email); err != nil {
			t.Fatal(err)
		}
	}
	before := len(c.mailer.messages)
	status := c.request("POST", "/api/auth/verification-status", body, 200)
	if unmarshal[int](t, status["cooldownSeconds"]) <= 60 {
		t.Fatal("hourly restriction missing")
	}
	if err := a.store.exec("DELETE FROM verification_send_events WHERE email=?", email); err != nil {
		t.Fatal(err)
	}
	status = c.request("POST", "/api/auth/verification-status", body, 200)
	if unmarshal[int](t, status["cooldownSeconds"]) != 0 {
		t.Fatal("stale restriction after reset")
	}
	if len(c.mailer.messages) != before {
		t.Fatal("status endpoint sent mail")
	}
	var count int
	if err := a.store.row("SELECT COUNT(*) FROM verification_send_events").Scan(&count); err != nil || count != 0 {
		t.Fatal("status endpoint reserved send")
	}
	c.verification(email)
	status = c.request("POST", "/api/auth/verification-status", body, 200)
	if seconds := unmarshal[int](t, status["cooldownSeconds"]); seconds < 1 || seconds > 60 {
		t.Fatal("resend interval missing")
	}
	c.request("POST", "/api/auth/verification-status", map[string]string{"email": "invalid"}, 400)
}
