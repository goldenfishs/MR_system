package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type interviewMailer struct {
	mu     sync.Mutex
	bodies []string
}

func (m *interviewMailer) SendMessage(_ context.Context, _, _, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bodies = append(m.bodies, body)
	return nil
}
func TestInterviewAdmissionSchedulePrivacyAndMail(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "运营")
	otherTeam := trainingTeamForTest(t, admin, "电控")
	student := &testClient{t: t, handler: admin.handler}
	student.login("student")
	completeProfile(t, student)
	resume := uploadForTest(t, student, "resume.pdf", []byte("%PDF-1.7 resume"), 201)
	ap := unmarshal[TeamApplication](t, student.request("POST", "/api/teams/"+team.ID+"/applications", map[string]any{"direction": "运营", "resumeFileId": resume.ID}, 201)["item"])
	if ap.Interview == nil || ap.Interview.Status != "pending" {
		t.Fatal("registration must enter interview")
	}
	admin.request("PATCH", "/api/team-applications/"+ap.ID, map[string]string{"status": "accepted"}, 409)
	if p, _ := latestTrainingInScope(a.store, ap.UserID, team.ID, "运营"); p != nil {
		t.Fatal("premature admission")
	}
	_, otherAP := trainingApplicantForTest(t, a, admin, otherTeam, "电控", "other-interview@test.local")
	visible := unmarshal[[]TeamApplication](t, student.request("GET", "/api/interviews", nil, 200)["items"])
	if len(visible) != 1 || visible[0].ID != ap.ID {
		t.Fatal("private applications leaked")
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"运营", []string{"recruitment", "mail"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	schedule := map[string]any{"applicationIds": []string{ap.ID, otherAP.ID}, "startAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339), "room": "教学楼 A201", "intervalMinutes": 10, "startOrder": 3}
	staff.request("PATCH", "/api/interviews/schedule", schedule, 403)
	unchanged, _ := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", ap.ID)
	if unchanged.Interview.At != "" {
		t.Fatal("partial unauthorized batch")
	}
	schedule["applicationIds"] = []string{ap.ID}
	staff.request("PATCH", "/api/interviews/schedule", schedule, 200)
	campaign := unmarshal[MailCampaign](t, staff.request("POST", "/api/interviews/notifications", map[string]any{"applicationIds": []string{ap.ID}}, 201)["item"])
	if len(campaign.Recipients) != 1 || !strings.Contains(campaign.Recipients[0].Body, "A201") || !strings.Contains(campaign.Recipients[0].Body, "面试序号：3") {
		t.Fatal("incorrect personalized notification")
	}
	mailer := &interviewMailer{}
	a.config.NoticeMailer = mailer
	schedule["room"] = "B301"
	staff.request("PATCH", "/api/interviews/schedule", schedule, 200)
	staff.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 409)
	campaign = unmarshal[MailCampaign](t, staff.request("POST", "/api/interviews/notifications", map[string]any{"applicationIds": []string{ap.ID}}, 201)["item"])
	staff.request("POST", "/api/mail-campaigns/"+campaign.ID+"/send", nil, 202)
	waitCampaign(t, staff, campaign.ID)
	mailer.mu.Lock()
	if len(mailer.bodies) != 1 || !strings.Contains(mailer.bodies[0], "B301") {
		t.Fatal("personalized body not delivered")
	}
	mailer.mu.Unlock()
	staff.request("POST", "/api/interviews/"+otherAP.ID+"/decision", map[string]string{"status": "passed"}, 403)
	staff.request("POST", "/api/interviews/"+ap.ID+"/decision", map[string]string{"status": "passed", "note": "面试通过"}, 200)
	staff.request("POST", "/api/interviews/"+ap.ID+"/decision", map[string]string{"status": "passed"}, 409)
	p, _ := latestTrainingInScope(a.store, ap.UserID, team.ID, "运营")
	if p == nil || p.Status != "training" {
		t.Fatal("passed interview must create training")
	}
	var count int
	a.store.row("SELECT COUNT(*) FROM team_memberships WHERE team_id=? AND user_id=?", team.ID, ap.UserID).Scan(&count)
	if count != 1 {
		t.Fatal("missing membership")
	}
	if p, _ := latestTrainingInScope(a.store, otherAP.UserID, otherTeam.ID, "电控"); p != nil {
		t.Fatal("other group changed")
	}
}
func TestInterviewLegacyRoundPreservesHistoryAndFutureDefaults(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	team := trainingTeamForTest(t, c, "机械", "运营")
	_, mechanical := trainingApplicantForTest(t, a, c, team, "机械", "legacy-mech@test.local")
	_, ops := trainingApplicantForTest(t, a, c, team, "运营", "legacy-ops@test.local")
	_, accepted := trainingApplicantForTest(t, a, c, team, "机械", "legacy-accepted@test.local")
	old := admitForTest(t, a, c, accepted)
	for i := 0; i < 2; i++ {
		c.request("POST", "/api/interviews/legacy-round", map[string]any{"teamIds": []string{team.ID}}, 200)
	}
	m, _ := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", mechanical.ID)
	o, _ := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", ops.ID)
	if m.Interview.Status != "exempt" || o.Interview.Status != "pending" {
		t.Fatal("exception scope incorrect")
	}
	c.request("PATCH", "/api/team-applications/"+ops.ID, map[string]string{"status": "accepted"}, 409)
	c.request("PATCH", "/api/team-applications/"+mechanical.ID, map[string]string{"status": "accepted"}, 200)
	if encoded(trainingProgressForTest(t, a, old.ID)) != encoded(old) {
		t.Fatal("migration changed training history")
	}
	future := trainingTeamForTest(t, c, "机械")
	if len(future.InterviewExemptDirections) > 0 {
		t.Fatal("future team inherited exception")
	}
}
