package main

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

type TeamInterview struct {
	Status    string `json:"status"`
	At        string `json:"at"`
	Room      string `json:"room"`
	Order     int    `json:"order"`
	Note      string `json:"note"`
	DecidedBy string `json:"decidedBy,omitempty"`
	DecidedAt string `json:"decidedAt,omitempty"`
}

func (a *App) registerInterviewRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/interviews", a.wrap(true, a.listInterviews))
	m.HandleFunc("PATCH /api/interviews/schedule", a.wrap(true, a.scheduleInterviews))
	m.HandleFunc("POST /api/interviews/{id}/decision", a.wrap(true, a.decideInterview))
	m.HandleFunc("POST /api/interviews/notifications", a.wrap(true, a.interviewNotifications))
	m.HandleFunc("POST /api/interviews/legacy-round", a.wrap(true, a.configureLegacyInterviewRound))
}
func interviewOf(ap TeamApplication) TeamInterview {
	if ap.Interview != nil {
		return *ap.Interview
	}
	status := "pending"
	if ap.Status == "accepted" {
		status = "passed"
	}
	if ap.Status == "rejected" {
		status = "rejected"
	}
	return TeamInterview{Status: status}
}
func (a *App) listInterviews(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := a.scopedTeamApplications(u, "")
	if err != nil {
		return err
	}
	for i := range items {
		value := interviewOf(items[i])
		items[i].Interview = &value
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt < items[j].CreatedAt })
	respond(w, 200, map[string]any{"items": items})
	return nil
}
func interviewApplication(s *Store, u User, id string) (TeamApplication, error) {
	ap, err := readOne[TeamApplication](s, "SELECT payload FROM team_applications WHERE id=?", id)
	if err != nil {
		return ap, err
	}
	if err = requirePermission(u, ap.Direction, "recruitment"); err != nil {
		return ap, err
	}
	if ap.UserID == u.ID && u.Role != "admin" {
		return ap, fail(403, "不能安排或审核自己的面试")
	}
	return ap, nil
}
func (a *App) scheduleInterviews(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		ApplicationIDs  []string `json:"applicationIds"`
		StartAt         string   `json:"startAt"`
		Room            string   `json:"room"`
		IntervalMinutes int      `json:"intervalMinutes"`
		StartOrder      int      `json:"startOrder"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	start, err := time.Parse(time.RFC3339, in.StartAt)
	in.Room = strings.TrimSpace(in.Room)
	if err != nil || !start.After(time.Now()) || in.Room == "" || len(in.Room) > 300 || len(in.ApplicationIDs) < 1 || len(in.ApplicationIDs) > 200 || len(normalizedIDs(in.ApplicationIDs)) != len(in.ApplicationIDs) || in.IntervalMinutes < 1 || in.IntervalMinutes > 120 || in.StartOrder < 1 || in.StartOrder > 10000 {
		return fail(400, "请选择 1–200 份报名，填写未来的面试时间、教室、起始序号及 1–120 分钟间隔")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	out := []TeamApplication{}
	for i, id := range in.ApplicationIDs {
		ap, err := interviewApplication(s, u, id)
		if err != nil {
			return err
		}
		v := interviewOf(ap)
		if ap.Status != "pending" || v.Status != "pending" {
			return fail(409, "只能安排待面试报名；已录取或已确认结果的报名不能重复排期")
		}
		v.At = start.Add(time.Duration(i*in.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339)
		v.Room = in.Room
		v.Order = in.StartOrder + i
		ap.Interview = &v
		ap.UpdatedAt = now()
		if err = s.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
			return err
		}
		out = append(out, ap)
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "安排面试", fmt.Sprintf("%s · %d 人", in.Room, len(out)))
	respond(w, 200, map[string]any{"items": out})
	return nil
}
func (a *App) decideInterview(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if !slices.Contains([]string{"passed", "rejected"}, in.Status) || len(in.Note) > 5000 {
		return fail(400, "请填写有效面试结果及说明")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	ap, err := interviewApplication(s, u, r.PathValue("id"))
	if err != nil {
		return err
	}
	v := interviewOf(ap)
	if ap.Status != "pending" || v.Status != "pending" {
		return fail(409, "该报名已处理，请刷新查看结果")
	}
	if in.Status == "passed" {
		if err = admitTraining(s, u, ap.UserID, ap.TeamID, ap.Direction, false); err != nil {
			return err
		}
		member := Membership{TeamID: ap.TeamID, UserID: ap.UserID, Direction: ap.Direction, JoinedAt: now()}
		if err = s.exec("INSERT INTO team_memberships(team_id,user_id,direction,joined_at,payload) VALUES(?,?,?,?,?) ON CONFLICT(team_id,user_id) DO UPDATE SET direction=excluded.direction,payload=excluded.payload", member.TeamID, member.UserID, member.Direction, member.JoinedAt, encoded(member)); err != nil {
			return err
		}
		ap.Status = "accepted"
	} else {
		ap.Status = "rejected"
	}
	v.Status = in.Status
	v.Note = strings.TrimSpace(in.Note)
	v.DecidedBy = u.ID
	v.DecidedAt = now()
	ap.Interview = &v
	ap.Feedback = v.Note
	ap.UpdatedAt = now()
	if err = s.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "确认面试结果", ap.Direction+" · "+ap.UserName+" · "+in.Status)
	respond(w, 200, map[string]any{"item": ap})
	return nil
}
func (a *App) mailRecipientAllowed(u User, recipient MailRecipient) (bool, error) {
	if recipient.InterviewApplicationID == "" {
		return a.recipientAllowed(u, recipient.UserID, "mail")
	}
	ap, err := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", recipient.InterviewApplicationID)
	if err != nil {
		return false, err
	}
	return ap.UserID == recipient.UserID && hasPermission(u, ap.Direction, "recruitment") && hasPermission(u, ap.Direction, "mail"), nil
}
func (a *App) interviewDeliveryValid(recipient MailRecipient) bool {
	if recipient.InterviewApplicationID == "" {
		return true
	}
	ap, err := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE id=?", recipient.InterviewApplicationID)
	if err != nil {
		return false
	}
	v := interviewOf(ap)
	return ap.Status == "pending" && v.Status == "pending" && v.At+"|"+v.Room+fmt.Sprint(v.Order) == recipient.InterviewSchedule && !expired(v.At)
}
func (a *App) interviewNotifications(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		ApplicationIDs []string `json:"applicationIds"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if len(in.ApplicationIDs) < 1 || len(in.ApplicationIDs) > 200 || len(normalizedIDs(in.ApplicationIDs)) != len(in.ApplicationIDs) {
		return fail(400, "请选择 1–200 份已安排面试的报名")
	}
	campaign := MailCampaign{ID: randomID(), Subject: "QUT机器人战队 · 面试通知", Body: "请查看各收件人的专属面试安排。", Status: "draft", CreatedBy: u.ID, CreatedAt: now(), UpdatedAt: now(), Recipients: []MailRecipient{}}
	for _, id := range in.ApplicationIDs {
		ap, err := interviewApplication(a.store, u, id)
		if err != nil {
			return err
		}
		if err = requirePermission(u, ap.Direction, "mail"); err != nil {
			return err
		}
		v := interviewOf(ap)
		at, err := time.Parse(time.RFC3339, v.At)
		if err != nil || !at.After(time.Now()) || v.Room == "" || v.Order < 1 || v.Status != "pending" || ap.Status != "pending" {
			return fail(409, "请先为所选待面试人员安排未来的时间、教室和顺序")
		}
		member, err := a.store.user(ap.UserID)
		if err != nil {
			return err
		}
		team, err := readOne[Team](a.store, "SELECT payload FROM teams WHERE id=?", ap.TeamID)
		if err != nil {
			return err
		}
		body := fmt.Sprintf("%s同学，你好！\n\n你报名的 %s（%s组）已安排面试。\n面试时间：%s（北京时间）\n面试地点：%s\n面试序号：%d\n\n请提前 10 分钟到场。若无法按时参加，请联系组管理员。最新安排可在网站的面试管理页面查看。\n\nQUT机器人战队", member.Name, team.Name, ap.Direction, at.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04"), v.Room, v.Order)
		campaign.Recipients = append(campaign.Recipients, MailRecipient{UserID: member.ID, Name: member.Name, Email: member.Email, Status: "pending", Body: body, InterviewApplicationID: ap.ID, InterviewSchedule: v.At + "|" + v.Room + fmt.Sprint(v.Order)})
	}
	if err := a.store.exec("INSERT INTO mail_campaigns(id,status,created_by,payload) VALUES(?,?,?,?)", campaign.ID, campaign.Status, u.ID, encoded(campaign)); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"item": campaign})
	return nil
}

// Explicit one-round setup; normal future teams never inherit this exception.
func (a *App) configureLegacyInterviewRound(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可以设置本轮历史面试特例")
	}
	var in struct {
		TeamIDs []string `json:"teamIds"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if len(in.TeamIDs) < 1 || len(in.TeamIDs) > 20 {
		return fail(400, "请选择本轮招募团队")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	count := 0
	for _, id := range normalizedIDs(in.TeamIDs) {
		team, err := readOne[Team](s, "SELECT payload FROM teams WHERE id=?", id)
		if err != nil {
			return err
		}
		team.InterviewExemptDirections = []string{}
		for _, d := range team.Directions {
			if d != "运营" {
				team.InterviewExemptDirections = append(team.InterviewExemptDirections, d)
			}
		}
		if err = s.exec("UPDATE teams SET payload=? WHERE id=?", encoded(team), id); err != nil {
			return err
		}
		apps, err := readAll[TeamApplication](s, "SELECT payload FROM team_applications WHERE team_id=?", id)
		if err != nil {
			return err
		}
		for _, ap := range apps {
			if ap.Interview != nil {
				continue
			}
			v := interviewOf(ap)
			if ap.Direction != "运营" && ap.Status != "rejected" {
				v.Status = "exempt"
				v.Note = "本轮已完成线下面试（历史招募特例）"
				v.DecidedBy = u.ID
				v.DecidedAt = now()
			}
			ap.Interview = &v
			ap.UpdatedAt = now()
			if err = s.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
				return err
			}
			count++
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "设置本轮历史面试特例", fmt.Sprintf("%d 份报名，运营组正常面试，保留已有录取与考核记录", count))
	respond(w, 200, map[string]any{"updatedCount": count})
	return nil
}
