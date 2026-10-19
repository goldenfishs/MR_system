package main

import "testing"

func TestWorkspaceScopeAndNewMembershipList(t *testing.T) {
	_, c := setup(t)
	user := unmarshal[User](t, c.register("new-membership@example.com", "StrongPassword!2026")["user"])
	resume := uploadForTest(t, c, "resume.pdf", []byte("%PDF-1.7\nresume"), 201)
	c.login("admin")
	team := unmarshal[Team](t, c.request("POST", "/api/teams", map[string]any{"name": "校队", "competition": "ROBOCON", "directions": []string{"机械"}}, 201)["item"])
	c.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": "StrongPassword!2026"}, 200)
	c.request("POST", "/api/teams/"+team.ID+"/applications", map[string]string{"direction": "机械", "resumeFileId": resume.ID}, 400)
	if unmarshal[bool](t, c.request("GET", "/api/workspace-summary", nil, 200)["profileComplete"]) {
		t.Fatal("new account unexpectedly has a complete profile")
	}
	completeProfile(t, c)
	application := unmarshal[TeamApplication](t, c.request("POST", "/api/teams/"+team.ID+"/applications", map[string]string{"direction": "机械", "resumeFileId": resume.ID}, 201)["item"])
	c.login("admin")
	c.request("POST", "/api/interviews/"+application.ID+"/decision", map[string]string{"status": "passed"}, 200)
	c.request("POST", "/api/records", map[string]any{"userId": user.ID, "title": "成员入队证明", "kind": "certificate"}, 201)
	members := unmarshal[[]struct {
		User
		Teams []struct{ ID, Name, Competition, Direction string } `json:"teams"`
	}](t, c.request("GET", "/api/members", nil, 200)["items"])
	found := false
	for _, member := range members {
		if member.ID == user.ID {
			found = len(member.Teams) == 1 && member.Teams[0].ID == team.ID && member.Teams[0].Direction == "机械"
		}
	}
	if !found {
		t.Fatal("new membership without legacy interview missing from directory")
	}
	stats := unmarshal[map[string]int](t, c.request("GET", "/api/workspace-summary", nil, 200)["stats"])
	if stats["applications"] != 1 || stats["members"] != 1 || stats["records"] != 1 {
		t.Fatalf("incorrect admin summary: %+v", stats)
	}
	c.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": "StrongPassword!2026"}, 200)
	mine := c.request("GET", "/api/workspace-summary", nil, 200)
	stats = unmarshal[map[string]int](t, mine["stats"])
	if stats["applications"] != 1 || stats["members"] != 1 || stats["records"] != 1 || stats["files"] != 2 || !unmarshal[bool](t, mine["profileComplete"]) {
		t.Fatalf("incorrect own summary: %+v", stats)
	}
	c.login("student")
	stats = unmarshal[map[string]int](t, c.request("GET", "/api/workspace-summary", nil, 200)["stats"])
	if stats["applications"] != 0 || stats["members"] != 0 || stats["records"] != 0 || stats["files"] != 0 {
		t.Fatalf("another member's information leaked into summary: %+v", stats)
	}
}
