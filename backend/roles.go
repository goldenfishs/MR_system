package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

var technicalDirections = []string{"机械", "电控", "视觉算法", "硬件", "运营"}
var groupPermissions = []string{"recruitment", "assessment", "tasks", "knowledge", "discussions", "members", "records", "mail"}

type GroupGrant struct {
	Direction   string   `json:"direction"`
	Permissions []string `json:"permissions"`
}

func normalizeUser(u *User) {
	legacy := u.Role
	switch legacy {
	case "mentor":
		u.Role, u.MemberType = "group_admin", "formal"
		if validDirection(u.Direction) {
			u.GroupGrants = []GroupGrant{{u.Direction, []string{"tasks", "knowledge", "discussions", "members"}}}
		}
	case "interviewer":
		u.Role, u.MemberType = "group_admin", "formal"
		if validDirection(u.Direction) {
			u.GroupGrants = []GroupGrant{{u.Direction, []string{"recruitment", "discussions"}}}
		}
	case "student":
		u.Role, u.MemberType = "freshman", "freshman"
	}
	if u.MemberType == "" {
		switch u.Role {
		case "admin", "group_admin", "member":
			u.MemberType = "formal"
		case "reserve":
			u.MemberType = "reserve"
		default:
			u.MemberType = "freshman"
		}
	}
	if u.Role != "admin" && u.Role != "group_admin" {
		u.Role = memberRole(u.MemberType)
	}
	if u.GroupGrants == nil {
		u.GroupGrants = []GroupGrant{}
	}
}
func memberRole(t string) string {
	switch t {
	case "formal":
		return "member"
	case "reserve":
		return "reserve"
	default:
		return "freshman"
	}
}
func hasPermission(u User, direction, permission string) bool {
	if u.Role == "admin" {
		return true
	}
	if u.Role != "group_admin" || direction == "" {
		return false
	}
	for _, grant := range u.GroupGrants {
		if grant.Direction == direction && slices.Contains(grant.Permissions, permission) {
			return true
		}
	}
	return false
}
func hasAnyPermission(u User, permission string) bool {
	if u.Role == "admin" {
		return true
	}
	for _, direction := range technicalDirections {
		if hasPermission(u, direction, permission) {
			return true
		}
	}
	return false
}
func requirePermission(u User, direction, permission string) error {
	if !hasPermission(u, direction, permission) {
		return fail(403, "没有该技术组的操作权限")
	}
	return nil
}
func validateUserAccess(u *User) error {
	if !slices.Contains([]string{"admin", "group_admin", "member", "reserve", "freshman"}, u.Role) || !slices.Contains([]string{"formal", "reserve", "freshman"}, u.MemberType) {
		return fail(400, "无效的账号角色或成员身份")
	}
	if u.Direction != "" && !validDirection(u.Direction) {
		return fail(400, "无效的所属技术组")
	}
	if u.Role != "admin" && u.Role != "group_admin" && u.Role != memberRole(u.MemberType) {
		return fail(400, "成员角色应与成员身份一致")
	}
	if len(u.GroupGrants) > 5 || u.Role != "group_admin" && len(u.GroupGrants) > 0 {
		return fail(400, "仅组管理员可配置分组授权")
	}
	seen := map[string]bool{}
	for i := range u.GroupGrants {
		g := &u.GroupGrants[i]
		if !validDirection(g.Direction) || seen[g.Direction] || len(g.Permissions) == 0 || len(g.Permissions) > len(groupPermissions) {
			return fail(400, "分组授权无效或重复")
		}
		seen[g.Direction] = true
		permissions := map[string]bool{}
		for _, p := range g.Permissions {
			if !slices.Contains(groupPermissions, p) || permissions[p] {
				return fail(400, "模块权限无效或重复")
			}
			permissions[p] = true
		}
	}
	if u.GroupGrants == nil {
		u.GroupGrants = []GroupGrant{}
	}
	return nil
}
func migrateRoles(s *Store) error {
	users, err := readAll[json.RawMessage](s, "SELECT payload FROM users")
	if err != nil {
		return err
	}
	for _, raw := range users {
		var u User
		if err = json.Unmarshal(raw, &u); err != nil {
			return err
		}
		before := encoded(u)
		normalizeUser(&u)
		if before != encoded(u) {
			if err = s.exec("UPDATE users SET payload=? WHERE id=?", encoded(u), u.ID); err != nil {
				return err
			}
		}
	}
	return s.exec("INSERT INTO schema_migrations(version,applied_at) VALUES (5,CURRENT_TIMESTAMP) ON CONFLICT(version) DO NOTHING")
}
func (a *App) memberDirections(id string) ([]string, error) {
	return memberDirectionsAt(a.store, id)
}
func memberDirectionsAt(s *Store, id string) ([]string, error) {
	user, err := s.user(id)
	if err != nil {
		return nil, err
	}
	progress, err := currentTraining(s, id)
	if err != nil {
		return nil, err
	}
	dirs := []string{}
	tracked := map[string]TrainingProgress{}
	trackedDirection := map[string]bool{}
	for _, p := range progress {
		tracked[p.TeamID+"\x00"+p.Direction] = p
		trackedDirection[p.Direction] = true
		active, err := trainingEnrollmentActive(s, p)
		if err != nil {
			return nil, err
		}
		if active {
			dirs = append(dirs, p.Direction)
		}
	}
	// Explicit administrative placement remains valid for imported members. Once
	// a group has a training record, that record governs its enrollment instead.
	if user.MemberType != "freshman" && validDirection(user.Direction) && !trackedDirection[user.Direction] {
		dirs = append(dirs, user.Direction)
	}
	memberships, err := readAll[Membership](s, "SELECT payload FROM team_memberships WHERE user_id=?", id)
	if err != nil {
		return nil, err
	}
	for _, m := range memberships {
		var direction string
		if err := s.row("SELECT direction FROM team_memberships WHERE team_id=? AND user_id=?", m.TeamID, m.UserID).Scan(&direction); err != nil {
			return nil, err
		}
		if _, hasProgress := tracked[m.TeamID+"\x00"+direction]; !hasProgress && validDirection(direction) {
			dirs = append(dirs, direction)
		}
	}
	// Retired single-application records are a fallback only before stage training
	// exists. Later explicit legacy admissions receive a scoped cycle of their own.
	if len(progress) == 0 {
		apps, err := readAll[Applicant](s, "SELECT payload FROM applicants WHERE user_id=?", id)
		if err != nil {
			return nil, err
		}
		for _, ap := range apps {
			if ap.Status == "accepted" && validDirection(ap.Direction) {
				dirs = append(dirs, ap.Direction)
			}
		}
	}
	return normalizedIDs(dirs), nil
}

func appliedDirectionsAt(s *Store, user User, enrolled []string) ([]string, error) {
	dirs := append([]string{}, enrolled...)
	apps, err := readAll[TeamApplication](s, "SELECT payload FROM team_applications WHERE user_id=?", user.ID)
	if err != nil {
		return nil, err
	}
	for _, ap := range apps {
		if ap.Status != "pending" || !validDirection(ap.Direction) {
			continue
		}
		p, err := latestTrainingInScope(s, user.ID, ap.TeamID, ap.Direction)
		if err != nil {
			return nil, err
		}
		if p == nil || p.Status != "eliminated" {
			dirs = append(dirs, ap.Direction)
		}
	}
	progress, err := currentTraining(s, user.ID)
	if err != nil {
		return nil, err
	}
	if len(progress) == 0 {
		legacy, err := readAll[Applicant](s, "SELECT payload FROM applicants WHERE user_id=?", user.ID)
		if err != nil {
			return nil, err
		}
		for _, ap := range legacy {
			if validDirection(ap.Direction) && slices.Contains([]string{"submitted", "screening", "interview"}, ap.Status) {
				dirs = append(dirs, ap.Direction)
			}
		}
	}
	// Stable navigation order, independent of application creation order.
	out := []string{}
	for _, direction := range technicalDirections {
		if slices.Contains(dirs, direction) {
			out = append(out, direction)
		}
	}
	return out, nil
}

// Applicant group preference is also a scope for contacting and reviewing a new recruit.
func (a *App) recipientAllowed(u User, id, permission string) (bool, error) {
	if u.Role == "admin" {
		return true, nil
	}
	target, err := a.store.user(id)
	if err != nil {
		return false, err
	}
	if permission == "recruitment" && isStaff(target) {
		for _, d := range technicalDirections {
			if hasPermission(u, d, "recruitment") && hasPermission(target, d, "recruitment") {
				return true, nil
			}
		}
	}
	dirs, err := a.memberDirections(id)
	if err != nil {
		return false, err
	}
	if permission == "recruitment" || permission == "mail" && len(dirs) == 0 {
		modern, err := readAll[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE user_id=?", id)
		if err != nil {
			return false, err
		}
		for _, ap := range modern {
			if permission == "recruitment" || ap.Status == "pending" {
				dirs = append(dirs, ap.Direction)
			}
		}
		old, err := readAll[Applicant](a.store, "SELECT payload FROM applicants WHERE user_id=?", id)
		if err != nil {
			return false, err
		}
		for _, ap := range old {
			if permission == "recruitment" || ap.Status != "rejected" {
				dirs = append(dirs, ap.Direction)
			}
		}
	}
	for _, d := range dirs {
		if hasPermission(u, d, permission) {
			return true, nil
		}
	}
	return false, nil
}
func (a *App) updateMember(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" && !hasAnyPermission(u, "assessment") {
		return fail(403, "只有管理员可分配角色与组权限，考核管理员仅可确认本组旧制新生考核")
	}
	var in struct {
		Role             *string       `json:"role"`
		MemberType       *string       `json:"memberType"`
		GroupGrants      *[]GroupGrant `json:"groupGrants"`
		Direction        *string       `json:"direction"`
		AssessmentPassed bool          `json:"assessmentPassed"`
		AssessmentNote   string        `json:"assessmentNote"`
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
	member, err := s.user(r.PathValue("id"))
	if err != nil {
		return err
	}
	oldRole, oldType := member.Role, member.MemberType
	if u.Role != "admin" {
		if in.Role != nil || in.GroupGrants != nil || in.Direction != nil || in.MemberType == nil || !slices.Contains([]string{"reserve", "formal"}, *in.MemberType) || !in.AssessmentPassed || oldType != "freshman" || member.ID == u.ID {
			return fail(403, "组考核管理员仅可确认本组其他新生的最终考核，不能调整角色、所属组或授权")
		}
		dirs, err := memberDirectionsAt(s, member.ID)
		if err != nil {
			return err
		}
		if len(dirs) == 0 {
			return fail(403, "新生须先通过报名审核并加入本人组别")
		}
		for _, direction := range dirs {
			if err = requirePermission(u, direction, "assessment"); err != nil {
				return err
			}
		}
	}
	progress, err := latestTraining(s, member.ID)
	if err != nil {
		return err
	}
	current, err := currentTraining(s, member.ID)
	if err != nil {
		return err
	}
	for _, p := range current {
		if p.Status == "training" && in.Direction != nil && strings.TrimSpace(*in.Direction) != member.Direction {
			return fail(409, "成员仍有组别在阶段培养中，请通过各组报名和考核管理所属组")
		}
	}
	if in.MemberType != nil {
		member.MemberType = *in.MemberType
		if member.Role != "admin" && member.Role != "group_admin" {
			member.Role = memberRole(member.MemberType)
		}
	}
	if in.Role != nil {
		member.Role = *in.Role
		if in.MemberType == nil && member.Role != "admin" && member.Role != "group_admin" {
			switch member.Role {
			case "member":
				member.MemberType = "formal"
			case "reserve":
				member.MemberType = "reserve"
			case "freshman":
				member.MemberType = "freshman"
			}
		}
	}
	if in.GroupGrants != nil {
		member.GroupGrants = *in.GroupGrants
	} else if member.Role != "group_admin" {
		member.GroupGrants = []GroupGrant{}
	}
	previousDirections := []string{}
	if in.Direction != nil {
		previousDirections, err = memberDirectionsAt(s, member.ID)
		if err != nil {
			return err
		}
		member.Direction = strings.TrimSpace(*in.Direction)
	}
	if err = validateUserAccess(&member); err != nil {
		return err
	}
	if len(in.AssessmentNote) > 5000 {
		return fail(400, "考核说明过长")
	}
	if oldType == "freshman" && member.MemberType != "freshman" {
		if progress != nil {
			return fail(409, "该新生已有培养周期，请在新生阶段考核中逐阶段完成晋升")
		}
		if !in.AssessmentPassed {
			return fail(409, "请先明确确认新生最终考核通过")
		}
		member.AssessmentPassedAt = now()
		member.AssessmentConfirmedBy = u.ID
		member.AssessmentNote = strings.TrimSpace(in.AssessmentNote)
	}
	if oldRole == "admin" && member.Role != "admin" {
		users, err := readAll[User](s, "SELECT payload FROM users")
		if err != nil {
			return err
		}
		admins := 0
		for _, v := range users {
			if v.Role == "admin" {
				admins++
			}
		}
		if admins <= 1 {
			return fail(409, "不能移除最后一位管理员")
		}
	}
	if err = s.exec("UPDATE users SET payload=? WHERE id=?", encoded(member), member.ID); err != nil {
		return err
	}
	if in.Direction != nil {
		memberships, err := readAll[Membership](s, "SELECT payload FROM team_memberships WHERE user_id=?", member.ID)
		if err != nil {
			return err
		}
		if len(memberships) > 0 && member.Direction == "" {
			return fail(400, "已入队成员必须指定有效技术组")
		}
		if len(memberships) > 1 || len(current) > 1 {
			// With independent group memberships this field selects the primary group;
			// changing it must not transfer every accepted application to that group.
			if !slices.Contains(previousDirections, member.Direction) {
				return fail(400, "主技术组须选自成员当前有效的已加入组别")
			}
		} else {
			for _, m := range memberships {
				m.Direction = member.Direction
				if err = s.exec("UPDATE team_memberships SET direction=?,payload=? WHERE team_id=? AND user_id=?", m.Direction, encoded(m), m.TeamID, m.UserID); err != nil {
					return err
				}
			}
			applicants, err := readAll[Applicant](s, "SELECT payload FROM applicants WHERE user_id=?", member.ID)
			if err != nil {
				return err
			}
			for _, ap := range applicants {
				if ap.Status == "accepted" {
					ap.Direction = member.Direction
					if ap.Direction == "" {
						return fail(400, "已录取成员必须指定有效技术组")
					}
					ap.UpdatedAt = now()
					if err = s.exec("UPDATE applicants SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
						return err
					}
				}
			}
			modern, err := readAll[TeamApplication](s, "SELECT payload FROM team_applications WHERE user_id=?", member.ID)
			if err != nil {
				return err
			}
			for _, ap := range modern {
				if ap.Status == "accepted" {
					ap.Direction = member.Direction
					ap.UpdatedAt = now()
					if err = s.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
						return err
					}
				}
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if oldType == "freshman" && member.MemberType != "freshman" {
		a.activity(u, "确认新生考核通过并晋升", member.Name)
	} else {
		a.activity(u, "调整成员身份与权限", member.Name)
	}
	respond(w, 200, map[string]any{"item": member})
	return nil
}
func (a *App) scopedMemberships(u User, permission string) ([]Membership, error) {
	items, err := readAll[Membership](a.store, "SELECT payload FROM team_memberships")
	if err != nil {
		return nil, err
	}
	out := []Membership{}
	for _, m := range items {
		if m.UserID == u.ID || hasPermission(u, m.Direction, permission) {
			out = append(out, m)
		}
	}
	return out, nil
}
func (a *App) scopedRecords(u User) ([]Record, error) {
	items, err := readAll[Record](a.store, "SELECT payload FROM records ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	out := []Record{}
	for _, item := range items {
		allowed := item.UserID == u.ID
		if !allowed {
			allowed, err = a.recipientAllowed(u, item.UserID, "records")
			if err != nil {
				return nil, err
			}
		}
		if allowed {
			out = append(out, item)
		}
	}
	return out, nil
}
func (a *App) scopedTeamApplications(u User, teamID string) ([]TeamApplication, error) {
	q := "SELECT payload FROM team_applications"
	args := []any{}
	if teamID != "" {
		q += " WHERE team_id=?"
		args = append(args, teamID)
	}
	apps, err := readAll[TeamApplication](a.store, q, args...)
	if err != nil {
		return nil, err
	}
	out := []TeamApplication{}
	for _, ap := range apps {
		if err = annotateTrainingApplication(a.store, &ap); err != nil {
			return nil, err
		}
		management := hasPermission(u, ap.Direction, "recruitment")
		if ap.UserID != u.ID && !management {
			continue
		}
		if management {
			owner, err := a.store.user(ap.UserID)
			if err != nil {
				return nil, err
			}
			p, err := a.store.profile(owner)
			if err != nil {
				return nil, err
			}
			ap.Profile = &p
			ap.UserName = owner.Name
		}
		out = append(out, ap)
	}
	return out, nil
}
func (a *App) contextualFileAccess(u User, id string) (bool, error) {
	records, err := a.scopedRecords(u)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if includesID(record.FileIDs, id) {
			return true, nil
		}
	}
	apps, err := a.scopedTeamApplications(u, "")
	if err != nil {
		return false, err
	}
	for _, ap := range apps {
		if hasPermission(u, ap.Direction, "recruitment") && (ap.ResumeFileID == id || ap.Profile != nil && ap.Profile.PhotoFileID == id) {
			return true, nil
		}
	}
	profiles, err := readAll[Profile](a.store, "SELECT payload FROM profiles")
	if err != nil {
		return false, err
	}
	for _, profile := range profiles {
		if profile.PhotoFileID == id {
			allowed, err := a.recipientAllowed(u, profile.UserID, "members")
			if err != nil || allowed {
				return allowed, err
			}
			return a.recipientAllowed(u, profile.UserID, "recruitment")
		}
	}
	return false, nil
}
func (a *App) campaignAllowed(u User, item MailCampaign) (bool, error) {
	if u.Role == "admin" {
		return true, nil
	}
	if !hasAnyPermission(u, "mail") {
		return false, nil
	}
	for _, recipient := range item.Recipients {
		ok, err := a.recipientAllowed(u, recipient.UserID, "mail")
		if err != nil {
			if err == sql.ErrNoRows {
				return false, nil
			}
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func requireUpdatedFiles(s *Store, u User, ids, existing []string) error {
	if len(ids) > 20 {
		return fail(400, "每次最多选择20个文件")
	}
	seen := map[string]bool{}
	fresh := []string{}
	for _, id := range ids {
		if seen[id] {
			return fail(400, "文件不能重复")
		}
		seen[id] = true
		if !includesID(existing, id) {
			fresh = append(fresh, id)
		}
	}
	return requireOwnedFiles(s, u, fresh)
}
