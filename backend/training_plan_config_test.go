package main

import "testing"

func TestTrainingPlanSyncOnlyActiveUnassessedScope(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "电控", "机械")
	otherTeam := trainingTeamForTest(t, admin, "电控")
	admit := func(team Team, group, email string) TrainingProgress {
		_, ap := trainingApplicantForTest(t, a, admin, team, group, email+"@test.local")
		return admitForTest(t, a, admin, ap)
	}
	untouched := admit(team, "电控", "untouched")
	assessed := admit(team, "电控", "assessed")
	eliminated := admit(team, "电控", "eliminated")
	completed := admit(team, "电控", "completed")
	revoked := admit(team, "电控", "revoked")
	mechanical := admit(team, "机械", "mechanical")
	other := admit(otherTeam, "电控", "other")
	trainingDecisionForTest(admin, assessed, "foundation", "passed", 200)
	trainingDecisionForTest(admin, eliminated, "foundation", "eliminated", 200)
	for _, stage := range completed.Stages {
		trainingDecisionForTest(admin, completed, stage.ID, "passed", 200)
	}
	if err := a.store.exec("DELETE FROM team_memberships WHERE user_id=?", revoked.UserID); err != nil {
		t.Fatal(err)
	}
	snapshots := map[string]string{}
	for _, p := range []TrainingProgress{assessed, eliminated, completed, revoked, mechanical, other} {
		snapshots[p.ID] = encoded(trainingProgressForTest(t, a, p.ID))
	}
	stages := []TrainingStage{{Title: "C语言基础"}, {Title: "单片机实践"}, {Title: "项目验收", Final: true}}
	result := admin.request("POST", "/api/training-plans", map[string]any{"teamId": team.ID, "direction": "电控", "title": "自定义培养", "stages": stages, "applyToUnassessed": true}, 201)
	if unmarshal[int](t, result["syncedCount"]) != 1 {
		t.Fatalf("wrong sync count: %v", result["syncedCount"])
	}
	plan := unmarshal[TrainingPlan](t, result["item"])
	current := trainingProgressForTest(t, a, untouched.ID)
	if current.PlanID != plan.ID || current.CurrentStageID != plan.Stages[0].ID || current.Stages[1].Title != "单片机实践" || len(current.Decisions) != 0 {
		t.Fatal("unassessed cycle not migrated")
	}
	for id, snapshot := range snapshots {
		if encoded(trainingProgressForTest(t, a, id)) != snapshot {
			t.Fatalf("protected cycle changed: %s", id)
		}
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"机械", []string{"assessment"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	staff.request("PATCH", "/api/training-plans/"+plan.ID, map[string]any{"title": "越权", "applyToUnassessed": true}, 403)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"assessment"}}})
	stages = []TrainingStage{{Title: "唯一阶段", Final: true}}
	result = staff.request("PATCH", "/api/training-plans/"+plan.ID, map[string]any{"title": "新版", "stages": stages, "applyToUnassessed": true}, 200)
	if unmarshal[int](t, result["syncedCount"]) != 1 {
		t.Fatal("delegated sync failed")
	}
	current = trainingProgressForTest(t, a, untouched.ID)
	if len(current.Stages) != 1 || !current.Stages[0].Final || current.PlanTitle != "新版" {
		t.Fatal("stage count not configurable")
	}
	// An invalid plan cannot partly change either the plan or a student's progress.
	before := encoded(current)
	staff.request("PATCH", "/api/training-plans/"+plan.ID, map[string]any{"stages": []TrainingStage{}, "applyToUnassessed": true}, 400)
	if encoded(trainingProgressForTest(t, a, untouched.ID)) != before {
		t.Fatal("invalid save changed progress")
	}
	trainingDecisionForTest(staff, current, current.CurrentStageID, "passed", 200)
	if trainingProgressForTest(t, a, current.ID).Status != "completed" {
		t.Fatal("migrated final stage cannot be assessed")
	}
}
