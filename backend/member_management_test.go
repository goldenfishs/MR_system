package main

import "testing"

func memberManagementForm(email string, directions []string) map[string]any {
	return map[string]any{"name": "多组新生", "email": email, "password": trainingPassword, "role": "freshman", "memberType": "freshman", "directions": directions}
}

func TestMemberManagementCreateMultipleIndependentGroups(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	mechanical := trainingTeamForTest(t, admin, "机械")
	electrical := trainingTeamForTest(t, admin, "电控")
	u := unmarshal[User](t, admin.request("POST", "/api/members", memberManagementForm("multi-create@test.local", []string{"电控", "机械"}), 201)["item"])
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": trainingPassword}, 200)
	checkGroupAccessForTest(t, student, true, []string{"机械", "电控"}, []string{"机械", "电控"})
	memberships := unmarshal[[]Membership](t, student.request("GET", "/api/memberships", nil, 200)["items"])
	if len(memberships) != 2 {
		t.Fatalf("expected two independent memberships: %+v", memberships)
	}
	progress, err := currentTraining(a.store, u.ID)
	if err != nil || len(progress) != 2 {
		t.Fatalf("expected two independent training cycles: %+v (%v)", progress, err)
	}
	var mechanicalProgress TrainingProgress
	for _, p := range progress {
		if p.Status != "training" || len(p.Decisions) != 0 || len(p.Stages) == 0 {
			t.Fatalf("invalid initial training: %+v", p)
		}
		if p.TeamID == mechanical.ID && p.Direction == "机械" {
			mechanicalProgress = p
		} else if p.TeamID != electrical.ID || p.Direction != "电控" {
			t.Fatalf("unexpected training scope: %+v", p)
		}
	}
	if mechanicalProgress.ID == "" {
		t.Fatal("mechanical training was not created")
	}
	trainingDecisionForTest(admin, mechanicalProgress, mechanicalProgress.Stages[0].ID, "eliminated", 200)
	checkGroupAccessForTest(t, student, true, []string{"电控"}, []string{"电控"})
	for _, p := range progress {
		if p.TeamID == electrical.ID && trainingProgressForTest(t, a, p.ID).Status != "training" {
			t.Fatal("eliminating mechanical enrollment affected electrical training")
		}
	}
}

func TestMemberManagementMultiDirectionTeamCannotOverwriteMembership(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	trainingTeamForTest(t, admin, "机械", "电控")
	counts := func() map[string]int {
		t.Helper()
		out := map[string]int{}
		for _, table := range []string{"users", "team_memberships", "training_progress", "training_plans", "team_applications"} {
			var count int
			if err := a.store.row("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			out[table] = count
		}
		return out
	}
	before := counts()
	admin.request("POST", "/api/members", memberManagementForm("atomic-multigroup@test.local", []string{"机械", "电控"}), 400)
	for table, count := range counts() {
		if before[table] != count {
			t.Fatalf("failed multi-group creation left partial %s changes: %d -> %d", table, before[table], count)
		}
	}
	// A failed attempt must release the email and permit a valid single-group retry.
	u := unmarshal[User](t, admin.request("POST", "/api/members", memberManagementForm("atomic-multigroup@test.local", []string{"机械"}), 201)["item"])
	before = counts()
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{"机械", "电控"}}, 400)
	for table, count := range counts() {
		if before[table] != count {
			t.Fatalf("failed multi-group edit left partial %s changes", table)
		}
	}
	memberships, err := readAll[Membership](a.store, "SELECT payload FROM team_memberships WHERE user_id=?", u.ID)
	if err != nil || len(memberships) != 1 || memberships[0].Direction != "机械" {
		t.Fatalf("multi-direction team silently overwrote original membership: %+v (%v)", memberships, err)
	}
}

func TestMemberManagementForcePromotionRequiresReasonAndAdmin(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	trainingTeamForTest(t, admin, "机械")
	u := unmarshal[User](t, admin.request("POST", "/api/members", memberManagementForm("force-reason@test.local", []string{"机械"}), 201)["item"])
	for _, note := range []string{"", " \n\t"} {
		admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"memberType": "formal", "forcePromotion": true, "assessmentNote": note}, 400)
	}
	stored, err := a.store.user(u.ID)
	if err != nil || stored.MemberType != "freshman" || stored.PromotionOverrideAt != "" {
		t.Fatalf("invalid override partially changed user: %+v (%v)", stored, err)
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"机械", []string{"assessment", "members"}}})
	manager := &testClient{t: t, handler: admin.handler}
	manager.login("mentor")
	manager.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{"机械"}, "memberType": "formal", "assessmentPassed": true}, 403)
	manager.request("PATCH", "/api/members/"+u.ID, map[string]any{"memberType": "formal", "forcePromotion": true, "assessmentNote": "组长请求"}, 403)
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"memberType": "formal", "forcePromotion": true, "assessmentNote": "  管理员确认转正式队员  "}, 200)
	stored, err = a.store.user(u.ID)
	if err != nil || stored.MemberType != "formal" || stored.Role != "member" || stored.PromotionOverrideBy != "demo-admin" || stored.PromotionOverrideAt == "" || stored.PromotionOverrideNote != "管理员确认转正式队员" || stored.AssessmentPassedAt != "" {
		t.Fatalf("direct promotion must audit override without fabricating assessment: %+v (%v)", stored, err)
	}
}

func TestMemberManagementCannotReaddEliminatedGroup(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "机械")
	u, application := trainingApplicantForTest(t, a, admin, team, "机械", "eliminated-edit@test.local")
	p := admitForTest(t, a, admin, application)
	trainingDecisionForTest(admin, p, p.Stages[0].ID, "eliminated", 200)
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{"机械"}}, 409)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": trainingPassword}, 200)
	checkGroupAccessForTest(t, student, false, nil, nil)
	progress, err := currentTraining(a.store, u.ID)
	if err != nil || len(progress) != 1 || progress[0].ID != p.ID || progress[0].Status != "eliminated" {
		t.Fatalf("failed reassignment altered eliminated cycle: %+v (%v)", progress, err)
	}
}

func TestMemberManagementRemovingLegacyGroupRevokesAccess(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	u := unmarshal[User](t, admin.request("POST", "/api/members", memberManagementForm("legacy-remove@test.local", []string{}), 201)["item"])
	legacy := Applicant{ID: randomID(), UserID: u.ID, Direction: "机械", Status: "accepted", CreatedAt: now(), UpdatedAt: now()}
	if err := a.store.exec("INSERT INTO applicants(id,user_id,payload) VALUES(?,?,?)", legacy.ID, legacy.UserID, encoded(legacy)); err != nil {
		t.Fatal(err)
	}
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": trainingPassword}, 200)
	checkGroupAccessForTest(t, student, true, []string{"机械"}, []string{"机械"})
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{}}, 200)
	checkGroupAccessForTest(t, student, false, nil, nil)
	stored, err := readOne[Applicant](a.store, "SELECT payload FROM applicants WHERE id=?", legacy.ID)
	if err != nil || stored.Status != "rejected" || stored.Notes == "" {
		t.Fatalf("legacy removal must revoke accepted status with explanation: %+v (%v)", stored, err)
	}
}

func TestAdminAdjustActiveGroupsPreservesHistoryAndRollsBack(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	mechanical := trainingTeamForTest(t, admin, "机械")
	trainingTeamForTest(t, admin, "电控")
	trainingTeamForTest(t, admin, "视觉算法")
	u, ap := trainingApplicantForTest(t, a, admin, mechanical, "机械", "transfer@test.local")
	p := admitForTest(t, a, admin, ap)
	trainingDecisionForTest(admin, p, p.Stages[0].ID, "passed", 200)
	p = trainingProgressForTest(t, a, p.ID)
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{"机械", "电控"}}, 200)
	before, _ := currentTraining(a.store, u.ID)
	// No operations may survive when adding the replacement group fails.
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{"运营"}}, 400)
	storedApp, _ := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", ap.ID)
	if storedApp.Status != "accepted" {
		t.Fatal("failed transfer revoked original application")
	}
	if active, _ := trainingEnrollmentActive(a.store, p); !active {
		t.Fatal("failed transfer removed membership")
	}
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{"电控", "视觉算法"}}, 200)
	if active, _ := trainingEnrollmentActive(a.store, p); active {
		t.Fatal("removed group still active")
	}
	if got := trainingProgressForTest(t, a, p.ID); encoded(got) != encoded(p) {
		t.Fatal("removed group's assessment history changed")
	}
	for _, old := range before {
		if old.Direction == "电控" && encoded(trainingProgressForTest(t, a, old.ID)) != encoded(old) {
			t.Fatal("parallel group changed")
		}
	}
	storedApp, _ = readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", ap.ID)
	if storedApp.Status != "rejected" || storedApp.Feedback == "" {
		t.Fatal("removed application not reconciled")
	}
	trainingDecisionForTest(admin, p, p.CurrentStageID, "passed", 409)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": trainingPassword}, 200)
	checkGroupAccessForTest(t, student, true, []string{"电控", "视觉算法"}, []string{"电控", "视觉算法"})
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{"机械", "电控", "视觉算法"}}, 200)
	restored, _ := latestTrainingInScope(a.store, u.ID, mechanical.ID, "机械")
	if restored.ID != p.ID || encoded(*restored) != encoded(p) {
		t.Fatal("restoring group lost previous progress")
	}
	stored, _ := a.store.user(u.ID)
	if stored.Role != u.Role || stored.MemberType != u.MemberType || len(stored.GroupGrants) != len(u.GroupGrants) {
		t.Fatal("group change altered identity or grants")
	}
	admin.request("PATCH", "/api/members/"+u.ID, map[string]any{"directions": []string{}}, 200)
	checkGroupAccessForTest(t, student, false, nil, nil)
}
