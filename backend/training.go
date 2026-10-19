package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

type TrainingStage struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Order       int    `json:"order"`
	Final       bool   `json:"final"`
}
type TrainingPlan struct {
	ID        string          `json:"id"`
	TeamID    string          `json:"teamId"`
	Direction string          `json:"direction"`
	Title     string          `json:"title"`
	Stages    []TrainingStage `json:"stages"`
	CreatedBy string          `json:"createdBy"`
	CreatedAt string          `json:"createdAt"`
	UpdatedBy string          `json:"updatedBy"`
	UpdatedAt string          `json:"updatedAt"`
}
type TrainingDecision struct {
	StageID       string `json:"stageId"`
	StageTitle    string `json:"stageTitle"`
	Status        string `json:"status"`
	Note          string `json:"note"`
	DecidedBy     string `json:"decidedBy"`
	DecidedByName string `json:"decidedByName"`
	DecidedAt     string `json:"decidedAt"`
	MemberType    string `json:"memberType,omitempty"`
}
type TrainingProgress struct {
	ID             string             `json:"id"`
	TeamID         string             `json:"teamId"`
	UserID         string             `json:"userId"`
	UserName       string             `json:"userName"`
	Email          string             `json:"email"`
	Direction      string             `json:"direction"`
	Cycle          int                `json:"cycle"`
	PlanID         string             `json:"planId"`
	PlanTitle      string             `json:"planTitle"`
	Stages         []TrainingStage    `json:"stages"`
	CurrentStageID string             `json:"currentStageId"`
	Status         string             `json:"status"`
	Decisions      []TrainingDecision `json:"decisions"`
	CreatedBy      string             `json:"createdBy"`
	CreatedAt      string             `json:"createdAt"`
	UpdatedAt      string             `json:"updatedAt"`
}

func (a *App) registerTrainingRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/training-plans", a.wrap(true, a.listTrainingPlans))
	m.HandleFunc("POST /api/training-plans", a.wrap(true, a.createTrainingPlan))
	m.HandleFunc("PATCH /api/training-plans/{id}", a.wrap(true, a.updateTrainingPlan))
	m.HandleFunc("GET /api/training-progress", a.wrap(true, a.listTrainingProgress))
	m.HandleFunc("POST /api/training-progress/decisions", a.wrap(true, a.decideTrainingStage))
}

func normalizeTrainingPlan(p *TrainingPlan) error {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" || len(p.Title) > 200 || !validDirection(p.Direction) || len(p.Stages) < 1 || len(p.Stages) > 12 {
		return fail(400, "请填写计划名称、有效组别，并配置 1 至 12 个培养阶段")
	}
	seen := map[string]bool{}
	for i := range p.Stages {
		s := &p.Stages[i]
		s.Title, s.Description = strings.TrimSpace(s.Title), strings.TrimSpace(s.Description)
		if s.ID == "" {
			s.ID = randomID()
		}
		if s.Title == "" || len(s.Title) > 200 || len(s.Description) > 5000 || len(s.ID) > 100 || seen[s.ID] || s.Final != (i == len(p.Stages)-1) {
			return fail(400, "阶段名称最多 200 字节、说明最多 5000 字节；仅最后一阶段应标为最终考核，阶段不可重复")
		}
		seen[s.ID] = true
		s.Order = i + 1
	}
	return nil
}
func (a *App) listTrainingPlans(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := readAll[TrainingPlan](a.store, "SELECT payload FROM training_plans ORDER BY direction,team_id")
	if err != nil {
		return err
	}
	out := []TrainingPlan{}
	for _, p := range items {
		if v := r.URL.Query().Get("teamId"); v != "" && p.TeamID != v {
			continue
		}
		if v := r.URL.Query().Get("direction"); v != "" && p.Direction != v {
			continue
		}
		out = append(out, p)
	}
	respond(w, 200, map[string]any{"items": out})
	return nil
}
func (a *App) createTrainingPlan(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		TeamID            string          `json:"teamId"`
		Direction         string          `json:"direction"`
		Title             string          `json:"title"`
		Stages            []TrainingStage `json:"stages"`
		ApplyToUnassessed bool            `json:"applyToUnassessed"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := requirePermission(u, in.Direction, "assessment"); err != nil {
		return err
	}
	plan := TrainingPlan{ID: randomID(), TeamID: in.TeamID, Direction: in.Direction, Title: in.Title, Stages: in.Stages, CreatedBy: u.ID, CreatedAt: now(), UpdatedBy: u.ID, UpdatedAt: now()}
	if err := normalizeTrainingPlan(&plan); err != nil {
		return err
	}
	if plan.TeamID != "" {
		team, err := readOne[Team](a.store, "SELECT payload FROM teams WHERE id=?", plan.TeamID)
		if err != nil {
			return err
		}
		if !slices.Contains(team.Directions, plan.Direction) {
			return fail(400, "该团队未开放此技术组")
		}
	}
	var count int
	if err := a.store.row("SELECT COUNT(*) FROM training_plans WHERE team_id=? AND direction=?", plan.TeamID, plan.Direction).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fail(409, "该团队与技术组已有培养计划，请编辑已有计划")
	}
	synced, err := a.saveTrainingPlan(r, plan, true, in.ApplyToUnassessed)
	if err != nil {
		return err
	}
	a.activity(u, "配置新生培养计划", plan.Direction+" · "+plan.Title+fmt.Sprintf(" · 同步 %d 人", synced))
	respond(w, 201, map[string]any{"item": plan, "syncedCount": synced})
	return nil
}
func (a *App) updateTrainingPlan(w http.ResponseWriter, r *http.Request, u User) error {
	plan, err := readOne[TrainingPlan](a.store, "SELECT payload FROM training_plans WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if err := requirePermission(u, plan.Direction, "assessment"); err != nil {
		return err
	}
	var in struct {
		Title             *string          `json:"title"`
		Stages            *[]TrainingStage `json:"stages"`
		ApplyToUnassessed bool             `json:"applyToUnassessed"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.Title != nil {
		plan.Title = *in.Title
	}
	if in.Stages != nil {
		plan.Stages = *in.Stages
	}
	if err := normalizeTrainingPlan(&plan); err != nil {
		return err
	}
	plan.UpdatedBy, plan.UpdatedAt = u.ID, now()
	synced, err := a.saveTrainingPlan(r, plan, false, in.ApplyToUnassessed)
	if err != nil {
		return err
	}
	a.activity(u, "更新新生培养计划", plan.Direction+" · "+plan.Title+fmt.Sprintf(" · 同步 %d 人", synced))
	respond(w, 200, map[string]any{"item": plan, "syncedCount": synced})
	return nil
}

// Save the plan and optional migration atomically. Assessed cycles are immutable.
func (a *App) saveTrainingPlan(r *http.Request, plan TrainingPlan, create, apply bool) (int, error) {
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if create {
		err = s.exec("INSERT INTO training_plans(id,team_id,direction,payload) VALUES(?,?,?,?)", plan.ID, plan.TeamID, plan.Direction, encoded(plan))
	} else {
		err = s.exec("UPDATE training_plans SET payload=? WHERE id=?", encoded(plan), plan.ID)
	}
	if err != nil {
		return 0, err
	}
	count := 0
	if apply {
		query := "SELECT payload FROM training_progress WHERE team_id=? AND direction=? AND status='training' ORDER BY id"
		if s.postgres {
			query += " FOR UPDATE"
		}
		items, err := readAll[TrainingProgress](s, query, plan.TeamID, plan.Direction)
		if err != nil {
			return 0, err
		}
		for _, p := range items {
			if len(p.Decisions) != 0 || len(p.Stages) == 0 || p.CurrentStageID != p.Stages[0].ID {
				continue
			}
			latest, err := latestTrainingInScope(s, p.UserID, p.TeamID, p.Direction)
			if err != nil {
				return 0, err
			}
			if latest == nil || latest.ID != p.ID {
				continue
			}
			active, err := trainingEnrollmentActive(s, p)
			if err != nil {
				return 0, err
			}
			if !active {
				continue
			}
			p.PlanID, p.PlanTitle, p.Stages = plan.ID, plan.Title, plan.Stages
			p.CurrentStageID, p.UpdatedAt = plan.Stages[0].ID, now()
			if err := s.exec("UPDATE training_progress SET payload=? WHERE id=?", encoded(p), p.ID); err != nil {
				return 0, err
			}
			count++
		}
	}
	return count, tx.Commit()
}

func latestTraining(s *Store, userID string) (*TrainingProgress, error) {
	p, err := readOne[TrainingProgress](s, "SELECT payload FROM training_progress WHERE user_id=? ORDER BY cycle DESC LIMIT 1", userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// Cycles retain their globally increasing audit number, but each team and group
// has its own current cycle. A decision in one group cannot supersede another.
func latestTrainingInScope(s *Store, userID, teamID, direction string) (*TrainingProgress, error) {
	p, err := readOne[TrainingProgress](s, "SELECT payload FROM training_progress WHERE user_id=? AND team_id=? AND direction=? ORDER BY cycle DESC LIMIT 1", userID, teamID, direction)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}
func currentTraining(s *Store, userID string) ([]TrainingProgress, error) {
	items, err := readAll[TrainingProgress](s, "SELECT payload FROM training_progress WHERE user_id=? ORDER BY cycle DESC", userID)
	if err != nil {
		return nil, err
	}
	out := []TrainingProgress{}
	seen := map[string]bool{}
	for _, p := range items {
		key := p.TeamID + "\x00" + p.Direction
		if !seen[key] {
			out = append(out, p)
			seen[key] = true
		}
	}
	return out, nil
}
func trainingEliminated(s *Store, u User) (bool, error) {
	if u.MemberType != "freshman" || u.Role == "admin" {
		return false, nil
	}
	directions, err := memberDirectionsAt(s, u.ID)
	if err != nil || len(directions) > 0 {
		return false, err
	}
	progress, err := currentTraining(s, u.ID)
	for _, p := range progress {
		if p.Status == "eliminated" {
			return true, err
		}
	}
	return false, err
}

func trainingEnrollmentActive(s *Store, p TrainingProgress) (bool, error) {
	if p.Status == "eliminated" {
		return false, nil
	}
	if p.TeamID != "" {
		var count int
		err := s.row("SELECT COUNT(*) FROM team_memberships WHERE team_id=? AND user_id=? AND direction=?", p.TeamID, p.UserID, p.Direction).Scan(&count)
		return count > 0, err
	}
	apps, err := readAll[Applicant](s, "SELECT payload FROM applicants WHERE user_id=?", p.UserID)
	if err != nil {
		return false, err
	}
	for _, ap := range apps {
		if ap.Status == "accepted" && ap.Direction == p.Direction {
			return true, nil
		}
	}
	return false, nil
}

func annotateTrainingApplication(s *Store, ap *TeamApplication) error {
	ap.TrainingStatus, ap.TrainingStageTitle, ap.TrainingCycle = "", "", 0
	p, err := latestTrainingInScope(s, ap.UserID, ap.TeamID, ap.Direction)
	if err != nil || p == nil {
		return err
	}
	ap.TrainingStatus, ap.TrainingCycle = p.Status, p.Cycle
	for _, stage := range p.Stages {
		if stage.ID == p.CurrentStageID {
			ap.TrainingStageTitle = stage.Title
		}
	}
	if ap.TrainingStageTitle == "" && len(p.Decisions) > 0 {
		ap.TrainingStageTitle = p.Decisions[len(p.Decisions)-1].StageTitle
	}
	return nil
}

// Only explicit, authorized readmission reopens an eliminated group. Admissions
// and assessments in other groups have no effect on this group's history.
func admitTraining(s *Store, actor User, userID, teamID, direction string, reopen bool) error {
	user, err := s.user(userID)
	if err != nil {
		return err
	}
	old, err := latestTrainingInScope(s, userID, teamID, direction)
	if err != nil {
		return err
	}
	if old != nil {
		if old.Status == "training" || old.Status == "completed" {
			return nil
		}
		if !reopen {
			return fail(409, "该组新生考核已淘汰，需本组考核管理员明确重新录取并开启新周期")
		}
		if err := requirePermission(actor, direction, "assessment"); err != nil {
			return err
		}
	}
	latest, err := latestTraining(s, userID)
	if err != nil {
		return err
	}
	cycle := 1
	if latest != nil {
		cycle = latest.Cycle + 1
	}

	plan, err := readOne[TrainingPlan](s, "SELECT payload FROM training_plans WHERE team_id=? AND direction=?", teamID, direction)
	if errors.Is(err, sql.ErrNoRows) && teamID != "" {
		plan, err = readOne[TrainingPlan](s, "SELECT payload FROM training_plans WHERE team_id='' AND direction=?", direction)
	}
	if errors.Is(err, sql.ErrNoRows) {
		plan = TrainingPlan{Title: "新生阶段培养", Stages: []TrainingStage{{ID: "foundation", Title: "基础学习", Order: 1}, {ID: "phase", Title: "阶段考核", Order: 2}, {ID: "final", Title: "最终考核", Order: 3, Final: true}}}
	} else if err != nil {
		return err
	}
	p := TrainingProgress{ID: randomID(), TeamID: teamID, UserID: userID, UserName: user.Name, Email: user.Email, Direction: direction, Cycle: cycle, PlanID: plan.ID, PlanTitle: plan.Title, Stages: plan.Stages, CurrentStageID: plan.Stages[0].ID, Status: "training", Decisions: []TrainingDecision{}, CreatedBy: actor.ID, CreatedAt: now(), UpdatedAt: now()}
	return s.exec("INSERT INTO training_progress(id,user_id,team_id,direction,cycle,status,payload) VALUES(?,?,?,?,?,?,?)", p.ID, p.UserID, p.TeamID, p.Direction, p.Cycle, p.Status, encoded(p))
}
func (a *App) listTrainingProgress(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := readAll[TrainingProgress](a.store, "SELECT payload FROM training_progress ORDER BY cycle DESC,user_id")
	if err != nil {
		return err
	}
	out := []TrainingProgress{}
	for _, p := range items {
		if p.UserID != u.ID && !hasPermission(u, p.Direction, "assessment") {
			continue
		}
		q := r.URL.Query()
		if q.Get("teamId") != "" && q.Get("teamId") != p.TeamID || q.Get("direction") != "" && q.Get("direction") != p.Direction || q.Get("status") != "" && q.Get("status") != p.Status {
			continue
		}
		owner, err := a.store.user(p.UserID)
		if err != nil {
			return err
		}
		p.UserName, p.Email = owner.Name, owner.Email
		out = append(out, p)
	}
	respond(w, 200, map[string]any{"items": out})
	return nil
}
func (a *App) decideTrainingStage(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "assessment") {
		return fail(403, "没有新生阶段考核权限")
	}
	var in struct {
		ProgressIDs      []string `json:"progressIds"`
		StageID          string   `json:"stageId"`
		Status           string   `json:"status"`
		Note             string   `json:"note"`
		TargetMemberType string   `json:"targetMemberType"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if len(in.ProgressIDs) == 0 || len(in.ProgressIDs) > 100 || len(normalizedIDs(in.ProgressIDs)) != len(in.ProgressIDs) || in.StageID == "" || len(in.StageID) > 100 || (in.Status != "passed" && in.Status != "eliminated") || len(in.Note) > 5000 {
		return fail(400, "请选择 1 至 100 名新生的当前阶段，并填写有效考核结果")
	}
	if in.TargetMemberType == "" {
		in.TargetMemberType = "reserve"
	}
	if in.TargetMemberType != "reserve" && in.TargetMemberType != "formal" {
		return fail(400, "最终考核仅可晋升梯队或正式队员")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	out := []TrainingProgress{}
	var batchFinal *bool
	for _, id := range in.ProgressIDs {
		query := "SELECT payload FROM training_progress WHERE id=?"
		if s.postgres {
			query += " FOR UPDATE"
		}
		p, err := readOne[TrainingProgress](s, query, id)
		if err != nil {
			return err
		}
		if err = requirePermission(u, p.Direction, "assessment"); err != nil {
			return err
		}
		if p.UserID == u.ID && u.Role != "admin" {
			return fail(403, "组管理员不能确认自己的新生考核")
		}
		latest, err := latestTrainingInScope(s, p.UserID, p.TeamID, p.Direction)
		if err != nil {
			return err
		}
		if latest == nil || latest.ID != p.ID || p.Status != "training" || p.CurrentStageID != in.StageID {
			return fail(409, "仅可处理当前培养周期的当前阶段，不可跳阶段或重复决定")
		}
		owner, err := s.user(p.UserID)
		if err != nil {
			return err
		}
		if admitted, err := trainingEnrollmentActive(s, p); err != nil {
			return err
		} else if !admitted {
			return fail(409, "该新生的录取资格已撤销，请先恢复报名资格后再继续考核")
		}
		index := -1
		for i, stage := range p.Stages {
			if stage.ID == p.CurrentStageID {
				index = i
				break
			}
		}
		if index < 0 || len(p.Decisions) != index {
			return fail(409, "培养阶段历史不完整，无法跳过前置阶段")
		}
		for i, decision := range p.Decisions {
			if decision.Status != "passed" || decision.StageID != p.Stages[i].ID {
				return fail(409, "必须通过全部前置阶段后才可继续")
			}
		}
		stage := p.Stages[index]
		if batchFinal != nil && *batchFinal != stage.Final {
			return fail(409, "不同培养计划的最终阶段不可与普通阶段一起批量决定，请分批确认")
		}
		final := stage.Final
		batchFinal = &final
		decision := TrainingDecision{StageID: stage.ID, StageTitle: stage.Title, Status: in.Status, Note: strings.TrimSpace(in.Note), DecidedBy: u.ID, DecidedByName: u.Name, DecidedAt: now()}
		if in.Status == "eliminated" {
			p.Status, p.CurrentStageID = "eliminated", ""
			if err = s.exec("DELETE FROM team_memberships WHERE user_id=? AND team_id=? AND direction=?", p.UserID, p.TeamID, p.Direction); err != nil {
				return err
			}
		} else if stage.Final {
			p.Status, p.CurrentStageID = "completed", ""
			// Completing another group must never downgrade an existing formal member.
			targetType := in.TargetMemberType
			if owner.MemberType == "formal" {
				targetType = "formal"
			}
			decision.MemberType = targetType
			if owner.MemberType == "freshman" || owner.Direction == "" {
				owner.Direction = p.Direction
			}
			owner.MemberType = targetType
			if owner.Role != "admin" && owner.Role != "group_admin" {
				owner.Role = memberRole(owner.MemberType)
			}
			owner.AssessmentPassedAt, owner.AssessmentConfirmedBy, owner.AssessmentNote = decision.DecidedAt, u.ID, decision.Note
			if err = s.exec("UPDATE users SET payload=? WHERE id=?", encoded(owner), owner.ID); err != nil {
				return err
			}
		} else {
			if index+1 >= len(p.Stages) {
				return fail(409, "培养计划缺少最终阶段")
			}
			p.CurrentStageID = p.Stages[index+1].ID
		}
		p.Decisions = append(p.Decisions, decision)
		p.UpdatedAt = now()
		if err = s.exec("UPDATE training_progress SET status=?,payload=? WHERE id=?", p.Status, encoded(p), p.ID); err != nil {
			return err
		}
		out = append(out, p)
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "确认新生阶段考核", in.Status+" · "+in.StageID)
	respond(w, 200, map[string]any{"items": out})
	return nil
}
