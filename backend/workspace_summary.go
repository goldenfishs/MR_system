package main

import (
	"net/http"
	"time"
)

// The workspace uses published teams. Legacy interview data remains available
// in its own view and does not inflate the current recruitment totals.
func (a *App) workspaceSummary(w http.ResponseWriter, _ *http.Request, u User) error {
	profile, err := a.store.profile(u)
	if err != nil {
		return err
	}
	teams, err := readAll[Team](a.store, "SELECT payload FROM teams")
	if err != nil {
		return err
	}
	q := "SELECT payload FROM team_applications"
	args := []any{}
	if u.Role != "admin" {
		q += " WHERE user_id=?"
		args = append(args, u.ID)
	}
	applications, err := readAll[TeamApplication](a.store, q, args...)
	if err != nil {
		return err
	}
	stats := map[string]int{"openTeams": 0, "applications": len(applications), "pendingApplications": 0, "members": 0, "records": 0, "files": 0, "activeTasks": 0, "pendingReviews": 0}
	for _, team := range teams {
		if team.Status == "open" && !expired(team.Deadline) {
			stats["openTeams"]++
		}
	}
	pipeline := []map[string]any{}
	for _, stage := range []struct{ status, label string }{{"pending", "待审核"}, {"accepted", "已入队"}, {"rejected", "未通过"}} {
		count := 0
		for _, application := range applications {
			if application.Status == stage.status {
				count++
			}
		}
		pipeline = append(pipeline, map[string]any{"status": stage.status, "label": stage.label, "count": count})
		if stage.status == "pending" {
			stats["pendingApplications"] = count
		}
	}
	memberQuery := "SELECT COUNT(DISTINCT user_id) FROM team_memberships"
	memberArgs := []any{}
	if u.Role != "admin" {
		memberQuery = "SELECT COUNT(*) FROM team_memberships WHERE user_id=?"
		memberArgs = append(memberArgs, u.ID)
	}
	if err = scanCount(a.store, stats, "members", memberQuery, memberArgs...); err != nil {
		return err
	}
	// Personal files always count only the current user's library, including for administrators.
	if err = scanCount(a.store, stats, "files", "SELECT COUNT(*) FROM files WHERE owner_id=?", u.ID); err != nil {
		return err
	}
	recordQuery := "SELECT COUNT(*) FROM records"
	recordArgs := []any{}
	if u.Role != "admin" {
		recordQuery += " WHERE user_id=?"
		recordArgs = append(recordArgs, u.ID)
	}
	if err = scanCount(a.store, stats, "records", recordQuery, recordArgs...); err != nil {
		return err
	}
	tasks, err := a.tasksFor(u)
	if err != nil {
		return err
	}
	visible := map[string]bool{}
	for _, task := range tasks {
		visible[task.ID] = true
		start, _ := time.Parse(time.RFC3339, task.StartsAt)
		if !expired(task.DueAt) && (task.StartsAt == "" || !time.Now().Before(start)) {
			stats["activeTasks"]++
		}
	}
	if canManage(u) || u.Role == "student" {
		subs, e := readAll[Submission](a.store, "SELECT payload FROM submissions ORDER BY version DESC")
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, sub := range subs {
			key := sub.TaskID + "/" + sub.UserID
			if seen[key] {
				continue
			}
			seen[key] = true
			if visible[sub.TaskID] && (canManage(u) || sub.UserID == u.ID) && sub.Status == "pending" {
				stats["pendingReviews"]++
			}
		}
	}
	respond(w, http.StatusOK, map[string]any{"stats": stats, "pipeline": pipeline, "profileComplete": profile.Complete})
	return nil
}

func scanCount(s *Store, stats map[string]int, key, query string, args ...any) error {
	var count int
	if err := s.row(query, args...).Scan(&count); err != nil {
		return err
	}
	stats[key] = count
	return nil
}
