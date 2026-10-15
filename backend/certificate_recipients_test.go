package main

import (
	"slices"
	"testing"
)

func TestCertificateRecipientsExposeOnlyEligibleGroups(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	mechanical := certificateMemberForTest(t, a, admin, "机械同学", "mech@recipients.test", "机械")
	control := certificateMemberForTest(t, a, admin, "控制同学", "ctrl@recipients.test", "电控")
	freshman := unmarshal[User](t, admin.request("POST", "/api/members", map[string]any{"name": "未录取新生", "email": "new@recipients.test", "password": "Recipient!2026", "role": "freshman", "memberType": "freshman", "direction": "机械"}, 201)["item"])
	team := unmarshal[Team](t, admin.request("POST", "/api/teams", map[string]any{"name": "控制测试团队", "description": "证书发放有效组别", "directions": []string{"电控"}, "competitions": []string{"RoboCon"}, "status": "open"}, 201)["item"])
	membership := Membership{UserID: mechanical.ID, TeamID: team.ID, Direction: "电控", JoinedAt: now()}
	if err := a.store.exec("INSERT INTO team_memberships(team_id,user_id,direction,joined_at,payload) VALUES(?,?,?,?,?)", membership.TeamID, membership.UserID, membership.Direction, membership.JoinedAt, encoded(membership)); err != nil {
		t.Fatal(err)
	}
	type candidate struct {
		User
		EligibleDirections []string `json:"eligibleDirections"`
	}
	all := unmarshal[[]candidate](t, admin.request("GET", "/api/users?permission=records", nil, 200)["items"])
	for _, user := range all {
		if user.ID == mechanical.ID && (!slices.Contains(user.EligibleDirections, "机械") || !slices.Contains(user.EligibleDirections, "电控")) {
			t.Fatal("multi-group member should be eligible for both active groups")
		}
		if user.ID == freshman.ID && len(user.EligibleDirections) != 0 {
			t.Fatal("preferred direction is not an active enrollment")
		}
	}
	staff := unmarshal[User](t, admin.request("POST", "/api/members", map[string]any{"name": "机械荣誉管理", "email": "staff@recipients.test", "password": "Recipient!2026", "role": "group_admin", "groupGrants": []GroupGrant{{Direction: "机械", Permissions: []string{"records"}}}}, 201)["item"])
	client := &testClient{t: t, handler: a.Handler()}
	client.request("POST", "/api/auth/login", map[string]string{"email": staff.Email, "password": "Recipient!2026"}, 200)
	limited := unmarshal[[]candidate](t, client.request("GET", "/api/users?permission=records", nil, 200)["items"])
	found := false
	for _, user := range limited {
		if user.ID == control.ID || user.ID == freshman.ID {
			t.Fatal("records picker exposed an unauthorized or unadmitted recipient")
		}
		if user.ID == mechanical.ID {
			found = true
			if len(user.EligibleDirections) != 1 || user.EligibleDirections[0] != "机械" {
				t.Fatal("picker exposed a group outside the issuer's records grant")
			}
		}
	}
	if !found {
		t.Fatal("authorized multi-group recipient missing")
	}
}
