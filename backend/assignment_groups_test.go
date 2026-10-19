package main

import "testing"

func TestAssignmentGroupValidation(t *testing.T) {
	base := []TaskQuestion{{ID: "a", GroupID: "g1", GroupTitle: "图像信息", GroupDescription: "共同题干"}, {ID: "b", GroupID: "g1", GroupTitle: "图像信息", GroupDescription: "共同题干"}, {ID: "c"}}
	if err := validateQuestionGroups(base); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"title", "order", "missing", "source"} {
		qs := append([]TaskQuestion{}, base...)
		switch kind {
		case "title":
			qs[1].GroupTitle = "不同标题"
		case "order":
			qs[1], qs[2] = qs[2], qs[1]
		case "missing":
			qs[0].GroupID = ""
		case "source":
			qs[0].SourceProblemID = "../../bad"
		}
		if validateQuestionGroups(qs) == nil {
			t.Errorf("accepted invalid %s", kind)
		}
	}
}

func TestAssignmentGroupSnapshotAndSourceFileScope(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	asset := uploadForTest(t, admin, "prompt.txt", []byte("training prompt"), 201)
	source := taskForm()
	source.Direction = "电控"
	source.Category = "problem"
	source.Kind = "document"
	source.SubmissionType = ""
	source.Judge = nil
	source.FileIDs = []string{asset.ID}
	source.BankVisibility = "internal"
	source.MechanicalProblem = &MechanicalProblem{Type: "single_choice", Options: []string{"A", "B"}, Answers: []string{"0"}}
	bank := taskForTest(t, admin, source)
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "电控", Permissions: []string{"tasks"}}})
	manager := &testClient{t: t, handler: admin.handler}
	manager.login("mentor")
	form := taskForm()
	form.Direction = "电控"
	form.Category = "assignment"
	form.Kind = "document"
	form.SubmissionType = "files"
	form.Judge = nil
	form.Status = "draft"
	form.MaxScore = 20
	form.Questions = []TaskQuestion{{ID: "one", GroupID: "g1", GroupTitle: "复合大题", SourceProblemID: bank.ID, Title: "选择", Description: "复制题干", Type: "single_choice", Options: []string{"A", "B"}, Answers: []string{"0"}, AutoGrade: true, FileIDs: []string{asset.ID}, MaxScore: 10}, {ID: "two", GroupID: "g1", GroupTitle: "复合大题", Title: "说明", Description: "解释理由", Type: "short_answer", MaxScore: 10}}
	assignment := taskForTest(t, manager, form)
	manager.request("POST", "/api/tasks/"+assignment.ID+"/publish", nil, 200)
	admin.request("PATCH", "/api/tasks/"+bank.ID, map[string]any{"description": "修改源题"}, 200)
	saved, err := a.store.task(assignment.ID)
	if err != nil || saved.Questions[0].Description != "复制题干" || saved.Questions[1].GroupTitle != "复合大题" {
		t.Fatal("snapshot or grouping changed", err)
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{Direction: "机械", Permissions: []string{"tasks"}}})
	form.Direction = "机械"
	manager.request("POST", "/api/tasks", form, 403)
}
