package main

import (
	"net/url"
	"testing"
)

const trainingPassword = "TrainingPassword!2026"

func trainingTeamForTest(t *testing.T, c *testClient, directions ...string) Team {
	t.Helper()
	return unmarshal[Team](t, c.request("POST", "/api/teams", map[string]any{"name": "新生培养战队", "competitions": []string{"RoboCon", "RoboMaster"}, "directions": directions}, 201)["item"])
}

// Prepare a pending application directly so these tests can focus on admission,
// stage transactions and authorization independently of file upload validation.
func trainingApplicantForTest(t *testing.T, a *App, c *testClient, team Team, direction, email string) (User, TeamApplication) {
	t.Helper()
	u := unmarshal[User](t, c.request("POST", "/api/members", map[string]any{"name": email, "email": email, "password": trainingPassword, "role": "freshman", "memberType": "freshman"}, 201)["item"])
	ap := TeamApplication{ID: randomID(), TeamID: team.ID, UserID: u.ID, UserName: u.Name, Direction: direction, Status: "pending", CreatedAt: now(), UpdatedAt: now()}
	if err := a.store.exec("INSERT INTO team_applications(id,team_id,user_id,payload) VALUES(?,?,?,?)", ap.ID, ap.TeamID, ap.UserID, encoded(ap)); err != nil {
		t.Fatal(err)
	}
	return u, ap
}

func admitForTest(t *testing.T, a *App, c *testClient, ap TeamApplication) TrainingProgress {
	t.Helper()
	c.request("PATCH", "/api/team-applications/"+ap.ID, map[string]string{"status": "accepted"}, 200)
	p, err := latestTraining(a.store, ap.UserID)
	if err != nil || p == nil {
		t.Fatalf("admission did not persist training progress: %v", err)
	}
	return *p
}

func trainingProgressForTest(t *testing.T, a *App, id string) TrainingProgress {
	t.Helper()
	p, err := readOne[TrainingProgress](a.store, "SELECT payload FROM training_progress WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func trainingDecisionForTest(c *testClient, p TrainingProgress, stage, status string, expected int) {
	c.t.Helper()
	c.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{p.ID}, "stageId": stage, "status": status, "note": "保存考核说明"}, expected)
}

func TestTrainingTeamListIncludesPersonalProgress(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控")
	user, ap := trainingApplicantForTest(t, a, admin, team, "电控", "team-list-training@test.local")
	p := admitForTest(t, a, admin, ap)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	check := func(status, stage string, cycle int) {
		t.Helper()
		teams := unmarshal[[]Team](t, student.request("GET", "/api/teams", nil, 200)["items"])
		for _, listed := range teams {
			if listed.ID != team.ID {
				continue
			}
			personal := listed.MyApplication
			if personal == nil || personal.ID != ap.ID || personal.Status != "accepted" || personal.TrainingStatus != status || personal.TrainingStageTitle != stage || personal.TrainingCycle != cycle {
				t.Fatalf("team list lost personal training projection: %+v", personal)
			}
			return
		}
		t.Fatal("team missing from participant list")
	}
	check("training", p.Stages[0].Title, 1)
	trainingDecisionForTest(admin, p, "foundation", "passed", 200)
	check("training", p.Stages[1].Title, 1)
	trainingDecisionForTest(admin, p, "phase", "eliminated", 200)
	check("eliminated", p.Stages[1].Title, 1)
	admin.request("PATCH", "/api/team-applications/"+ap.ID, map[string]any{"status": "accepted", "reopenTraining": true}, 200)
	current, err := latestTraining(a.store, user.ID)
	if err != nil || current == nil {
		t.Fatalf("readmission did not persist progress: %v", err)
	}
	check("training", current.Stages[0].Title, 2)
	for _, stage := range current.Stages {
		trainingDecisionForTest(admin, *current, stage.ID, "passed", 200)
	}
	check("completed", current.Stages[len(current.Stages)-1].Title, 2)
	for _, listed := range unmarshal[[]Team](t, admin.request("GET", "/api/teams", nil, 200)["items"]) {
		if listed.MyApplication != nil {
			t.Fatal("team list exposed another user's personal application")
		}
	}
}

func TestTrainingPlanSnapshotsSequentialStagesAndDelegatedFinal(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控", "机械")
	plan := unmarshal[TrainingPlan](t, admin.request("POST", "/api/training-plans", map[string]any{"teamId": team.ID, "direction": "电控", "title": "控制组培养", "stages": []TrainingStage{{ID: "learn", Title: "学习"}, {ID: "trial", Title: "阶段测评"}, {ID: "final", Title: "最终考核", Final: true}}}, 201)["item"])
	first, ap := trainingApplicantForTest(t, a, admin, team, "电控", "first-training@test.local")
	p := admitForTest(t, a, admin, ap)
	admin.request("PATCH", "/api/training-plans/"+plan.ID, map[string]any{"title": "新版计划", "stages": []TrainingStage{{ID: "learn", Title: "新版学习"}, {ID: "final", Title: "新版最终考核", Final: true}}}, 200)
	_, secondAP := trainingApplicantForTest(t, a, admin, team, "电控", "second-training@test.local")
	second := admitForTest(t, a, admin, secondAP)
	stored := trainingProgressForTest(t, a, p.ID)
	if len(stored.Stages) != 3 || stored.Stages[0].Title != "学习" || len(second.Stages) != 2 || second.PlanTitle != "新版计划" || second.Stages[0].Order != 1 {
		t.Fatal("editing a plan changed an admitted student's snapshot or failed to affect later admissions")
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"assessment"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	staff.request("POST", "/api/training-plans", map[string]any{"direction": "机械", "title": "越权", "stages": []TrainingStage{{Title: "最终考核", Final: true}}}, 403)
	admin.request("PATCH", "/api/members/"+first.ID, map[string]any{"memberType": "formal", "assessmentPassed": true}, 409)
	admin.request("PATCH", "/api/members/"+first.ID, map[string]any{"role": "admin", "memberType": "formal", "assessmentPassed": true}, 409)
	admin.request("PATCH", "/api/members/"+first.ID, map[string]string{"direction": "机械"}, 409)
	grantForTest(t, admin, first.ID, []GroupGrant{{"电控", []string{"tasks", "assessment"}}})
	self := &testClient{t: t, handler: admin.handler}
	self.request("POST", "/api/auth/login", map[string]string{"email": first.Email, "password": trainingPassword}, 200)
	trainingDecisionForTest(self, p, "learn", "passed", 403)
	trainingDecisionForTest(staff, p, "final", "passed", 409)
	trainingDecisionForTest(staff, p, "learn", "passed", 200)
	trainingDecisionForTest(staff, p, "learn", "passed", 409)
	trainingDecisionForTest(staff, p, "trial", "passed", 200)
	staff.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{p.ID}, "stageId": "final", "status": "passed", "targetMemberType": "formal", "note": "完成全部考核"}, 200)
	member := unmarshal[User](t, self.request("GET", "/api/auth/me", nil, 200)["user"])
	stored = trainingProgressForTest(t, a, p.ID)
	if member.MemberType != "formal" || member.Role != "group_admin" || len(member.GroupGrants) != 1 || member.AssessmentConfirmedBy != "demo-mentor" || member.AssessmentNote != "完成全部考核" || stored.Status != "completed" || len(stored.Decisions) != 3 || stored.Decisions[2].MemberType != "formal" || stored.Decisions[2].DecidedByName == "" || stored.Decisions[2].DecidedAt == "" {
		t.Fatal("delegated final assessment failed to preserve role grants, member identity or audit")
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{})
	trainingDecisionForTest(staff, second, "learn", "passed", 403)
}

func TestTrainingBatchIsAtomicAndRejectsMixedFinalSemantics(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控", "机械")
	_, firstAP := trainingApplicantForTest(t, a, admin, team, "电控", "batch-first@test.local")
	first := admitForTest(t, a, admin, firstAP)
	if plans := unmarshal[[]TrainingPlan](t, admin.request("GET", "/api/training-plans", nil, 200)["items"]); len(plans) != 0 || first.PlanID != "" || len(first.Stages) != 3 {
		t.Fatal("unconfigured admission failed to create the default snapshot or forged a configured plan")
	}
	_, secondAP := trainingApplicantForTest(t, a, admin, team, "电控", "batch-second@test.local")
	second := admitForTest(t, a, admin, secondAP)
	trainingDecisionForTest(admin, second, "foundation", "passed", 200)
	admin.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{first.ID, second.ID}, "stageId": "foundation", "status": "passed"}, 409)
	if len(trainingProgressForTest(t, a, first.ID).Decisions) != 0 {
		t.Fatal("invalid second candidate failed to roll back the first candidate")
	}
	admin.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{first.ID, first.ID}, "stageId": "foundation", "status": "passed"}, 400)
	_, mechanicalAP := trainingApplicantForTest(t, a, admin, team, "机械", "batch-mechanical@test.local")
	mechanical := admitForTest(t, a, admin, mechanicalAP)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"assessment"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	staff.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{first.ID, mechanical.ID}, "stageId": "foundation", "status": "passed"}, 403)
	if len(trainingProgressForTest(t, a, first.ID).Decisions) != 0 {
		t.Fatal("cross-group batch partially committed")
	}
	trainingDecisionForTest(staff, first, "foundation", "passed", 200)
	staff.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{first.ID, second.ID}, "stageId": "phase", "status": "passed", "note": "批量阶段审核"}, 200)
	for _, p := range []TrainingProgress{first, second} {
		if trainingProgressForTest(t, a, p.ID).CurrentStageID != "final" {
			t.Fatal("valid batch did not advance all participants")
		}
	}
	_, ordinaryAP := trainingApplicantForTest(t, a, admin, team, "电控", "ordinary-stage@test.local")
	ordinary := admitForTest(t, a, admin, ordinaryAP)
	admin.request("POST", "/api/training-plans", map[string]any{"teamId": team.ID, "direction": "电控", "title": "单阶段考核", "stages": []TrainingStage{{ID: "foundation", Title: "直接最终考核", Final: true}}}, 201)
	_, finalAP := trainingApplicantForTest(t, a, admin, team, "电控", "final-stage@test.local")
	final := admitForTest(t, a, admin, finalAP)
	admin.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{ordinary.ID, final.ID}, "stageId": "foundation", "status": "passed"}, 409)
	if len(trainingProgressForTest(t, a, ordinary.ID).Decisions) != 0 || len(trainingProgressForTest(t, a, final.ID).Decisions) != 0 {
		t.Fatal("mixed normal and final stage semantics partially advanced or promoted candidates")
	}
}

func TestTrainingEliminationRevokesPersonalAccessButRetainsManagementAndHistory(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控")
	user, ap := trainingApplicantForTest(t, a, admin, team, "电控", "eliminated@test.local")
	p := admitForTest(t, a, admin, ap)
	legacy := Applicant{ID: randomID(), UserID: user.ID, Name: user.Name, Direction: "电控", Status: "accepted"}
	if err := a.store.exec("INSERT INTO applicants(id,user_id,payload) VALUES(?,?,?)", legacy.ID, user.ID, encoded(legacy)); err != nil {
		t.Fatal(err)
	}
	honor := unmarshal[Record](t, admin.request("POST", "/api/records", map[string]any{"userId": user.ID, "title": "面试通过证明", "kind": "certificate"}, 201)["item"])
	grantForTest(t, admin, user.ID, []GroupGrant{{"电控", []string{"tasks", "knowledge", "assessment"}}})
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	file := uploadForTest(t, admin, "elimination-reference.pdf", []byte("%PDF-1.7\nreference"), 201)
	admin.request("POST", "/api/knowledge-base", map[string]any{"title": "本组管理资料", "direction": "电控", "kind": "pdf", "fileIds": []string{file.ID}}, 201)
	task := taskForTest(t, admin, taskForm())
	trainingDecisionForTest(admin, p, "foundation", "eliminated", 200)
	if unmarshal[bool](t, student.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("eliminated freshman regained learning through old accepted application or staff role")
	}
	student.request("POST", "/api/tasks/"+task.ID+"/submissions", map[string]string{"content": "越过淘汰"}, 403)
	student.request("PATCH", "/api/tasks/"+task.ID, map[string]string{"title": "保留获授权管理功能"}, 200)
	downloadForTest(t, student, file.ID, "preview", 200)
	listed := unmarshal[[]KnowledgeResource](t, student.request("GET", "/api/knowledge-base", nil, 200)["resources"])
	for _, resource := range listed {
		if resource.Direction != "电控" {
			t.Fatal("eliminated manager's knowledge grant opened other groups")
		}
	}
	if len(unmarshal[[]Membership](t, student.request("GET", "/api/memberships", nil, 200)["items"])) != 0 {
		t.Fatal("eliminated participant retained active team membership")
	}
	records := unmarshal[[]Record](t, student.request("GET", "/api/records", nil, 200)["items"])
	if len(records) != 1 || records[0].ID != honor.ID {
		t.Fatal("elimination destroyed participant honor history")
	}
	applications := unmarshal[[]TeamApplication](t, student.request("GET", "/api/teams/"+team.ID+"/applications", nil, 200)["items"])
	if len(applications) != 1 || applications[0].Status != "accepted" || applications[0].TrainingStatus != "eliminated" || applications[0].TrainingCycle != 1 {
		t.Fatal("elimination did not preserve and annotate the original accepted application")
	}
	admin.request("PATCH", "/api/members/"+user.ID, map[string]any{"memberType": "reserve", "assessmentPassed": true}, 409)
	admin.request("PATCH", "/api/team-applications/"+ap.ID, map[string]string{"status": "accepted"}, 409)
	// Imported historical elimination must not revoke an established member's identity.
	member, err := a.store.user(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	member.MemberType = "reserve"
	if err = a.store.exec("UPDATE users SET payload=? WHERE id=?", encoded(member), member.ID); err != nil {
		t.Fatal(err)
	}
	if !unmarshal[bool](t, student.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("old elimination incorrectly revoked established reserve identity")
	}
}

func TestTrainingReadmissionRequiresOwnAssessmentGrantAndPreservesOtherGroups(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "机械")
	user, oldAP := trainingApplicantForTest(t, a, admin, team, "机械", "readmission@test.local")
	old := admitForTest(t, a, admin, oldAP)
	trainingDecisionForTest(admin, old, "foundation", "eliminated", 200)
	otherTeam := trainingTeamForTest(t, admin, "电控")
	newAP := TeamApplication{ID: randomID(), TeamID: otherTeam.ID, UserID: user.ID, UserName: user.Name, Direction: "电控", Status: "pending", CreatedAt: now(), UpdatedAt: now()}
	if err := a.store.exec("INSERT INTO team_applications(id,team_id,user_id,payload) VALUES(?,?,?,?)", newAP.ID, newAP.TeamID, newAP.UserID, encoded(newAP)); err != nil {
		t.Fatal(err)
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"recruitment"}}, {"机械", []string{"recruitment"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	// Another group's first admission is independent of the old elimination.
	staff.request("PATCH", "/api/team-applications/"+newAP.ID, map[string]string{"status": "accepted"}, 200)
	current, err := latestTrainingInScope(a.store, user.ID, otherTeam.ID, "电控")
	if err != nil || current == nil || current.Cycle != 2 || current.Status != "training" {
		t.Fatalf("independent group admission failed: %+v, %v", current, err)
	}
	staff.request("PATCH", "/api/team-applications/"+oldAP.ID, map[string]string{"status": "accepted"}, 409)
	staff.request("PATCH", "/api/team-applications/"+oldAP.ID, map[string]any{"status": "accepted", "reopenTraining": true}, 403)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"机械", []string{"recruitment", "assessment"}}})
	staff.request("PATCH", "/api/team-applications/"+oldAP.ID, map[string]any{"status": "accepted", "reopenTraining": true}, 200)
	reopened, err := latestTrainingInScope(a.store, user.ID, team.ID, "机械")
	if err != nil || reopened == nil || reopened.Cycle != 3 || reopened.Status != "training" || len(reopened.Decisions) != 0 {
		t.Fatalf("readmission did not start a fresh scoped cycle: %+v, %v", reopened, err)
	}
	if historical := trainingProgressForTest(t, a, old.ID); historical.Status != "eliminated" || len(historical.Decisions) != 1 {
		t.Fatal("readmission changed historical elimination")
	}
	trainingDecisionForTest(admin, *current, "foundation", "passed", 200)
	trainingDecisionForTest(staff, old, "foundation", "passed", 409)
	trainingDecisionForTest(staff, *reopened, "foundation", "passed", 200)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	access := student.request("GET", "/api/learning-access", nil, 200)
	if !unmarshal[bool](t, access["allowed"]) || len(unmarshal[[]string](t, access["enrolledDirections"])) != 2 {
		t.Fatal("both groups should remain independently active")
	}
	if len(unmarshal[[]TrainingProgress](t, student.request("GET", "/api/training-progress", nil, 200)["items"])) != 3 {
		t.Fatal("independent admission lost prior cycle history")
	}
	oldApplications := unmarshal[[]TeamApplication](t, staff.request("GET", "/api/teams/"+team.ID+"/applications", nil, 200)["items"])
	if len(oldApplications) != 1 || oldApplications[0].TrainingCycle != 3 || oldApplications[0].TrainingStatus != "training" {
		t.Fatal("recruiter did not see their own group's current cycle")
	}
}

func TestTrainingRevokedAdmissionCannotFinishOrChangeActiveGroup(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控", "机械")
	user, ap := trainingApplicantForTest(t, a, admin, team, "电控", "revoked-training@test.local")
	p := admitForTest(t, a, admin, ap)
	legacy := Applicant{ID: randomID(), UserID: user.ID, Name: user.Name, Direction: "机械", Status: "accepted"}
	if err := a.store.exec("INSERT INTO applicants(id,user_id,payload) VALUES(?,?,?)", legacy.ID, user.ID, encoded(legacy)); err != nil {
		t.Fatal(err)
	}
	admin.request("PATCH", "/api/members/"+user.ID, map[string]string{"direction": "机械"}, 409)
	trainingDecisionForTest(admin, p, "foundation", "passed", 200)
	trainingDecisionForTest(admin, p, "phase", "passed", 200)
	admin.request("PATCH", "/api/team-applications/"+ap.ID, map[string]string{"status": "rejected"}, 200)
	trainingDecisionForTest(admin, p, "final", "passed", 409)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	if unmarshal[bool](t, student.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("an obsolete legacy application bypassed current cycle admission revocation")
	}
	admin.request("PATCH", "/api/team-applications/"+ap.ID, map[string]string{"status": "accepted"}, 200)
	trainingDecisionForTest(admin, p, "final", "passed", 200)
	if current, err := latestTraining(a.store, user.ID); err != nil || current == nil || current.ID != p.ID || current.Cycle != 1 || current.Status != "completed" {
		t.Fatal("restoring admission replaced the existing active cycle")
	}
}

func TestLegacyAssessmentCanBeDelegatedWithoutGrantingRoleAdministration(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控")
	user, _ := trainingApplicantForTest(t, a, admin, team, "电控", "legacy-assessment@test.local")
	legacy := Applicant{ID: randomID(), UserID: user.ID, Name: user.Name, Direction: "电控", Status: "accepted"}
	if err := a.store.exec("INSERT INTO applicants(id,user_id,payload) VALUES(?,?,?)", legacy.ID, user.ID, encoded(legacy)); err != nil {
		t.Fatal(err)
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"assessment"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	staff.request("PATCH", "/api/members/"+user.ID, map[string]any{"memberType": "reserve", "assessmentPassed": true, "role": "admin"}, 403)
	promoted := unmarshal[User](t, staff.request("PATCH", "/api/members/"+user.ID, map[string]any{"memberType": "reserve", "assessmentPassed": true, "assessmentNote": "旧制最终考核"}, 200)["item"])
	if promoted.Role != "reserve" || promoted.MemberType != "reserve" || promoted.AssessmentConfirmedBy != "demo-mentor" || promoted.AssessmentPassedAt == "" {
		t.Fatal("legacy assessment delegation failed to persist the confirmation audit")
	}
	staff.request("PATCH", "/api/members/"+user.ID, map[string]any{"memberType": "formal", "assessmentPassed": true}, 403)
	listed := unmarshal[[]TrainingProgress](t, staff.request("GET", "/api/training-progress?direction="+url.QueryEscape("机械"), nil, 200)["items"])
	if len(listed) != 0 {
		t.Fatal("assessment scope exposed other group training")
	}
}

func TestFreshmanGroupManagerCanApplyButGrantsDoNotReplaceAdmission(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控")
	user := unmarshal[User](t, admin.request("POST", "/api/members", map[string]any{"name": "兼任组管理新生", "email": "fresh-manager@test.local", "password": trainingPassword, "role": "group_admin", "memberType": "freshman", "groupGrants": []GroupGrant{{"电控", []string{"knowledge", "recruitment", "assessment"}}}}, 201)["item"])
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)
	if unmarshal[bool](t, student.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("management grants substituted for personal admission")
	}
	completeProfile(t, student)
	resume := uploadForTest(t, student, "fresh-manager-resume.pdf", []byte("%PDF-1.7\nresume"), 201)
	ap := unmarshal[TeamApplication](t, student.request("POST", "/api/teams/"+team.ID+"/applications", map[string]string{"direction": "电控", "resumeFileId": resume.ID}, 201)["item"])
	if ap.UserID != user.ID || ap.Status != "pending" {
		t.Fatal("group manager could not participate in personal admission")
	}
	student.request("PATCH", "/api/team-applications/"+ap.ID, map[string]string{"status": "accepted"}, 403)
	admin.request("POST", "/api/interviews/"+ap.ID+"/decision", map[string]string{"status": "passed"}, 200)
	if !unmarshal[bool](t, student.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("admitted freshman manager did not receive personal learning access")
	}
	progress := unmarshal[[]TrainingProgress](t, student.request("GET", "/api/training-progress", nil, 200)["items"])[0]
	trainingDecisionForTest(student, progress, progress.CurrentStageID, "passed", 403)
}
