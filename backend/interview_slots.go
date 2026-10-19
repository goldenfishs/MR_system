package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

const interviewSlotsSettingID = "interview-slots-v1"

// Slots contain no applicant identities. Reservations stay on applications.
type InterviewSlot struct {
	ID        string `json:"id"`
	TeamID    string `json:"teamId"`
	Direction string `json:"direction"`
	StartAt   string `json:"startAt"`
	EndAt     string `json:"endAt"`
	Room      string `json:"room"`
	Capacity  int    `json:"capacity"`
	Closed    bool   `json:"closed"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Booked    int    `json:"booked"`
	Remaining int    `json:"remaining"`
}
type interviewSlotState struct {
	Slots []InterviewSlot `json:"slots"`
}

func (a *App) registerInterviewSlotRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/interview-slots", a.wrap(true, a.listInterviewSlots))
	m.HandleFunc("POST /api/interview-slots/batch", a.wrap(true, a.createInterviewSlotBatch))
	m.HandleFunc("PATCH /api/interview-slots/{id}", a.wrap(true, a.updateInterviewSlot))
	m.HandleFunc("POST /api/interview-slots/{id}/book", a.wrap(true, a.bookInterviewSlot))
	m.HandleFunc("DELETE /api/interviews/{id}/booking", a.wrap(true, a.cancelInterviewBooking))
}

// A real UPDATE acquires a database lock before reading applications, serializing
// reservations across processes on SQLite and PostgreSQL, not just goroutines.
func lockInterviewSlots(s *Store) error {
	if err := s.exec("INSERT INTO settings(id,payload) VALUES(?,?) ON CONFLICT(id) DO NOTHING", interviewSlotsSettingID, encoded(interviewSlotState{Slots: []InterviewSlot{}})); err != nil {
		return err
	}
	return s.exec("UPDATE settings SET payload=payload WHERE id=?", interviewSlotsSettingID)
}
func readInterviewSlots(s *Store) (interviewSlotState, error) {
	state, err := readOne[interviewSlotState](s, "SELECT payload FROM settings WHERE id=?", interviewSlotsSettingID)
	if errors.Is(err, sql.ErrNoRows) {
		return interviewSlotState{Slots: []InterviewSlot{}}, nil
	}
	return state, err
}
func saveInterviewSlots(s *Store, state interviewSlotState) error {
	return s.exec("UPDATE settings SET payload=? WHERE id=?", encoded(state), interviewSlotsSettingID)
}
func pendingInterview(ap TeamApplication) bool {
	return ap.Status == "pending" && interviewOf(ap).Status == "pending"
}
func interviewSlotCounts(apps []TeamApplication) map[string]int {
	counts := map[string]int{}
	for _, ap := range apps {
		if pendingInterview(ap) && ap.Interview != nil && ap.Interview.SlotID != "" {
			counts[ap.Interview.SlotID]++
		}
	}
	return counts
}
func countedInterviewSlot(slot InterviewSlot, counts map[string]int) InterviewSlot {
	slot.Booked = counts[slot.ID]
	slot.Remaining = max(0, slot.Capacity-slot.Booked)
	return slot
}
func (a *App) listInterviewSlots(w http.ResponseWriter, r *http.Request, u User) error {
	state, err := readInterviewSlots(a.store)
	if err != nil {
		return err
	}
	apps, err := readAll[TeamApplication](a.store, "SELECT payload FROM team_applications")
	if err != nil {
		return err
	}
	teamID, direction, applicationID := r.URL.Query().Get("teamId"), r.URL.Query().Get("direction"), r.URL.Query().Get("applicationId")
	if direction != "" && !validDirection(direction) {
		return fail(400, "组别无效")
	}
	if applicationID != "" {
		found := false
		for _, ap := range apps {
			if ap.ID == applicationID {
				if ap.UserID != u.ID && !hasPermission(u, ap.Direction, "recruitment") {
					return fail(403, "无权查看该报名的面试时段")
				}
				if teamID != "" && teamID != ap.TeamID || direction != "" && direction != ap.Direction {
					return fail(400, "报名与筛选组别不一致")
				}
				teamID, direction, found = ap.TeamID, ap.Direction, true
			}
		}
		if !found {
			return fail(404, "报名不存在")
		}
	}
	eligible := map[string]bool{}
	myBookings := []map[string]string{}
	for _, ap := range apps {
		if ap.UserID != u.ID || !pendingInterview(ap) {
			continue
		}
		eligible[ap.TeamID+"|"+ap.Direction] = true
		if ap.Interview != nil && ap.Interview.SlotID != "" && (applicationID == "" || ap.ID == applicationID) && (teamID == "" || ap.TeamID == teamID) && (direction == "" || ap.Direction == direction) {
			myBookings = append(myBookings, map[string]string{"applicationId": ap.ID, "slotId": ap.Interview.SlotID})
		}
	}
	counts := interviewSlotCounts(apps)
	items := []InterviewSlot{}
	for _, slot := range state.Slots {
		if teamID != "" && slot.TeamID != teamID || direction != "" && slot.Direction != direction {
			continue
		}
		manager := hasPermission(u, slot.Direction, "recruitment")
		if !manager && !eligible[slot.TeamID+"|"+slot.Direction] {
			continue
		}
		own := slices.ContainsFunc(myBookings, func(b map[string]string) bool { return b["slotId"] == slot.ID })
		if !manager && (slot.Closed || expired(slot.StartAt)) && !own {
			continue
		}
		items = append(items, countedInterviewSlot(slot, counts))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartAt == items[j].StartAt {
			return items[i].Room < items[j].Room
		}
		return items[i].StartAt < items[j].StartAt
	})
	respond(w, 200, map[string]any{"items": items, "myBookings": myBookings, "serverTime": now()})
	return nil
}
func validateSlotScope(s *Store, u User, teamID, direction string) error {
	if err := requirePermission(u, direction, "recruitment"); err != nil {
		return err
	}
	team, err := readOne[Team](s, "SELECT payload FROM teams WHERE id=?", teamID)
	if err != nil {
		return err
	}
	if !validDirection(direction) || !slices.Contains(team.Directions, direction) {
		return fail(400, "请选择该招募团队开放的组别")
	}
	return nil
}
func overlapsInterview(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}
func slotRoomConflict(candidate InterviewSlot, slots []InterviewSlot, counts map[string]int) bool {
	start, _ := time.Parse(time.RFC3339, candidate.StartAt)
	end, _ := time.Parse(time.RFC3339, candidate.EndAt)
	for _, other := range slots {
		if candidate.ID == other.ID || candidate.Room != other.Room || other.Closed && counts[other.ID] == 0 {
			continue
		}
		os, _ := time.Parse(time.RFC3339, other.StartAt)
		oe, _ := time.Parse(time.RFC3339, other.EndAt)
		if overlapsInterview(start, end, os, oe) {
			return true
		}
	}
	return false
}

// Existing direct schedules also reserve their room. Keep bookable slots and
// administrator-assigned interviews from silently double-booking one classroom.
func manualInterviewRoomConflict(room string, start, end time.Time, apps []TeamApplication, skipID string) bool {
	for _, ap := range apps {
		if ap.ID == skipID || !pendingInterview(ap) {
			continue
		}
		v := interviewOf(ap)
		if v.SlotID != "" || v.Room != room {
			continue
		}
		otherStart, otherEnd, valid := appointmentInterval(ap, nil)
		if valid && overlapsInterview(start, end, otherStart, otherEnd) {
			return true
		}
	}
	return false
}
func (a *App) createInterviewSlotBatch(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		TeamID          string `json:"teamId"`
		Direction       string `json:"direction"`
		StartAt         string `json:"startAt"`
		IntervalMinutes int    `json:"intervalMinutes"`
		Count           int    `json:"count"`
		Capacity        int    `json:"capacity"`
		Room            string `json:"room"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	start, err := time.Parse(time.RFC3339, in.StartAt)
	in.Room = strings.TrimSpace(in.Room)
	if err != nil || !start.After(time.Now()) || in.IntervalMinutes < 1 || in.IntervalMinutes > 120 || in.Count < 1 || in.Count > 100 || in.Capacity < 1 || in.Capacity > 100 || in.Room == "" || len(in.Room) > 300 {
		return fail(400, "请填写未来时间、1–120 分钟间隔、1–100 个时段、每时段 1–100 人和教室")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = lockInterviewSlots(s); err != nil {
		return err
	}
	if err = validateSlotScope(s, u, in.TeamID, in.Direction); err != nil {
		return err
	}
	state, err := readInterviewSlots(s)
	if err != nil {
		return err
	}
	if len(state.Slots)+in.Count > 5000 {
		return fail(409, "面试时段数量已达上限，请联系系统管理员归档历史场次")
	}
	apps, err := readAll[TeamApplication](s, "SELECT payload FROM team_applications")
	if err != nil {
		return err
	}
	counts := interviewSlotCounts(apps)
	items := []InterviewSlot{}
	for i := 0; i < in.Count; i++ {
		at := start.Add(time.Duration(i*in.IntervalMinutes) * time.Minute)
		slot := InterviewSlot{ID: randomID(), TeamID: in.TeamID, Direction: in.Direction, StartAt: at.UTC().Format(time.RFC3339), EndAt: at.Add(time.Duration(in.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339), Room: in.Room, Capacity: in.Capacity, CreatedAt: now(), UpdatedAt: now()}
		if slotRoomConflict(slot, state.Slots, counts) || manualInterviewRoomConflict(slot.Room, at, at.Add(time.Duration(in.IntervalMinutes)*time.Minute), apps, "") {
			return fail(409, "该教室在所选时间已有面试时段，请调整时间或教室")
		}
		state.Slots = append(state.Slots, slot)
		items = append(items, countedInterviewSlot(slot, counts))
	}
	if err = saveInterviewSlots(s, state); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "创建面试预约时段", fmt.Sprintf("%s · %s · %d 个时段", in.Direction, in.Room, len(items)))
	respond(w, 201, map[string]any{"items": items})
	return nil
}
func (a *App) updateInterviewSlot(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		Room            *string `json:"room"`
		Capacity        *int    `json:"capacity"`
		Closed          *bool   `json:"closed"`
		StartAt         *string `json:"startAt"`
		IntervalMinutes *int    `json:"intervalMinutes"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.Room == nil && in.Capacity == nil && in.Closed == nil && in.StartAt == nil && in.IntervalMinutes == nil {
		return fail(400, "请提供要修改的时段字段")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = lockInterviewSlots(s); err != nil {
		return err
	}
	state, err := readInterviewSlots(s)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(state.Slots, func(slot InterviewSlot) bool { return slot.ID == r.PathValue("id") })
	if idx < 0 {
		return fail(404, "面试时段不存在")
	}
	slot := state.Slots[idx]
	if err = requirePermission(u, slot.Direction, "recruitment"); err != nil {
		return err
	}
	apps, err := readAll[TeamApplication](s, "SELECT payload FROM team_applications")
	if err != nil {
		return err
	}
	counts := interviewSlotCounts(apps)
	original := slot
	if in.Room != nil {
		slot.Room = strings.TrimSpace(*in.Room)
	}
	if in.Capacity != nil {
		slot.Capacity = *in.Capacity
	}
	if in.Closed != nil {
		slot.Closed = *in.Closed
	}
	start, _ := time.Parse(time.RFC3339, slot.StartAt)
	end, _ := time.Parse(time.RFC3339, slot.EndAt)
	duration := end.Sub(start)
	if in.StartAt != nil {
		start, err = time.Parse(time.RFC3339, *in.StartAt)
		if err != nil {
			return fail(400, "面试开始时间格式无效")
		}
	}
	if in.IntervalMinutes != nil {
		if *in.IntervalMinutes < 1 || *in.IntervalMinutes > 120 {
			return fail(400, "时段长度须为 1–120 分钟")
		}
		duration = time.Duration(*in.IntervalMinutes) * time.Minute
	}
	slot.StartAt = start.UTC().Format(time.RFC3339)
	slot.EndAt = start.Add(duration).UTC().Format(time.RFC3339)
	if slot.Room == "" || len(slot.Room) > 300 || slot.Capacity < 1 || slot.Capacity > 100 {
		return fail(400, "请输入教室和 1–100 人的容量")
	}
	if slot.Capacity < counts[slot.ID] {
		return fail(409, "容量不能小于已预约人数")
	}
	changedSchedule := slot.Room != original.Room || slot.StartAt != original.StartAt || slot.EndAt != original.EndAt
	if changedSchedule && counts[slot.ID] > 0 {
		return fail(409, "该时段已有预约，请先取消或改签，再调整时间或教室")
	}
	if (changedSchedule || original.Closed && !slot.Closed) && !start.After(time.Now()) {
		return fail(409, "已开始的时段不能重新开放或修改排期")
	}
	if (!slot.Closed || counts[slot.ID] > 0) && (slotRoomConflict(slot, state.Slots, counts) || manualInterviewRoomConflict(slot.Room, start, start.Add(duration), apps, "")) {
		return fail(409, "该教室在所选时间已有面试时段")
	}
	slot.UpdatedAt = now()
	state.Slots[idx] = slot
	if err = saveInterviewSlots(s, state); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": countedInterviewSlot(slot, counts)})
	return nil
}
func appointmentInterval(ap TeamApplication, slots []InterviewSlot) (time.Time, time.Time, bool) {
	v := interviewOf(ap)
	start, err := time.Parse(time.RFC3339, v.At)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	minutes := v.DurationMinutes
	if minutes < 1 {
		minutes = 15
	}
	end := start.Add(time.Duration(minutes) * time.Minute)
	for _, slot := range slots {
		if slot.ID == v.SlotID {
			if parsed, err := time.Parse(time.RFC3339, slot.EndAt); err == nil {
				end = parsed
			}
			break
		}
	}
	return start, end, true
}
func checkInterviewApplicantConflict(ap TeamApplication, start, end time.Time, apps []TeamApplication, slots []InterviewSlot) error {
	for _, other := range apps {
		if other.ID == ap.ID || other.UserID != ap.UserID || !pendingInterview(other) {
			continue
		}
		os, oe, valid := appointmentInterval(other, slots)
		if valid && overlapsInterview(start, end, os, oe) {
			return fail(409, "该成员已有其他组别的重叠面试，请选择其他时段")
		}
	}
	return nil
}
func bookingApplication(s *Store, u User, id string) (TeamApplication, error) {
	ap, err := readOne[TeamApplication](s, "SELECT payload FROM team_applications WHERE id=?", id)
	if err != nil {
		return ap, err
	}
	if ap.UserID != u.ID && !hasPermission(u, ap.Direction, "recruitment") {
		return ap, fail(403, "只能预约自己的面试或管理获授权组别")
	}
	if !pendingInterview(ap) {
		return ap, fail(409, "只有待面试的报名可以预约；已免试或已确认结果的报名不能预约")
	}
	return ap, nil
}
func (a *App) bookInterviewSlot(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		ApplicationID string `json:"applicationId"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = lockInterviewSlots(s); err != nil {
		return err
	}
	ap, err := bookingApplication(s, u, in.ApplicationID)
	if err != nil {
		return err
	}
	state, err := readInterviewSlots(s)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(state.Slots, func(slot InterviewSlot) bool { return slot.ID == r.PathValue("id") })
	if idx < 0 {
		return fail(404, "面试时段不存在")
	}
	slot := state.Slots[idx]
	if ap.TeamID != slot.TeamID || ap.Direction != slot.Direction {
		return fail(400, "所选时段不属于这份报名的团队和组别")
	}
	start, _ := time.Parse(time.RFC3339, slot.StartAt)
	end, _ := time.Parse(time.RFC3339, slot.EndAt)
	if !start.After(time.Now()) {
		return fail(409, "该面试时段已开始，不能预约或改签")
	}
	v := interviewOf(ap)
	if v.SlotID == slot.ID {
		respond(w, 200, map[string]any{"item": ap})
		return nil
	}
	if slot.Closed {
		return fail(409, "该时段已停止预约，请选择其他时段")
	}
	if v.At != "" && expired(v.At) {
		return fail(409, "原面试已开始，请联系组管理员处理")
	}
	apps, err := readAll[TeamApplication](s, "SELECT payload FROM team_applications")
	if err != nil {
		return err
	}
	counts := interviewSlotCounts(apps)
	if counts[slot.ID] >= slot.Capacity {
		return fail(409, "该时段已约满，请刷新后选择其他时段")
	}
	if err = checkInterviewApplicantConflict(ap, start, end, apps, state.Slots); err != nil {
		return err
	}
	orders := map[int]bool{}
	for _, other := range apps {
		if pendingInterview(other) && other.Interview != nil && other.Interview.SlotID == slot.ID {
			orders[other.Interview.Order] = true
		}
	}
	order := 1
	for orders[order] {
		order++
	}
	v.SlotID = slot.ID
	v.At = slot.StartAt
	v.Room = slot.Room
	v.Order = order
	v.DurationMinutes = int(end.Sub(start) / time.Minute)
	ap.Interview = &v
	ap.UpdatedAt = now()
	if err = s.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "预约面试", ap.Direction+" · "+slot.Room+" · "+slot.StartAt)
	respond(w, 200, map[string]any{"item": ap})
	return nil
}
func (a *App) cancelInterviewBooking(w http.ResponseWriter, r *http.Request, u User) error {
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = lockInterviewSlots(s); err != nil {
		return err
	}
	ap, err := bookingApplication(s, u, r.PathValue("id"))
	if err != nil {
		return err
	}
	v := interviewOf(ap)
	if v.SlotID == "" {
		return fail(409, "该报名没有可取消的预约时段，请联系组管理员调整手动排期")
	}
	if expired(v.At) && !hasPermission(u, ap.Direction, "recruitment") {
		return fail(409, "面试已开始，请联系组管理员处理")
	}
	v.SlotID = ""
	v.At = ""
	v.Room = ""
	v.Order = 0
	v.DurationMinutes = 0
	ap.Interview = &v
	ap.UpdatedAt = now()
	if err = s.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "取消面试预约", ap.Direction+" · "+ap.UserName)
	respond(w, 200, map[string]any{"item": ap})
	return nil
}
