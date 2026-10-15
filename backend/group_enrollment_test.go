package main

import (
	"slices"
	"testing"
	"time"
)

func addGroupApplicationForTest(t *testing.T, a *App, user User, team Team, direction, status string) TeamApplication {
	t.Helper()
	ap := TeamApplication{ID: randomID(), TeamID: team.ID, UserID: user.ID, UserName: user.Name, Direction: direction, Status: status, CreatedAt: now(), UpdatedAt: now()}
	if err := a.store.exec("INSERT INTO team_applications(id,team_id,user_id,payload) VALUES(?,?,?,?)", ap.ID, ap.TeamID, ap.UserID, encoded(ap)); err != nil {
		t.Fatal(err)
	}
	return ap
}

func checkGroupAccessForTest(t *testing.T, c *testClient, allowed bool, applied, enrolled []string) {
	t.Helper()
	access := c.request("GET", "/api/learning-access", nil, 200)
	gotApplied := unmarshal[[]string](t, access["appliedDirections"])
	gotEnrolled := unmarshal[[]string](t, access["enrolledDirections"])
	slices.Sort(gotApplied)
	slices.Sort(gotEnrolled)
	applied = slices.Clone(applied)
	enrolled = slices.Clone(enrolled)
	slices.Sort(applied)
	slices.Sort(enrolled)
	if unmarshal[bool](t, access["allowed"]) != allowed || !slices.Equal(gotApplied, applied) || !slices.Equal(gotEnrolled, enrolled) {
		t.Fatalf("unexpected learning access: %+v, want allowed=%t applied=%v enrolled=%v", access, allowed, applied, enrolled)
	}
}

func TestIndependentGroupsAssignWorkAndPreserveOtherAssessmentAfterElimination(t *testing.T) {
	a, admin := setup(t)
	// Keep dashboard assertions independent of demo task counts.
	for _, query := range []string{"DELETE FROM submissions", "DELETE FROM tasks"} {
		if err := a.store.exec(query); err != nil {
			t.Fatal(err)
		}
	}
	admin.login("admin")
	mechanical := trainingTeamForTest(t, admin, "机械")
	control := trainingTeamForTest(t, admin, "电控")
	algorithm := trainingTeamForTest(t, admin, "视觉算法")
	hardware := trainingTeamForTest(t, admin, "硬件")
	otherControl := trainingTeamForTest(t, admin, "电控")
	user, mechanicalAP := trainingApplicantForTest(t, a, admin, mechanical, "机械", "independent-groups@test.local")
	controlAP := addGroupApplicationForTest(t, a, user, control, "电控", "pending")
	addGroupApplicationForTest(t, a, user, algorithm, "视觉算法", "pending")
	addGroupApplicationForTest(t, a, user, hardware, "硬件", "rejected")
	// A preference does not confer enrollment or star a group.
	user.Direction = "运营"
	if err := a.store.exec("UPDATE users SET payload=? WHERE id=?", encoded(user), user.ID); err != nil {
		t.Fatal(err)
	}
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	checkGroupAccessForTest(t, student, false, []string{"机械", "电控", "视觉算法"}, nil)

	makeTask := func(team Team, direction, category, starts, due string) Task {
		t.Helper()
		form := taskForm()
		form.TeamID, form.Direction, form.Kind, form.SubmissionType = team.ID, direction, "document", ""
		form.Category, form.StartsAt, form.DueAt, form.Required = category, starts, due, true
		return taskForTest(t, admin, form)
	}
	mechanicalTask := makeTask(mechanical, "机械", "assignment", "", "")
	controlTask := makeTask(control, "电控", "assignment", "", "")
	algorithmTask := makeTask(algorithm, "视觉算法", "assignment", "", "")
	unrelatedTask := makeTask(otherControl, "电控", "assignment", "", "")
	future := makeTask(control, "电控", "exam", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339))
	past := makeTask(control, "电控", "exam", time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339), time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	mechanicalProgress := admitForTest(t, a, admin, mechanicalAP)
	controlProgress := admitForTest(t, a, admin, controlAP)
	checkGroupAccessForTest(t, student, true, []string{"机械", "电控", "视觉算法"}, []string{"机械", "电控"})
	wrongScope := taskForm()
	wrongScope.TeamID, wrongScope.Direction = mechanical.ID, "电控"
	admin.request("POST", "/api/tasks", wrongScope, 400)
	stats := unmarshal[map[string]int](t, student.request("GET", "/api/workspace-summary", nil, 200)["stats"])
	if stats["activeTasks"] != 2 {
		t.Fatalf("dashboard counted browsing-only tasks: %v", stats)
	}
	for _, item := range unmarshal[[]Task](t, student.request("GET", "/api/tasks", nil, 200)["items"]) {
		switch item.ID {
		case mechanicalTask.ID, controlTask.ID:
			if !item.Assigned || !item.CanSubmit {
				t.Fatal("each accepted group must assign its open work")
			}
		case algorithmTask.ID, unrelatedTask.ID:
			if item.Assigned || item.CanSubmit {
				t.Fatal("pending enrollment or another team's work became required")
			}
		case future.ID:
			if !item.Assigned || item.CanSubmit || !item.Locked || item.Description != "" {
				t.Fatal("scheduled exam lost assignment or leaked future content")
			}
		case past.ID:
			if !item.Assigned || item.CanSubmit {
				t.Fatal("closed exam should retain assignment without permitting submissions")
			}
		}
	}
	student.request("POST", "/api/tasks/"+unrelatedTask.ID+"/submissions", map[string]string{"content": "同方向但不是本人团队"}, 403)
	student.request("POST", "/api/tasks/"+mechanicalTask.ID+"/submissions", map[string]string{"content": "机械成果"}, 201)
	student.request("POST", "/api/tasks/"+controlTask.ID+"/submissions", map[string]string{"content": "电控成果"}, 201)
	trainingDecisionForTest(admin, mechanicalProgress, "foundation", "passed", 200)
	trainingDecisionForTest(admin, controlProgress, "foundation", "passed", 200)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"机械", []string{"assessment", "recruitment"}}})
	mechanicalAdmin := &testClient{t: t, handler: admin.handler}
	mechanicalAdmin.login("mentor")
	trainingDecisionForTest(mechanicalAdmin, controlProgress, "phase", "eliminated", 403)
	trainingDecisionForTest(mechanicalAdmin, mechanicalProgress, "phase", "eliminated", 200)
	checkGroupAccessForTest(t, student, true, []string{"电控", "视觉算法"}, []string{"电控"})
	student.request("POST", "/api/tasks/"+mechanicalTask.ID+"/submissions", map[string]string{"content": "绕过本组淘汰"}, 403)
	student.request("POST", "/api/tasks/"+controlTask.ID+"/submissions", map[string]string{"content": "继续另一组训练"}, 201)
	trainingDecisionForTest(admin, controlProgress, "phase", "passed", 200)
	admin.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{controlProgress.ID}, "stageId": "final", "status": "passed", "targetMemberType": "formal"}, 200)
	checkGroupAccessForTest(t, student, true, []string{"电控", "视觉算法"}, []string{"电控"})
	student.request("POST", "/api/tasks/"+mechanicalTask.ID+"/submissions", map[string]string{"content": "正式身份不能恢复淘汰组"}, 403)
	mechanicalAdmin.request("PATCH", "/api/team-applications/"+mechanicalAP.ID, map[string]string{"status": "accepted"}, 409)
	mechanicalAdmin.request("PATCH", "/api/team-applications/"+mechanicalAP.ID, map[string]any{"status": "accepted", "reopenTraining": true}, 200)
	reopened, err := latestTrainingInScope(a.store, user.ID, mechanical.ID, "机械")
	if err != nil || reopened == nil || reopened.ID == mechanicalProgress.ID {
		t.Fatalf("missing re-admitted group cycle: %+v %v", reopened, err)
	}
	for _, stage := range reopened.Stages {
		trainingDecisionForTest(mechanicalAdmin, *reopened, stage.ID, "passed", 200)
	}
	me := unmarshal[User](t, student.request("GET", "/api/auth/me", nil, 200)["user"])
	if me.MemberType != "formal" {
		t.Fatal("completing a second group downgraded formal membership")
	}
	checkGroupAccessForTest(t, student, true, []string{"机械", "电控", "视觉算法"}, []string{"机械", "电控"})
	admin.request("PATCH", "/api/members/"+user.ID, map[string]string{"direction": "硬件"}, 400)
	admin.request("PATCH", "/api/members/"+user.ID, map[string]string{"direction": "电控"}, 200)
	checkGroupAccessForTest(t, student, true, []string{"机械", "电控", "视觉算法"}, []string{"机械", "电控"})
	for _, ap := range []TeamApplication{mechanicalAP, controlAP} {
		stored, err := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", ap.ID)
		if err != nil || stored.Direction != ap.Direction {
			t.Fatal("primary group change rewrote another group's application")
		}
	}
}

func TestGroupAssessmentContinuesAfterAnotherGroupPromotesMember(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	control := trainingTeamForTest(t, admin, "电控")
	mechanical := trainingTeamForTest(t, admin, "机械")
	user, controlAP := trainingApplicantForTest(t, a, admin, control, "电控", "parallel-final@test.local")
	mechanicalAP := addGroupApplicationForTest(t, a, user, mechanical, "机械", "pending")
	p1 := admitForTest(t, a, admin, controlAP)
	p2 := admitForTest(t, a, admin, mechanicalAP)
	for _, stage := range p1.Stages {
		trainingDecisionForTest(admin, p1, stage.ID, "passed", 200)
	}
	trainingDecisionForTest(admin, p2, "foundation", "passed", 200)
	trainingDecisionForTest(admin, p2, "phase", "eliminated", 200)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	checkGroupAccessForTest(t, student, true, []string{"电控"}, []string{"电控"})
	member := unmarshal[User](t, student.request("GET", "/api/auth/me", nil, 200)["user"])
	if member.MemberType != "reserve" {
		t.Fatal("another group's elimination revoked previously earned member identity")
	}
	// An established member still gets the new group's own training process.
	algorithm := trainingTeamForTest(t, admin, "视觉算法")
	algorithmAP := addGroupApplicationForTest(t, a, user, algorithm, "视觉算法", "pending")
	p3 := admitForTest(t, a, admin, algorithmAP)
	if p3.TeamID != algorithm.ID || p3.Status != "training" {
		t.Fatal("new group admission skipped group-specific assessment")
	}
}

func TestSameDirectionTeamsKeepIndependentTrainingAndTaskScope(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	firstTeam := trainingTeamForTest(t, admin, "电控")
	secondTeam := trainingTeamForTest(t, admin, "电控")
	user, firstAP := trainingApplicantForTest(t, a, admin, firstTeam, "电控", "same-direction@test.local")
	secondAP := addGroupApplicationForTest(t, a, user, secondTeam, "电控", "pending")
	first := admitForTest(t, a, admin, firstAP)
	second := admitForTest(t, a, admin, secondAP)
	trainingDecisionForTest(admin, first, "foundation", "eliminated", 200)
	trainingDecisionForTest(admin, second, "foundation", "passed", 200)
	form := taskForm()
	form.TeamID = firstTeam.ID
	oldTask := taskForTest(t, admin, form)
	form.TeamID = secondTeam.ID
	currentTask := taskForTest(t, admin, form)
	// A stale membership imported after elimination cannot reactivate that team,
	// even though the same direction remains effective through another team.
	stale := Membership{TeamID: firstTeam.ID, UserID: user.ID, Direction: "电控", JoinedAt: now()}
	if err := a.store.exec("INSERT INTO team_memberships(team_id,user_id,direction,joined_at,payload) VALUES(?,?,?,?,?)", stale.TeamID, stale.UserID, stale.Direction, stale.JoinedAt, encoded(stale)); err != nil {
		t.Fatal(err)
	}
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	checkGroupAccessForTest(t, student, true, []string{"电控"}, []string{"电控"})
	for _, task := range unmarshal[[]Task](t, student.request("GET", "/api/tasks", nil, 200)["items"]) {
		if task.ID == oldTask.ID && (task.Assigned || task.CanSubmit) {
			t.Fatal("same-direction active team reactivated eliminated team's task")
		}
		if task.ID == currentTask.ID && (!task.Assigned || !task.CanSubmit) {
			t.Fatal("same-direction elimination blocked independent team")
		}
	}
	student.request("POST", "/api/tasks/"+oldTask.ID+"/submissions", map[string]string{"content": "stale membership"}, 403)
}

func TestFinalBatchKeepsEachMembersPromotionIndependent(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控")
	priorTeam := trainingTeamForTest(t, admin, "机械")
	firstUser, priorAP := trainingApplicantForTest(t, a, admin, priorTeam, "机械", "batch-established@test.local")
	prior := admitForTest(t, a, admin, priorAP)
	for _, stage := range prior.Stages {
		admin.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{prior.ID}, "stageId": stage.ID, "status": "passed", "targetMemberType": "formal"}, 200)
	}
	firstAP := addGroupApplicationForTest(t, a, firstUser, team, "电控", "pending")
	first := admitForTest(t, a, admin, firstAP)
	secondUser, secondAP := trainingApplicantForTest(t, a, admin, team, "电控", "batch-still-freshman@test.local")
	second := admitForTest(t, a, admin, secondAP)
	for _, stage := range []string{"foundation", "phase"} {
		admin.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{first.ID, second.ID}, "stageId": stage, "status": "passed"}, 200)
	}
	admin.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{first.ID, second.ID}, "stageId": "final", "status": "passed", "targetMemberType": "reserve"}, 200)
	for _, expected := range []struct{ id, memberType string }{{firstUser.ID, "formal"}, {secondUser.ID, "reserve"}} {
		user, err := a.store.user(expected.id)
		if err != nil || user.MemberType != expected.memberType {
			t.Fatalf("batch promotion crossed member boundaries: got %+v err=%v want %s", user, err, expected.memberType)
		}
	}
}
