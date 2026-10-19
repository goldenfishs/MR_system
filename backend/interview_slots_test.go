package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func slotsForTest(t *testing.T, c *testClient, team Team, direction, room string, start time.Time, count, capacity int) []InterviewSlot {
	t.Helper()
	return unmarshal[[]InterviewSlot](t, c.request("POST", "/api/interview-slots/batch", map[string]any{"teamId": team.ID, "direction": direction, "startAt": start.UTC().Format(time.RFC3339), "intervalMinutes": 30, "count": count, "capacity": capacity, "room": room}, 201)["items"])
}
func slotStudent(t *testing.T, admin *testClient, u User) *testClient {
	t.Helper()
	c := &testClient{t: t, handler: admin.handler}
	c.request("POST", "/api/auth/login", map[string]string{"email": u.Email, "password": trainingPassword}, 200)
	return c
}
func storedInterviewApplication(t *testing.T, a *App, id string) TeamApplication {
	t.Helper()
	ap, err := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	return ap
}
func TestInterviewSlotsBookingPrivacyRescheduleAndCapacity(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "运营")
	other := trainingTeamForTest(t, admin, "电控")
	u, ap := trainingApplicantForTest(t, a, admin, team, "运营", "slots-first@test.local")
	v, bp := trainingApplicantForTest(t, a, admin, team, "运营", "slots-second@test.local")
	_, foreign := trainingApplicantForTest(t, a, admin, other, "电控", "slots-foreign@test.local")
	start := time.Now().Add(48 * time.Hour).Truncate(time.Minute)
	slots := slotsForTest(t, admin, team, "运营", "A201", start, 3, 1)
	slotsForTest(t, admin, other, "电控", "B201", start, 1, 1)
	student := slotStudent(t, admin, u)
	second := slotStudent(t, admin, v)
	student.request("POST", "/api/interview-slots/batch", map[string]any{"teamId": team.ID, "direction": "运营", "startAt": start.UTC().Format(time.RFC3339), "intervalMinutes": 30, "count": 1, "capacity": 1, "room": "C201"}, 403)
	student.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": bp.ID}, 403)
	student.request("GET", "/api/interview-slots?applicationId="+foreign.ID, nil, 403)
	available := student.request("GET", "/api/interview-slots", nil, 200)
	items := unmarshal[[]InterviewSlot](t, available["items"])
	if len(items) != 3 {
		t.Fatalf("foreign scope leaked: %d", len(items))
	}
	for _, forbidden := range []string{v.Email, bp.ID, foreign.ID, "userName", "resumeFileId", "profile"} {
		if strings.Contains(encoded(available), forbidden) {
			t.Fatalf("slot API leaked %s", forbidden)
		}
	}
	booked := unmarshal[TeamApplication](t, student.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": ap.ID}, 200)["item"])
	if booked.Interview.SlotID != slots[0].ID || booked.Interview.Room != "A201" || booked.Interview.DurationMinutes != 30 {
		t.Fatal("incorrect interview snapshot")
	}
	student.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": ap.ID}, 200)
	second.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": bp.ID}, 409)
	second.request("POST", "/api/interview-slots/"+slots[1].ID+"/book", map[string]string{"applicationId": bp.ID}, 200)
	student.request("POST", "/api/interview-slots/"+slots[1].ID+"/book", map[string]string{"applicationId": ap.ID}, 409)
	if storedInterviewApplication(t, a, ap.ID).Interview.SlotID != slots[0].ID {
		t.Fatal("failed reschedule lost reservation")
	}
	admin.request("PATCH", "/api/interview-slots/"+slots[0].ID, map[string]any{"room": "Changed", "capacity": 2}, 409)
	if got := unmarshal[[]InterviewSlot](t, admin.request("GET", "/api/interview-slots", nil, 200)["items"]); got[0].Room == "Changed" {
		t.Fatal("invalid edit partially persisted")
	}
	admin.request("PATCH", "/api/interview-slots/"+slots[2].ID, map[string]bool{"closed": true}, 200)
	student.request("POST", "/api/interview-slots/"+slots[2].ID+"/book", map[string]string{"applicationId": ap.ID}, 409)
	admin.request("PATCH", "/api/interview-slots/"+slots[2].ID, map[string]bool{"closed": false}, 200)
	student.request("POST", "/api/interview-slots/"+slots[2].ID+"/book", map[string]string{"applicationId": ap.ID}, 200)
	second.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": bp.ID}, 200)
	admin.request("PATCH", "/api/interview-slots/"+slots[2].ID, map[string]bool{"closed": true}, 200)
	closedOwn := unmarshal[[]InterviewSlot](t, student.request("GET", "/api/interview-slots", nil, 200)["items"])
	if len(closedOwn) != 3 {
		t.Fatal("own closed reservation disappeared")
	}
	second.request("DELETE", "/api/interviews/"+ap.ID+"/booking", nil, 403)
	student.request("DELETE", "/api/interviews/"+ap.ID+"/booking", nil, 200)
	if storedInterviewApplication(t, a, ap.ID).Interview.At != "" {
		t.Fatal("cancellation retained schedule")
	}
	student.request("DELETE", "/api/interviews/"+ap.ID+"/booking", nil, 409)
	// Managers may allocate a slot; reducing capacity below the occupied count fails.
	admin.request("PATCH", "/api/interview-slots/"+slots[0].ID, map[string]int{"capacity": 2}, 200)
	admin.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": ap.ID}, 200)
	admin.request("PATCH", "/api/interview-slots/"+slots[0].ID, map[string]int{"capacity": 1}, 409)
	if storedInterviewApplication(t, a, ap.ID).Interview.Order != 2 {
		t.Fatal("slot sequence duplicated")
	}
}

func TestInterviewSlotsCrossGroupConflictsAndExistingWorkflows(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "运营")
	other := trainingTeamForTest(t, admin, "电控")
	user, ap := trainingApplicantForTest(t, a, admin, team, "运营", "slots-multi@test.local")
	bp := TeamApplication{ID: randomID(), TeamID: other.ID, UserID: user.ID, UserName: user.Name, Direction: "电控", Status: "pending", CreatedAt: now(), UpdatedAt: now()}
	if err := a.store.exec("INSERT INTO team_applications(id,team_id,user_id,payload) VALUES(?,?,?,?)", bp.ID, bp.TeamID, bp.UserID, encoded(bp)); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(48 * time.Hour).Truncate(time.Minute)
	slots := slotsForTest(t, admin, team, "运营", "A201", start, 2, 1)
	others := slotsForTest(t, admin, other, "电控", "B201", start.Add(15*time.Minute), 2, 1)
	student := slotStudent(t, admin, user)
	student.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": ap.ID}, 200)
	student.request("POST", "/api/interview-slots/"+others[0].ID+"/book", map[string]string{"applicationId": bp.ID}, 409)
	student.request("POST", "/api/interview-slots/"+others[1].ID+"/book", map[string]string{"applicationId": bp.ID}, 200)
	student.request("POST", "/api/interview-slots/"+others[1].ID+"/book", map[string]string{"applicationId": ap.ID}, 400)
	manual := map[string]any{"applicationIds": []string{ap.ID}, "startAt": start.Add(50 * time.Minute).UTC().Format(time.RFC3339), "room": "C301", "intervalMinutes": 10, "startOrder": 7}
	admin.request("PATCH", "/api/interviews/schedule", manual, 409)
	if storedInterviewApplication(t, a, ap.ID).Interview.SlotID != slots[0].ID {
		t.Fatal("conflicting manual schedule removed reservation")
	}
	manual["startAt"] = start.Add(90 * time.Minute).UTC().Format(time.RFC3339)
	admin.request("PATCH", "/api/interviews/schedule", manual, 200)
	if v := storedInterviewApplication(t, a, ap.ID).Interview; v.SlotID != "" || v.Order != 7 || v.DurationMinutes != 10 {
		t.Fatal("manual schedule did not release reservation")
	}
	view := unmarshal[[]InterviewSlot](t, admin.request("GET", "/api/interview-slots?teamId="+team.ID, nil, 200)["items"])
	if view[0].Booked != 0 || view[0].Remaining != 1 {
		t.Fatal("manual schedule did not restore vacancy")
	}
	admin.request("POST", "/api/interviews/"+bp.ID+"/decision", map[string]string{"status": "rejected"}, 200)
	student.request("POST", "/api/interview-slots/"+others[0].ID+"/book", map[string]string{"applicationId": bp.ID}, 409)
	historical := storedInterviewApplication(t, a, bp.ID).Interview
	if historical.SlotID != others[1].ID || historical.Status != "rejected" {
		t.Fatal("decision lost scheduling history")
	}
	view = unmarshal[[]InterviewSlot](t, admin.request("GET", "/api/interview-slots?teamId="+other.ID, nil, 200)["items"])
	if view[1].Booked != 0 {
		t.Fatal("decided application counted as pending")
	}
	// Exempt applications cannot self-book and an expired slot is never offered.
	ap = storedInterviewApplication(t, a, ap.ID)
	ap.Interview = &TeamInterview{Status: "exempt"}
	if err := a.store.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
		t.Fatal(err)
	}
	student.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": ap.ID}, 409)
	admin.request("POST", "/api/interview-slots/batch", map[string]any{"teamId": team.ID, "direction": "运营", "startAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), "intervalMinutes": 10, "count": 1, "capacity": 1, "room": "Z"}, 400)
}

func TestInterviewSlotsConcurrentLastSeat(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "运营")
	first, ap := trainingApplicantForTest(t, a, admin, team, "运营", "slots-race-first@test.local")
	second, bp := trainingApplicantForTest(t, a, admin, team, "运营", "slots-race-second@test.local")
	slots := slotsForTest(t, admin, team, "运营", "A401", time.Now().Add(48*time.Hour), 1, 1)
	clients := []*testClient{slotStudent(t, admin, first), slotStudent(t, admin, second)}
	aps := []TeamApplication{ap, bp}
	statuses := make(chan int, 2)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := range clients {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			<-start
			body, _ := json.Marshal(map[string]string{"applicationId": aps[i].ID})
			r := httptest.NewRequest(http.MethodPost, "/api/interview-slots/"+slots[0].ID+"/book", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(clients[i].cookie)
			w := httptest.NewRecorder()
			clients[i].handler.ServeHTTP(w, r)
			statuses <- w.Code
		}(i)
	}
	close(start)
	wait.Wait()
	close(statuses)
	got := map[int]int{}
	for code := range statuses {
		got[code]++
	}
	if got[200] != 1 || got[409] != 1 {
		t.Fatalf("last seat race results: %v", got)
	}
	items := unmarshal[[]InterviewSlot](t, admin.request("GET", "/api/interview-slots", nil, 200)["items"])
	if len(items) != 1 || items[0].Booked != 1 || items[0].Remaining != 0 {
		t.Fatalf("overbooking: %+v", items)
	}
}

func TestInterviewSlotsBatchConflictRollbackAndManagerScope(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "运营", "电控")
	start := time.Now().Add(48 * time.Hour).Truncate(time.Minute)
	slots := slotsForTest(t, admin, team, "运营", "A501", start.Add(30*time.Minute), 1, 1)
	admin.request("POST", "/api/interview-slots/batch", map[string]any{"teamId": team.ID, "direction": "电控", "startAt": start.UTC().Format(time.RFC3339), "intervalMinutes": 30, "count": 2, "capacity": 1, "room": "A501"}, 409)
	state, err := readInterviewSlots(a.store)
	if err != nil || len(state.Slots) != 1 {
		t.Fatal("conflicting batch partially persisted", err)
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"运营", []string{"recruitment"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	staff.request("POST", "/api/interview-slots/batch", map[string]any{"teamId": team.ID, "direction": "电控", "startAt": start.UTC().Format(time.RFC3339), "intervalMinutes": 30, "count": 1, "capacity": 1, "room": "B501"}, 403)
	other := slotsForTest(t, admin, team, "电控", "B501", start, 1, 1)
	staff.request("PATCH", "/api/interview-slots/"+other[0].ID, map[string]bool{"closed": true}, 403)
	staff.request("PATCH", "/api/interview-slots/"+slots[0].ID, map[string]bool{"closed": true}, 200)
	visible := unmarshal[[]InterviewSlot](t, staff.request("GET", "/api/interview-slots", nil, 200)["items"])
	if len(visible) != 1 || visible[0].Direction != "运营" {
		t.Fatal("manager saw unpermitted scope")
	}
}

// Called with two independent stores, proving cross-connection locking.
func exerciseInterviewSlotsPostgres(t *testing.T, databaseURL string, store *Store) {
	t.Helper()
	firstApp := newApp(store, Config{Demo: true, WebDir: t.TempDir(), Mailer: &testMailer{}})
	admin := &testClient{t: t, handler: firstApp.Handler()}
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "运营")
	user, ap := trainingApplicantForTest(t, firstApp, admin, team, "运营", "pg-slot-first@test.local")
	other, bp := trainingApplicantForTest(t, firstApp, admin, team, "运营", "pg-slot-second@test.local")
	slots := slotsForTest(t, admin, team, "运营", "PG101", time.Now().Add(72*time.Hour), 2, 1)
	secondStore, err := openStore(databaseURL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.db.Close()
	secondApp := newApp(secondStore, Config{Demo: true, WebDir: t.TempDir(), Mailer: &testMailer{}})
	first := slotStudent(t, admin, user)
	second := &testClient{t: t, handler: secondApp.Handler()}
	second.request("POST", "/api/auth/login", map[string]string{"email": other.Email, "password": trainingPassword}, 200)
	clients := []*testClient{first, second}
	applications := []TeamApplication{ap, bp}
	type raceResult struct {
		index, code int
		body        string
	}
	results := make(chan raceResult, 2)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := range clients {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			<-start
			body, _ := json.Marshal(map[string]string{"applicationId": applications[i].ID})
			r := httptest.NewRequest(http.MethodPost, "/api/interview-slots/"+slots[0].ID+"/book", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(clients[i].cookie)
			w := httptest.NewRecorder()
			clients[i].handler.ServeHTTP(w, r)
			results <- raceResult{i, w.Code, w.Body.String()}
		}(i)
	}
	close(start)
	wait.Wait()
	close(results)
	winner, loser := -1, -1
	for result := range results {
		switch result.code {
		case 200:
			winner = result.index
		case 409:
			loser = result.index
		default:
			t.Fatalf("PostgreSQL booking returned %d: %s", result.code, result.body)
		}
	}
	if winner < 0 || loser < 0 {
		t.Fatal("PostgreSQL last seat was double booked")
	}
	clients[loser].request("POST", "/api/interview-slots/"+slots[1].ID+"/book", map[string]string{"applicationId": applications[loser].ID}, 200)
	clients[winner].request("POST", "/api/interview-slots/"+slots[1].ID+"/book", map[string]string{"applicationId": applications[winner].ID}, 409)
	if got := storedInterviewApplication(t, firstApp, applications[winner].ID); got.Interview.SlotID != slots[0].ID {
		t.Fatal("PostgreSQL failed reschedule lost original seat")
	}
	clients[winner].request("DELETE", "/api/interviews/"+applications[winner].ID+"/booking", nil, 200)
	clients[loser].request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": applications[loser].ID}, 200)
	if err = secondStore.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(databaseURL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	state, err := readInterviewSlots(reopened)
	if err != nil || len(state.Slots) < 2 {
		t.Fatal("PostgreSQL slots did not survive reconnect", err)
	}
	persisted, err := readOne[TeamApplication](reopened, "SELECT payload FROM team_applications WHERE id=?", applications[loser].ID)
	if err != nil || persisted.Interview == nil || persisted.Interview.SlotID != slots[0].ID {
		t.Fatal("PostgreSQL reservation did not survive reconnect", err)
	}
}

func TestInterviewSlotsManualRoomConflictsAndBatchMove(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "运营")
	_, ap := trainingApplicantForTest(t, a, admin, team, "运营", "slots-room-first@test.local")
	_, bp := trainingApplicantForTest(t, a, admin, team, "运营", "slots-room-second@test.local")
	start := time.Now().Add(48 * time.Hour).Truncate(time.Minute)
	manual := map[string]any{"applicationIds": []string{ap.ID, bp.ID}, "startAt": start.UTC().Format(time.RFC3339), "room": "C601", "intervalMinutes": 30, "startOrder": 1}
	admin.request("PATCH", "/api/interviews/schedule", manual, 200)
	// Moving the whole batch may overlap its old placement but not final placement.
	manual["startAt"] = start.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	admin.request("PATCH", "/api/interviews/schedule", manual, 200)
	admin.request("POST", "/api/interview-slots/batch", map[string]any{"teamId": team.ID, "direction": "运营", "startAt": start.Add(30 * time.Minute).UTC().Format(time.RFC3339), "intervalMinutes": 30, "count": 1, "capacity": 1, "room": "C601"}, 409)
	slots := slotsForTest(t, admin, team, "运营", "D601", start.Add(30*time.Minute), 1, 1)
	admin.request("PATCH", "/api/interview-slots/"+slots[0].ID, map[string]string{"room": "C601"}, 409)
	manual["room"] = "D601"
	manual["applicationIds"] = []string{ap.ID}
	admin.request("PATCH", "/api/interviews/schedule", manual, 409)
	admin.request("PATCH", "/api/interview-slots/"+slots[0].ID, map[string]bool{"closed": true}, 200)
	admin.request("PATCH", "/api/interviews/schedule", manual, 200)
	admin.request("PATCH", "/api/interview-slots/"+slots[0].ID, map[string]bool{"closed": false}, 409)
}

func TestInterviewSlotsExpiredScheduleAndNotificationInvalidation(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	team := trainingTeamForTest(t, admin, "运营")
	user, ap := trainingApplicantForTest(t, a, admin, team, "运营", "slots-expiry@test.local")
	slots := slotsForTest(t, admin, team, "运营", "Expiry101", time.Now().Add(48*time.Hour), 2, 1)
	student := slotStudent(t, admin, user)
	student.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": ap.ID}, 200)
	draft := unmarshal[MailCampaign](t, admin.request("POST", "/api/interviews/notifications", map[string]any{"applicationIds": []string{ap.ID}}, 201)["item"])
	if !a.interviewDeliveryValid(draft.Recipients[0]) {
		t.Fatal("fresh booking notification invalid")
	}
	student.request("POST", "/api/interview-slots/"+slots[1].ID+"/book", map[string]string{"applicationId": ap.ID}, 200)
	if a.interviewDeliveryValid(draft.Recipients[0]) {
		t.Fatal("old notification remained valid after reschedule")
	}
	// Make the currently booked slot past without sleeping in the test.
	state, err := readInterviewSlots(a.store)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	state.Slots[1].StartAt = past
	state.Slots[1].EndAt = time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339)
	if err = saveInterviewSlots(a.store, state); err != nil {
		t.Fatal(err)
	}
	ap = storedInterviewApplication(t, a, ap.ID)
	ap.Interview.At = past
	if err = a.store.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
		t.Fatal(err)
	}
	student.request("POST", "/api/interview-slots/"+slots[1].ID+"/book", map[string]string{"applicationId": ap.ID}, 409)
	student.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": ap.ID}, 409)
	student.request("DELETE", "/api/interviews/"+ap.ID+"/booking", nil, 409)
	admin.request("DELETE", "/api/interviews/"+ap.ID+"/booking", nil, 200)
	student.request("POST", "/api/interview-slots/"+slots[0].ID+"/book", map[string]string{"applicationId": ap.ID}, 200)
	admin.request("PATCH", "/api/team-applications/"+ap.ID, map[string]string{"status": "rejected"}, 200)
	items := unmarshal[[]InterviewSlot](t, admin.request("GET", "/api/interview-slots", nil, 200)["items"])
	for _, slot := range items {
		if slot.Booked != 0 {
			t.Fatal("application review did not release occupancy")
		}
	}
	student.request("DELETE", "/api/interviews/"+ap.ID+"/booking", nil, 409)
}
