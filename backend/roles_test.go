package main

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

func grantForTest(t *testing.T, c *testClient, id string, grants []GroupGrant) {
	t.Helper()
	c.request("PATCH", "/api/members/"+id, map[string]any{"role": "group_admin", "groupGrants": grants}, 200)
}
func TestGroupGrantsScopeModulesConcurrentGroupsAndLiveRevocation(t *testing.T) {
	_, admin := setup(t)
	admin.login("admin")
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"tasks", "members", "recruitment", "records", "mail", "discussions"}}, {"机械", []string{"knowledge"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	me := unmarshal[User](t, staff.request("GET", "/api/auth/me", nil, 200)["user"])
	if me.Role != "group_admin" || len(me.GroupGrants) != 2 {
		t.Fatal("concurrent group grants not exposed")
	}
	staff.request("PATCH", "/api/settings", map[string]string{"organization": "forged"}, 403)
	staff.request("PATCH", "/api/members/demo-student", map[string]string{"role": "admin"}, 403)
	staff.request("POST", "/api/teams", map[string]any{"name": "forged"}, 403)
	task := taskForm()
	task.Title = "本人组作业"
	created := taskForTest(t, staff, task)
	task.Direction = "机械"
	task.Kind = "document"
	staff.request("POST", "/api/tasks", task, 403)
	task.Direction = ""
	staff.request("POST", "/api/tasks", task, 403)
	file := uploadForTest(t, staff, "mechanical.pdf", []byte("%PDF-1.7\nreference"), 201)
	resource := unmarshal[KnowledgeResource](t, staff.request("POST", "/api/knowledge-base", map[string]any{"title": "机械共享资料", "kind": "pdf", "direction": "机械", "fileIds": []string{file.ID}}, 201)["resource"])
	staff.request("POST", "/api/knowledge-base", map[string]any{"title": "电控资料", "kind": "pdf", "direction": "电控", "fileIds": []string{file.ID}}, 403)
	staff.request("PATCH", "/api/knowledge-base/"+resource.ID, map[string]string{"direction": "视觉算法"}, 403)
	source := uploadForTest(t, staff, "self.c", []byte("int main(){return 0;}"), 201)
	submitted := unmarshal[Submission](t, staff.request("POST", "/api/tasks/"+created.ID+"/submissions", map[string]any{"content": "本人测试", "fileIds": []string{source.ID}}, 201)["item"])
	staff.request("PATCH", "/api/submissions/"+submitted.ID, map[string]any{"status": "approved", "score": 100}, 403)
	mechanicalUsers := unmarshal[[]User](t, admin.request("GET", "/api/users", nil, 200)["items"])
	for _, user := range mechanicalUsers {
		if user.Direction == "机械" && user.ID != "demo-mentor" {
			members := unmarshal[[]User](t, staff.request("GET", "/api/members", nil, 200)["items"])
			for _, member := range members {
				if member.ID == user.ID {
					t.Fatal("members exposed other group")
				}
			}
		}
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{})
	staff.request("POST", "/api/tasks", taskForm(), 403)
	staff.request("POST", "/api/knowledge-base", map[string]any{"title": "机械资料", "kind": "pdf", "direction": "机械", "fileIds": []string{file.ID}}, 403)
	staff.request("GET", "/api/users?permission=mail", nil, 403)
	staff.request("GET", "/api/members", nil, 403)
	if !unmarshal[bool](t, staff.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("revoking management should retain personal learning")
	}
}

func TestFreshmanAdmissionAssessmentPromotionAndLastAdministrator(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	if err := a.store.exec("DELETE FROM applicants WHERE user_id=?", "demo-student"); err != nil {
		t.Fatal(err)
	}
	team := unmarshal[Team](t, admin.request("POST", "/api/teams", map[string]any{"name": "统一战队", "competitions": []string{"RoboCon", "RoboMaster"}, "directions": []string{"机械", "电控"}}, 201)["item"])
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	completeProfile(t, student)
	resume := uploadForTest(t, student, "resume.pdf", []byte("%PDF-1.7\nresume"), 201)
	application := unmarshal[TeamApplication](t, student.request("POST", "/api/teams/"+team.ID+"/applications", map[string]string{"direction": "电控", "resumeFileId": resume.ID}, 201)["item"])
	if unmarshal[bool](t, student.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("unapproved freshman has learning access")
	}
	admin.request("PATCH", "/api/team-applications/"+application.ID, map[string]string{"status": "accepted", "memberType": "formal"}, 400)
	admin.request("POST", "/api/interviews/"+application.ID+"/decision", map[string]string{"status": "passed"}, 200)
	me := unmarshal[User](t, student.request("GET", "/api/auth/me", nil, 200)["user"])
	if me.Role != "freshman" || me.MemberType != "freshman" {
		t.Fatal("admission should not promote freshman")
	}
	access := student.request("GET", "/api/learning-access", nil, 200)
	if !unmarshal[bool](t, access["allowed"]) || len(unmarshal[[]string](t, access["directions"])) != 5 {
		t.Fatal("admitted freshman cannot learn all groups")
	}
	admin.request("PATCH", "/api/members/demo-student", map[string]string{"memberType": "reserve"}, 409)
	admin.request("PATCH", "/api/members/demo-student", map[string]any{"memberType": "reserve", "assessmentPassed": true}, 409)
	progress := unmarshal[[]TrainingProgress](t, student.request("GET", "/api/training-progress", nil, 200)["items"])[0]
	for _, stage := range progress.Stages {
		admin.request("POST", "/api/training-progress/decisions", map[string]any{"progressIds": []string{progress.ID}, "stageId": stage.ID, "status": "passed", "note": "完成新生阶段考核"}, 200)
	}
	promoted := unmarshal[User](t, student.request("GET", "/api/auth/me", nil, 200)["user"])
	if promoted.Role != "reserve" || promoted.AssessmentPassedAt == "" || promoted.AssessmentConfirmedBy != "demo-admin" {
		t.Fatal("assessment promotion not recorded")
	}
	admin.request("PATCH", "/api/members/demo-student", map[string]string{"memberType": "formal", "direction": "机械"}, 200)
	memberships := unmarshal[[]Membership](t, student.request("GET", "/api/memberships", nil, 200)["items"])
	if len(memberships) != 1 || memberships[0].Direction != "机械" {
		t.Fatal("personal group change did not update membership")
	}
	current, err := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", application.ID)
	if err != nil || current.Direction != "机械" {
		t.Fatal("accepted application did not follow group change")
	}
	admin.request("PATCH", "/api/members/demo-admin", map[string]string{"role": "freshman"}, 409)
	admin.request("PATCH", "/api/members/demo-student", map[string]string{"role": "freshman"}, 200)
	admin.request("PATCH", "/api/team-applications/"+application.ID, map[string]string{"status": "rejected"}, 200)
	if unmarshal[bool](t, student.request("GET", "/api/learning-access", nil, 200)["allowed"]) {
		t.Fatal("revoked freshman admission still grants access")
	}
}

func TestMemberManagementSupportsMultipleGroupsAndAuditedForcePromotion(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	electrical := trainingTeamForTest(t, admin, "电控")
	mechanical := trainingTeamForTest(t, admin, "机械")
	user, application := trainingApplicantForTest(t, a, admin, electrical, "电控", "multi-group-promotion@test.local")
	progress := admitForTest(t, a, admin, application)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": user.Email, "password": trainingPassword}, 200)

	admin.request("PATCH", "/api/members/"+user.ID, map[string]any{"directions": []string{"电控", "机械"}}, 200)
	checkGroupAccessForTest(t, student, true, []string{"电控", "机械"}, []string{"电控", "机械"})
	admin.request("PATCH", "/api/members/"+user.ID, map[string]any{"directions": []string{"机械"}}, 200)
	checkGroupAccessForTest(t, student, true, []string{"机械"}, []string{"机械"})
	admin.request("PATCH", "/api/members/"+user.ID, map[string]any{"directions": []string{"电控", "机械"}}, 200)

	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"assessment"}}})
	manager := &testClient{t: t, handler: admin.handler}
	manager.login("mentor")
	manager.request("PATCH", "/api/members/"+user.ID, map[string]any{"memberType": "reserve", "forcePromotion": true, "assessmentNote": "越权尝试"}, 403)

	admin.request("PATCH", "/api/members/"+user.ID, map[string]any{
		"memberType": "reserve", "forcePromotion": true, "assessmentNote": "队务会议决定直接调整成员身份。",
		"directions": []string{"电控", "机械"},
	}, 200)
	promoted, err := readOne[User](a.store, "SELECT payload FROM users WHERE id=?", user.ID)
	if err != nil || promoted.Role != "reserve" || promoted.MemberType != "reserve" || promoted.PromotionOverrideAt == "" || promoted.PromotionOverrideBy != "demo-admin" || promoted.PromotionOverrideNote == "" {
		t.Fatalf("direct promotion was not audited: %+v (%v)", promoted, err)
	}
	storedProgress := trainingProgressForTest(t, a, progress.ID)
	if storedProgress.Status != "training" || len(storedProgress.Decisions) != 0 || promoted.AssessmentPassedAt != "" {
		t.Fatal("administrator override fabricated a passed training stage")
	}
	admin.request("PATCH", "/api/members/"+user.ID, map[string]any{"directions": []string{"机械"}}, 200)
	checkGroupAccessForTest(t, student, true, []string{"机械"}, []string{"机械"})
	admin.request("PATCH", "/api/members/"+user.ID, map[string]any{"directions": []string{"电控", "机械"}}, 200)
	for _, stage := range progress.Stages {
		trainingDecisionForTest(admin, progress, stage.ID, "passed", 200)
	}
	admin.request("PATCH", "/api/members/"+user.ID, map[string]any{"directions": []string{"机械"}}, 200)
	checkGroupAccessForTest(t, student, true, []string{"机械"}, []string{"机械"})
	current, err := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", application.ID)
	if err != nil || current.Status != "rejected" {
		t.Fatalf("removing a completed group did not release its accepted application: %+v (%v)", current, err)
	}
	if mechanical.ID == "" {
		t.Fatal("mechanical recruitment team was not created")
	}
}

func TestAllGroupLearningDoesNotExposeReviewsHiddenCasesOrFutureExams(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	task := taskForm()
	task.Direction = "视觉算法"
	task.SubmissionType = "code"
	task.Description = "ALGORITHM_BODY"
	task.Judge = &JudgeConfig{LanguageIDs: []int{71}, TestCases: []JudgeCase{{Input: "", ExpectedOutput: "public"}, {Input: "SECRET_CASE", ExpectedOutput: "hidden", Hidden: true}}}
	problem := taskForTest(t, admin, task)
	answer := uploadForTest(t, admin, "answer.zip", []byte("private submitted work"), 201)
	submission := Submission{ID: randomID(), TaskID: problem.ID, UserID: "demo-interviewer", FileIDs: []string{answer.ID}, Version: 1, Status: "pending", CreatedAt: now()}
	if err := a.store.exec("INSERT INTO submissions(id,task_id,user_id,version,payload) VALUES(?,?,?,?,?)", submission.ID, submission.TaskID, submission.UserID, submission.Version, encoded(submission)); err != nil {
		t.Fatal(err)
	}
	examFile := uploadForTest(t, admin, "exam.pdf", []byte("%PDF-1.7\nsecret exam"), 201)
	exam := taskForm()
	exam.Direction = "机械"
	exam.Kind = "document"
	exam.Category = "exam"
	exam.StartsAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	exam.DueAt = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	exam.FileIDs = []string{examFile.ID}
	exam.Description = "SECRET_FUTURE_EXAM"
	scheduled := taskForTest(t, admin, exam)
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	listed := unmarshal[[]Task](t, student.request("GET", "/api/tasks?direction="+url.QueryEscape("视觉算法"), nil, 200)["items"])
	found := false
	for _, item := range listed {
		if item.ID == problem.ID {
			found = true
			if item.CanSubmit || item.Judge == nil || len(item.Judge.TestCases) != 1 {
				t.Fatal("cross-group reader got submit permission or hidden case")
			}
		}
	}
	if !found {
		t.Fatal("cross-group learning missing")
	}
	if len(unmarshal[[]Submission](t, student.request("GET", "/api/tasks/"+problem.ID+"/submissions", nil, 200)["items"])) != 0 {
		t.Fatal("cross-group learner saw others' submissions")
	}
	student.request("GET", "/api/files/"+answer.ID, nil, 403)
	student.request("POST", "/api/tasks/"+problem.ID+"/submissions", map[string]string{"content": "forged"}, 403)
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	staff.request("PATCH", "/api/submissions/"+submission.ID, map[string]any{"status": "approved", "score": 90}, 403)
	staff.request("GET", "/api/files/"+answer.ID, nil, 403)
	listed = unmarshal[[]Task](t, staff.request("GET", "/api/tasks?direction="+url.QueryEscape("机械"), nil, 200)["items"])
	for _, item := range listed {
		if item.ID == scheduled.ID && (!item.Locked || item.Description != "" || len(item.FileIDs) != 0) {
			t.Fatal("group admin of another group saw unreleased exam")
		}
	}
	staff.request("GET", "/api/files/"+examFile.ID, nil, 403)
}

func TestScopedHonorMailRecipientsAndRecruitmentFiles(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"records", "mail", "recruitment"}}})
	electrical := unmarshal[User](t, admin.request("POST", "/api/members", map[string]any{"name": "电控队员", "email": "electrical@scope.test", "password": "ScopePassword!2026", "role": "member", "memberType": "formal", "direction": "电控"}, 201)["item"])
	mechanical := unmarshal[User](t, admin.request("POST", "/api/members", map[string]any{"name": "机械队员", "email": "mechanical@scope.test", "password": "ScopePassword!2026", "role": "member", "memberType": "formal", "direction": "机械"}, 201)["item"])
	outsiderFile := uploadForTest(t, admin, "mechanical-certificate.pdf", []byte("%PDF-1.7\ncertificate"), 201)
	outsiderRecord := unmarshal[Record](t, admin.request("POST", "/api/records", map[string]any{"userId": mechanical.ID, "title": "机械荣誉", "kind": "award", "fileIds": []string{outsiderFile.ID}}, 201)["item"])
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	recipients := unmarshal[[]User](t, staff.request("GET", "/api/users?permission=mail", nil, 200)["items"])
	found := false
	for _, user := range recipients {
		if user.ID == mechanical.ID {
			t.Fatal("out-of-scope mail recipient leaked")
		}
		if user.ID == electrical.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("in-scope recipient missing")
	}
	staff.request("POST", "/api/mail-campaigns", map[string]any{"subject": "越界", "body": "测试", "userIds": []string{mechanical.ID}}, 403)
	campaign := unmarshal[MailCampaign](t, staff.request("POST", "/api/mail-campaigns", map[string]any{"subject": "本组通知", "body": "测试", "userIds": []string{electrical.ID}}, 201)["item"])
	staff.request("GET", "/api/mail-campaigns/"+campaign.ID, nil, 200)
	staff.request("POST", "/api/records", map[string]any{"userId": mechanical.ID, "title": "越界荣誉", "kind": "award"}, 403)
	certificate := uploadForTest(t, staff, "certificate.pdf", []byte("%PDF-1.7\ncertificate"), 201)
	staff.request("POST", "/api/records", map[string]any{"userId": electrical.ID, "title": "本组荣誉", "kind": "award", "fileIds": []string{certificate.ID}}, 201)
	records := unmarshal[[]Record](t, staff.request("GET", "/api/records", nil, 200)["items"])
	for _, record := range records {
		if record.ID == outsiderRecord.ID {
			t.Fatal("out-of-scope honor leaked")
		}
	}
	staff.request("GET", "/api/files/"+outsiderFile.ID, nil, 403)
	// Direction reassignment removes access to previously valid recipient campaigns.
	admin.request("PATCH", "/api/members/"+electrical.ID, map[string]string{"direction": "机械"}, 200)
	staff.request("GET", "/api/mail-campaigns/"+campaign.ID, nil, 403)
	// Recruitment-only users receive the shared pool of authorized reviewers.
	reviewers := unmarshal[[]User](t, staff.request("GET", "/api/users?permission=recruitment", nil, 200)["items"])
	foundAdmin := false
	for _, user := range reviewers {
		if user.ID == "demo-admin" {
			foundAdmin = true
		}
	}
	if !foundAdmin {
		t.Fatal("recruitment administrator list omitted global administrator")
	}
	_ = a
}

func TestMailDeliveryRechecksActualSenderAfterRevocation(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"mail"}}})
	userIDs := []string{}
	for _, email := range []string{"first@scope.test", "second@scope.test"} {
		member := unmarshal[User](t, admin.request("POST", "/api/members", map[string]any{"name": email, "email": email, "password": "ScopePassword!2026", "role": "member", "direction": "电控"}, 201)["item"])
		userIDs = append(userIDs, member.ID)
	}
	// The global administrator creates the draft, but the delegated sender's
	// live authorization must control delivery of every remaining recipient.
	campaign := unmarshal[MailCampaign](t, admin.request("POST", "/api/mail-campaigns", map[string]any{"subject": "权限撤销测试", "body": "本地邮件替身", "userIds": userIDs}, 201)["item"])
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	mailer := &noticeFixture{block: release, started: started}
	a.config.NoticeMailer = mailer
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	staff.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 202)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("mailer not called")
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{})
	close(release)
	delivered := waitCampaign(t, admin, campaign.ID)
	if delivered.Status != "partial" || delivered.Recipients[0].Status != "sent" || delivered.Recipients[1].Status != "failed" {
		t.Fatalf("remaining deliveries survived sender revocation: %#v", delivered)
	}
	if mailer.count("first@scope.test") != 1 || mailer.count("second@scope.test") != 0 {
		t.Fatal("worker checked draft creator rather than actual sender")
	}
}

func TestRecipientPickersRequireProfileModulePermission(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	member := unmarshal[User](t, admin.request("POST", "/api/members", map[string]any{"name": "收件人", "email": "recipient@scope.test", "password": "ScopePassword!2026", "role": "member", "direction": "电控"}, 201)["item"])
	member.AssessmentNote = "PRIVATE_ASSESSMENT"
	if err := a.store.exec("UPDATE users SET payload=? WHERE id=?", encoded(member), member.ID); err != nil {
		t.Fatal(err)
	}
	profile := Profile{UserID: member.ID, Name: member.Name, Phone: "PRIVATE_PHONE", Hobbies: "PRIVATE_HOBBIES", StudentNumber: "20260123", PhotoFileID: "PRIVATE_PHOTO", ClassName: "PRIVATE_CLASS"}
	if err := a.store.saveProfile(profile); err != nil {
		t.Fatal(err)
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"mail", "records"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	type pickerUser struct {
		User
		Profile Profile `json:"profile"`
	}
	for _, permission := range []string{"mail", "records"} {
		users := unmarshal[[]pickerUser](t, staff.request("GET", "/api/users?permission="+permission, nil, 200)["items"])
		found := false
		for _, user := range users {
			if user.ID != member.ID {
				continue
			}
			found = true
			if user.Name != member.Name || user.Email != member.Email || user.Profile.Phone != "" || user.Profile.Hobbies != "" || user.Profile.PhotoFileID != "" || user.Profile.ClassName != "" || user.AssessmentNote != "" {
				t.Fatalf("%s picker exposed unrelated profile information: %#v", permission, user)
			}
			if permission == "records" && user.Profile.StudentNumber != profile.StudentNumber || permission == "mail" && user.Profile.StudentNumber != "" {
				t.Fatalf("%s picker returned incorrect identity fields", permission)
			}
		}
		if !found {
			t.Fatal("authorized recipient missing")
		}
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"mail", "records", "members"}}})
	users := unmarshal[[]pickerUser](t, staff.request("GET", "/api/users?permission=mail", nil, 200)["items"])
	for _, user := range users {
		if user.ID == member.ID && (user.Profile.Phone != profile.Phone || user.AssessmentNote != member.AssessmentNote) {
			t.Fatal("explicit profile module grant did not restore full details")
		}
	}
}

func TestLegacyRolesPersistentlyMigrateWithoutGlobalPrivilege(t *testing.T) {
	a, c := setup(t)
	cases := []User{{ID: "legacy-mentor", Name: "旧导师", Email: "legacy-mentor@test.local", Role: "mentor", Direction: "机械"}, {ID: "legacy-reviewer", Name: "旧面试官", Email: "legacy-reviewer@test.local", Role: "interviewer", Direction: "电控"}, {ID: "legacy-blank", Name: "无组导师", Email: "legacy-blank@test.local", Role: "mentor"}, {ID: "legacy-student", Name: "旧学员", Email: "legacy-student@test.local", Role: "student", Direction: "机械"}}
	for _, user := range cases {
		if err := a.store.exec("INSERT INTO users(id,email,password_hash,payload) VALUES(?,?,?,?)", user.ID, user.Email, "unused", encoded(user)); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateRoles(a.store); err != nil {
		t.Fatal(err)
	}
	for _, old := range cases {
		var raw string
		if err := a.store.row("SELECT payload FROM users WHERE id=?", old.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var user User
		if err := json.Unmarshal([]byte(raw), &user); err != nil {
			t.Fatal(err)
		}
		if old.Role == "student" {
			if user.Role != "freshman" || user.MemberType != "freshman" {
				t.Fatal("legacy student promotion changed")
			}
			continue
		}
		if user.Role != "group_admin" || hasPermission(user, "", "tasks") {
			t.Fatal("legacy staff retained global privileges")
		}
		if old.Direction == "" && len(user.GroupGrants) != 0 {
			t.Fatal("blank direction gained implicit permissions")
		}
		if old.Role == "mentor" && old.Direction != "" && (!hasPermission(user, old.Direction, "tasks") || hasPermission(user, "电控", "tasks")) {
			t.Fatal("legacy mentor scope wrong")
		}
		if old.Role == "interviewer" && (!hasPermission(user, "电控", "recruitment") || hasAnyPermission(user, "tasks")) {
			t.Fatal("legacy interviewer modules wrong")
		}
	}
	c.login("admin")
	for _, role := range []string{"evil", "mentor", "interviewer"} {
		c.request("POST", "/api/members", map[string]string{"name": "invalid", "email": "invalid@test.local", "password": "ScopePassword!2026", "role": role}, 400)
	}
	c.request("POST", "/api/members", map[string]string{"name": "invalid", "email": "invalid@test.local", "password": "ScopePassword!2026", "role": "member", "memberType": "freshman"}, 400)
	c.request("PATCH", "/api/members/demo-mentor", map[string]any{"groupGrants": []GroupGrant{{"机械", []string{"settings"}}}}, 400)
	c.request("PATCH", "/api/members/demo-mentor", map[string]any{"groupGrants": []GroupGrant{{"机械", []string{"tasks"}}, {"机械", []string{"mail"}}}}, 400)
}
