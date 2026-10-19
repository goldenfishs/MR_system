package main

import (
	"golang.org/x/crypto/bcrypt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountDeactivationRevokesSessionsAndPreservesHistory(t *testing.T) {
	a, admin, student, task := autoAssignmentForTest(t, false)
	second := &testClient{t: t, handler: admin.handler}
	second.login("student")
	oldCookie, secondCookie := student.cookie, second.cookie
	student.request("POST", "/api/tasks/"+task.ID+"/submission-draft", map[string]any{"answers": autoAssignmentAnswers(false)}, 200)
	student.request("POST", "/api/tasks/"+task.ID+"/submission-finalize", nil, 200)
	file := uploadForTest(t, student, "history.c", []byte("int main(void){return 0;}"), 201)
	before, err := a.store.user("demo-student")
	if err != nil {
		t.Fatal(err)
	}
	oldSubs, err := a.store.submissions(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var memberships int
	if err = a.store.row("SELECT COUNT(*) FROM team_memberships WHERE user_id=?", before.ID).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	admin.request("POST", "/api/users/"+before.ID+"/deactivate", map[string]any{"reason": " "}, 400)
	admin.request("POST", "/api/users/"+before.ID+"/deactivate", map[string]any{"reason": strings.Repeat("x", 2001)}, 400)
	closed := unmarshal[User](t, admin.request("POST", "/api/users/"+before.ID+"/deactivate", map[string]any{"reason": "  已离队，保留历史  "}, 200)["item"])
	if closed.DeactivatedAt == "" || closed.DeactivatedBy != "demo-admin" || closed.DeactivationReason != "已离队，保留历史" || closed.Role != before.Role || closed.MemberType != before.MemberType {
		t.Fatalf("invalid closure: %+v", closed)
	}
	found := false
	for _, listed := range unmarshal[[]User](t, admin.request("GET", "/api/users", nil, 200)["items"]) {
		if listed.ID == before.ID {
			found = listed.DeactivatedAt != "" && listed.DeactivationReason == closed.DeactivationReason
		}
	}
	if !found {
		t.Fatal("administrator cannot find closed account for restoration")
	}
	student.request("GET", "/api/auth/me", nil, 401)
	second.request("GET", "/api/auth/me", nil, 401)
	admin.request("POST", "/api/users/"+before.ID+"/deactivate", map[string]any{"reason": "again"}, 409)
	admin.request("PATCH", "/api/members/"+before.ID, map[string]any{"role": "admin"}, 409)
	student.request("POST", "/api/auth/login", map[string]string{"email": before.Email, "password": "DemoStudent!2026"}, 403)
	var sessions int
	if err = a.store.row("SELECT COUNT(*) FROM sessions WHERE user_id=?", before.ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("sessions not revoked")
	}
	// Defense against restored stale sessions and administrative password-only reset.
	if err = a.store.exec("INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", tokenHash(oldCookie.Value), before.ID, "2099-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	student.cookie = oldCookie
	student.request("GET", "/api/auth/me", nil, 401)
	hash, err := bcrypt.GenerateFromPassword([]byte("ResetPassword!2026"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.store.exec("UPDATE users SET password_hash=? WHERE id=?", string(hash), before.ID); err != nil {
		t.Fatal(err)
	}
	student.request("POST", "/api/auth/login", map[string]string{"email": before.Email, "password": "ResetPassword!2026"}, 403)
	if _, err = a.createSession(a.store, httptest.NewRequest("POST", "/", nil), before); err == nil {
		t.Fatal("stale user bypassed disabled guard")
	}
	restored := unmarshal[User](t, admin.request("POST", "/api/users/"+before.ID+"/reactivate", map[string]any{}, 200)["item"])
	if restored.DeactivatedAt != "" || restored.DeactivatedBy != "" || restored.DeactivationReason != "" {
		t.Fatal("closure not cleared")
	}
	student.cookie = oldCookie
	second.cookie = secondCookie
	student.request("GET", "/api/auth/me", nil, 401)
	second.request("GET", "/api/auth/me", nil, 401)
	student.request("POST", "/api/auth/login", map[string]string{"email": before.Email, "password": "ResetPassword!2026"}, 200)
	var finalHash string
	if err = a.store.row("SELECT password_hash FROM users WHERE id=?", before.ID).Scan(&finalHash); err != nil || finalHash != string(hash) {
		t.Fatal("restore changed password")
	}
	retained, err := a.store.submissions(task.ID)
	if err != nil || encoded(retained) != encoded(oldSubs) {
		t.Fatal("submission history changed")
	}
	var retainedMemberships int
	if err = a.store.row("SELECT COUNT(*) FROM team_memberships WHERE user_id=?", before.ID).Scan(&retainedMemberships); err != nil || retainedMemberships != memberships {
		t.Fatal("memberships changed")
	}
	downloadForTest(t, student, file.ID, "download", 200)
	admin.request("POST", "/api/users/"+before.ID+"/reactivate", map[string]any{}, 409)
}

func TestAccountLifecycleAdminPermissionsAndLastActiveAdmin(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	student.request("POST", "/api/users/demo-admin/deactivate", map[string]any{"reason": "x"}, 403)
	student.request("POST", "/api/users/demo-admin/reactivate", map[string]any{}, 403)
	mentor := &testClient{t: t, handler: admin.handler}
	mentor.login("mentor")
	mentor.request("POST", "/api/users/demo-student/deactivate", map[string]any{"reason": "x"}, 403)
	mentor.request("POST", "/api/users/demo-student/reactivate", map[string]any{}, 403)
	admin.request("POST", "/api/users/demo-admin/deactivate", map[string]any{"reason": "x"}, 409)
	admin.request("POST", "/api/users/missing/deactivate", map[string]any{"reason": "x"}, 404)
	extra := unmarshal[User](t, admin.request("POST", "/api/members", map[string]any{"name": "备用管理", "email": "second-admin@example.test", "password": "AdminBackup!2026", "role": "admin", "memberType": "formal"}, 201)["item"])
	closed := unmarshal[User](t, admin.request("POST", "/api/users/"+extra.ID+"/deactivate", map[string]any{"reason": "暂时停用"}, 200)["item"])
	if hasPermission(closed, "机械", "tasks") || hasAnyPermission(closed, "members") || isStaff(closed) {
		t.Fatal("closed administrator retained privileges")
	}
	admin.request("PATCH", "/api/members/demo-admin", map[string]any{"role": "member", "memberType": "formal"}, 409)
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"reason":"x"}`))
	req.SetPathValue("id", "demo-admin")
	err := a.deactivateUser(httptest.NewRecorder(), req, User{ID: "other", Role: "admin"})
	if err == nil || !strings.Contains(err.Error(), "最后一位") {
		t.Fatalf("last active admin guard missing: %v", err)
	}
}
