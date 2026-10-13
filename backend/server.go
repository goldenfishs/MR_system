package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	Demo          bool
	Secure        bool
	PublicOrigin  string
	WebDir        string
	Mailer        VerificationMailer
	EmailFrom     string
	EmailSecurity string
	UploadDir     string
	NoticeMailer  NoticeMailer
	JudgeURL      string
	JudgeToken    string
}
type attempt struct {
	count int
	since time.Time
}
type App struct {
	store      *Store
	config     Config
	attempts   map[string]attempt
	rateMu     sync.Mutex
	platformMu sync.Mutex
	judgeSlots chan struct{}
}
type apiError struct {
	status            int
	message           string
	retryAfterSeconds int
}

func (e *apiError) Error() string       { return e.message }
func fail(status int, msg string) error { return &apiError{status: status, message: msg} }
func now() string                       { return time.Now().UTC().Format(time.RFC3339Nano) }
func randomID() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fail(400, "请求数据格式不正确")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fail(400, "请求只能包含一个 JSON 对象")
	}
	return nil
}
func isStaff(u User) bool     { return u.Role != "student" }
func canManage(u User) bool   { return u.Role == "admin" || u.Role == "mentor" }
func validTeam(t string) bool { return t == "RoboMaster" || t == "ROBOCON" || t == "RoboCon" }
func validDirection(d string) bool {
	return d == "机械" || d == "电控" || d == "视觉算法" || d == "硬件" || d == "运营"
}
func validURL(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 2000 {
		return false
	}
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}
func expired(s string) bool {
	if s == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, s)
	return err != nil || time.Now().After(t)
}
func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func normalizedEmail(s string) (string, bool) {
	if len(s) > 254 {
		return "", false
	}
	s = strings.ToLower(strings.TrimSpace(s))
	addr, e := mail.ParseAddress(s)
	return s, e == nil && addr.Address == s && len(s) <= 254
}
func (a *App) wrap(auth bool, handler func(http.ResponseWriter, *http.Request, User) error) http.HandlerFunc {
	return a.wrapWithMutationLock(auth, true, handler)
}
func (a *App) wrapWithMutationLock(auth, mutationLock bool, handler func(http.ResponseWriter, *http.Request, User) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("request panic: %v", rec)
				respond(w, 500, map[string]string{"error": "服务器处理失败，请稍后重试"})
			}
		}()
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				allowed := a.config.PublicOrigin != "" && origin == a.config.PublicOrigin
				expectedScheme := "http"
				if r.TLS != nil || a.config.Secure {
					expectedScheme = "https"
				}
				if !allowed && (err != nil || u.Host != r.Host || u.Scheme != expectedScheme || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil) {
					respond(w, 403, map[string]string{"error": "请求来源不受信任"})
					return
				}
			}
			limit := int64(1 << 20)
			if r.URL.Path == "/api/files" {
				limit = 26 << 20
			} else if r.URL.Path == "/api/auth/register" || r.URL.Path == "/api/profile" || r.URL.Path == "/api/settings/logo" {
				limit = 4 << 20
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				respond(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "请求过大或读取失败"})
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if mutationLock {
				a.store.mu.Lock()
				defer a.store.mu.Unlock()
			}
		}
		var user User
		var err error
		if auth {
			user, err = a.currentUser(r)
			if err != nil {
				respond(w, 401, map[string]string{"error": "请先登录"})
				return
			}
		}
		if err = handler(w, r, user); err != nil {
			var ae *apiError
			if errors.As(err, &ae) {
				body := map[string]any{"error": ae.message}
				if ae.status == http.StatusTooManyRequests {
					seconds := max(1, ae.retryAfterSeconds)
					w.Header().Set("Retry-After", strconv.Itoa(seconds))
					body["retryAfterSeconds"] = seconds
				}
				respond(w, ae.status, body)
			} else if errors.Is(err, sql.ErrNoRows) {
				respond(w, 404, map[string]string{"error": "记录不存在"})
			} else {
				log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
				respond(w, 500, map[string]string{"error": "服务器处理失败，请稍后重试"})
			}
		}
	}
}
func (a *App) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", a.wrap(false, func(w http.ResponseWriter, r *http.Request, u User) error {
		if err := a.store.db.PingContext(r.Context()); err != nil {
			return err
		}
		respond(w, 200, map[string]string{"status": "ok"})
		return nil
	}))
	m.HandleFunc("GET /api/public/config", a.wrap(false, a.publicConfig))
	m.HandleFunc("GET /api/public/logo/{id}", a.wrap(false, a.publicBrandLogo))
	m.HandleFunc("POST /api/settings/logo", a.wrap(true, a.uploadBrandLogo))
	m.HandleFunc("POST /api/auth/login", a.wrap(false, a.login))
	m.HandleFunc("POST /api/auth/register", a.wrap(false, a.register))
	m.HandleFunc("POST /api/auth/send-verification-code", a.wrapWithMutationLock(false, false, a.sendVerificationCode))
	m.HandleFunc("POST /api/auth/logout", a.wrap(false, a.logout))
	m.HandleFunc("GET /api/auth/me", a.wrap(true, func(w http.ResponseWriter, r *http.Request, u User) error {
		respond(w, 200, map[string]any{"user": u, "demoMode": a.config.Demo})
		return nil
	}))
	m.HandleFunc("GET /api/dashboard", a.wrap(true, a.dashboard))
	m.HandleFunc("GET /api/applicants", a.wrap(true, a.listApplicants))
	m.HandleFunc("POST /api/applicants", a.wrap(true, a.saveApplication))
	m.HandleFunc("PATCH /api/applicants/{id}", a.wrap(true, a.reviewApplication))
	m.HandleFunc("GET /api/members", a.wrap(true, a.members))
	m.HandleFunc("POST /api/members", a.wrap(true, a.createMember))
	m.HandleFunc("GET /api/tasks", a.wrap(true, a.listTasks))
	m.HandleFunc("POST /api/tasks", a.wrap(true, a.createTask))
	m.HandleFunc("GET /api/tasks/{id}/submissions", a.wrap(true, a.listSubmissions))
	m.HandleFunc("POST /api/tasks/{id}/submissions", a.wrap(true, a.submitTask))
	m.HandleFunc("PATCH /api/submissions/{id}", a.wrap(true, a.reviewSubmission))
	m.HandleFunc("GET /api/settings", a.wrap(true, a.getSettings))
	m.HandleFunc("GET /api/settings/email", a.wrap(true, a.emailSettings))
	m.HandleFunc("PATCH /api/settings", a.wrap(true, a.updateSettings))
	a.registerPlatformRoutes(m)
	a.registerTaskPlatformRoutes(m)
	a.registerLearningRoutes(m)
	m.HandleFunc("GET /api/workspace-summary", a.wrap(true, a.workspaceSummary))
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 404, map[string]string{"error": "接口不存在"})
	})
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		root, err := os.OpenRoot(a.config.WebDir)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer root.Close()
		p := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		f, err := root.Open(p)
		if err != nil {
			if filepath.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
			f, err = root.Open("index.html")
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(info.Name(), ".html") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		m.ServeHTTP(w, r)
	})
}
func (a *App) currentUser(r *http.Request) (User, error) {
	c, err := r.Cookie("robot_hub_session")
	if err != nil {
		return User{}, err
	}
	if len(c.Value) != 40 {
		return User{}, sql.ErrNoRows
	}
	return readOne[User](a.store, `SELECT u.payload FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash(c.Value), now())
}
func (a *App) setSession(w http.ResponseWriter, r *http.Request, u User) error {
	cookie, err := a.createSession(a.store, r, u)
	if err != nil {
		return err
	}
	http.SetCookie(w, cookie)
	return nil
}

// createSession accepts a transaction-scoped store so registration, challenge
// consumption, and the initial session either commit together or roll back.
func (a *App) createSession(s *Store, r *http.Request, u User) (*http.Cookie, error) {
	if c, err := r.Cookie("robot_hub_session"); err == nil {
		if err = s.exec("DELETE FROM sessions WHERE token_hash=?", tokenHash(c.Value)); err != nil {
			return nil, err
		}
	}
	token := randomID()
	expiry := time.Now().UTC().Add(24 * time.Hour)
	if err := s.exec("DELETE FROM sessions WHERE expires_at<?", now()); err != nil {
		return nil, err
	}
	if err := s.exec("INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", tokenHash(token), u.ID, expiry.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	return &http.Cookie{Name: "robot_hub_session", Value: token, Path: "/", HttpOnly: true, Secure: a.config.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 86400, Expires: expiry}, nil
}
func rateKey(r *http.Request, email string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host + "|" + email
}
func (a *App) clearRate(r *http.Request, email string) {
	a.rateMu.Lock()
	delete(a.attempts, rateKey(r, email))
	a.rateMu.Unlock()
}
func (a *App) rateAllowed(r *http.Request, email string) bool {
	host := rateKey(r, email)
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	for k, v := range a.attempts {
		if time.Since(v.since) > 15*time.Minute {
			delete(a.attempts, k)
		}
	}
	v := a.attempts[host]
	if v.since.IsZero() {
		v.since = time.Now()
	}
	v.count++
	a.attempts[host] = v
	return v.count <= 30
}
func (a *App) login(w http.ResponseWriter, r *http.Request, _ User) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	email, valid := normalizedEmail(in.Email)
	if !valid || len(in.Password) > 72 || len(in.Password) < 1 {
		return fail(401, "邮箱或密码不正确")
	}
	if !a.rateAllowed(r, email) {
		return rateLimit("尝试次数过多，请 15 分钟后重试", 900)
	}
	var raw, hash string
	err := a.store.row("SELECT payload,password_hash FROM users WHERE email=?", email).Scan(&raw, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil {
		hash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	}
	match := bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password))
	if err != nil || match != nil {
		return fail(401, "邮箱或密码不正确")
	}
	var u User
	if err = json.Unmarshal([]byte(raw), &u); err != nil {
		return err
	}
	if err = a.setSession(w, r, u); err != nil {
		return err
	}
	a.clearRate(r, email)
	respond(w, 200, map[string]any{"user": u})
	return nil
}
func (a *App) logout(w http.ResponseWriter, r *http.Request, _ User) error {
	if c, err := r.Cookie("robot_hub_session"); err == nil {
		if err = a.store.exec("DELETE FROM sessions WHERE token_hash=?", tokenHash(c.Value)); err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "robot_hub_session", Path: "/", HttpOnly: true, Secure: a.config.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	respond(w, 200, map[string]bool{"ok": true})
	return nil
}
func (a *App) publicConfig(w http.ResponseWriter, r *http.Request, _ User) error {
	s, err := a.store.settings()
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"organization": s.Organization, "season": s.Season, "demoMode": a.config.Demo, "applicationOpen": s.ApplicationOpen, "applicationDeadline": s.ApplicationDeadline, "emailVerificationRequired": true, "registrationEnabled": a.config.Mailer != nil, "brandName": s.BrandName, "logoUrl": s.LogoURL, "teamName": s.TeamName, "secondaryTeamName": s.SecondaryTeamName, "competitions": s.Competitions})
	return nil
}
func (a *App) activity(u User, action, target string) {
	v := Activity{ID: randomID(), Actor: u.Name, Action: action, Target: target, CreatedAt: now()}
	if err := a.store.exec("INSERT INTO activities(id,user_id,created_at,payload) VALUES(?,?,?,?)", v.ID, u.ID, v.CreatedAt, encoded(v)); err != nil {
		log.Printf("activity write: %v", err)
	}
}
func sortApplicants(items []Applicant) {
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
}
func (a *App) scopedApplicants(u User) ([]Applicant, error) {
	q := "SELECT payload FROM applicants"
	var args []any
	if u.Role == "student" {
		q += " WHERE user_id=?"
		args = append(args, u.ID)
	}
	items, err := readAll[Applicant](a.store, q, args...)
	if err != nil {
		return nil, err
	}
	out := []Applicant{}
	for _, v := range items {
		if u.Role == "interviewer" && v.InterviewerID != u.ID {
			continue
		}
		if u.Role == "student" {
			v.Notes = ""
			v.Score = nil
		}
		out = append(out, v)
	}
	sortApplicants(out)
	return out, nil
}
func (a *App) listApplicants(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := a.scopedApplicants(u)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"items": items})
	return nil
}
func (a *App) saveApplication(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "student" && u.Role != "admin" {
		return fail(403, "只有报名者或管理员可填写报名表")
	}
	s, err := a.store.settings()
	if err != nil {
		return err
	}
	if !s.ApplicationOpen || expired(s.ApplicationDeadline) {
		return fail(409, "本轮报名已关闭")
	}
	var in Applicant
	if err = decode(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 || len(in.Phone) < 5 || len(in.Phone) > 32 || strings.TrimSpace(in.University) == "" || len(in.University) > 200 || strings.TrimSpace(in.Major) == "" || len(in.Major) > 200 || in.Year == "" || len(in.Year) > 40 || !validTeam(in.Team) || !validDirection(in.Direction) || (in.SecondDirection != "" && (!validDirection(in.SecondDirection) || in.SecondDirection == in.Direction)) || strings.TrimSpace(in.Motivation) == "" || len(in.Motivation) > 12000 || len(in.Experience) > 20000 || !validURL(in.PortfolioURL) {
		return fail(400, "请完整填写报名信息，检查志愿和作品链接")
	}
	existing, err := readOne[Applicant](a.store, "SELECT payload FROM applicants WHERE user_id=?", u.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existing.Status != "submitted" && existing.Status != "screening" {
		return fail(409, "当前报名阶段不能修改资料")
	}
	in.UserID = u.ID
	in.Email = u.Email
	in.ID = existing.ID
	in.CreatedAt = existing.CreatedAt
	in.Status = existing.Status
	in.InterviewAt = existing.InterviewAt
	in.InterviewerID = existing.InterviewerID
	in.Score = existing.Score
	in.Notes = existing.Notes
	if in.ID == "" {
		in.ID = randomID()
		in.CreatedAt = now()
		in.Status = "submitted"
	}
	in.UpdatedAt = now()
	if err = a.store.exec("INSERT INTO applicants(id,user_id,payload) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET payload=excluded.payload", in.ID, in.UserID, encoded(in)); err != nil {
		return err
	}
	a.activity(u, "提交报名", in.Team+" · "+in.Direction)
	if u.Role == "student" {
		in.Notes = ""
		in.Score = nil
	}
	respond(w, 200, map[string]any{"item": in})
	return nil
}
func (a *App) reviewApplication(w http.ResponseWriter, r *http.Request, u User) error {
	item, err := a.store.applicant(r.PathValue("id"))
	if err != nil {
		return err
	}
	if u.Role != "admin" && (u.Role != "interviewer" || item.InterviewerID != u.ID) {
		return fail(403, "没有评价该候选人的权限")
	}
	var in struct {
		Status        *string  `json:"status"`
		InterviewAt   *string  `json:"interviewAt"`
		InterviewerID *string  `json:"interviewerId"`
		Score         *float64 `json:"score"`
		Notes         *string  `json:"notes"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if u.Role != "admin" && (in.Status != nil || in.InterviewAt != nil || in.InterviewerID != nil) {
		return fail(403, "面试官只能填写评分与评语")
	}
	if in.Status != nil {
		switch *in.Status {
		case "submitted", "screening", "interview", "accepted", "rejected":
			item.Status = *in.Status
		default:
			return fail(400, "无效的报名状态")
		}
	}
	if in.InterviewAt != nil {
		if !validDate(*in.InterviewAt) {
			return fail(400, "面试时间格式不正确")
		}
		item.InterviewAt = *in.InterviewAt
	}
	if in.InterviewerID != nil {
		if *in.InterviewerID != "" {
			interviewer, e := a.store.user(*in.InterviewerID)
			if e != nil || (interviewer.Role != "interviewer" && interviewer.Role != "admin") {
				return fail(400, "请选择有效的面试官或管理员")
			}
		}
		item.InterviewerID = *in.InterviewerID
	}
	if in.Score != nil {
		if !finite(*in.Score) || *in.Score < 0 || *in.Score > 100 {
			return fail(400, "面试评分必须在 0–100 之间")
		}
		item.Score = in.Score
	}
	if in.Notes != nil {
		if len(*in.Notes) > 20000 {
			return fail(400, "评语过长")
		}
		item.Notes = *in.Notes
	}
	item.UpdatedAt = now()
	owner, err := a.store.user(item.UserID)
	if err != nil {
		return err
	}
	if item.Status == "accepted" {
		owner.Team = item.Team
		owner.Direction = item.Direction
	} else {
		owner.Team = ""
		owner.Direction = ""
	}
	tx, err := a.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(a.store.q("UPDATE applicants SET payload=? WHERE id=?"), encoded(item), item.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(a.store.q("UPDATE users SET payload=? WHERE id=?"), encoded(owner), owner.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "更新面试评价", item.Name)
	respond(w, 200, map[string]any{"item": item})
	return nil
}
func (a *App) memberList() ([]User, error) {
	users, err := readAll[User](a.store, "SELECT payload FROM users")
	if err != nil {
		return nil, err
	}
	applications, err := readAll[Applicant](a.store, "SELECT payload FROM applicants")
	if err != nil {
		return nil, err
	}
	admitted := map[string]bool{}
	memberships, err := readAll[Membership](a.store, "SELECT payload FROM team_memberships")
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		admitted[membership.UserID] = true
	}
	for _, v := range applications {
		if v.Status == "accepted" {
			admitted[v.UserID] = true
		}
	}
	items := []User{}
	for _, u := range users {
		if isStaff(u) || admitted[u.ID] {
			items = append(items, u)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].JoinedAt < items[j].JoinedAt })
	return items, nil
}
func (a *App) members(w http.ResponseWriter, r *http.Request, u User) error {
	if !isStaff(u) {
		return fail(403, "仅工作人员可查看成员列表")
	}
	items, err := a.memberList()
	if err != nil {
		return err
	}
	teams, err := readAll[Team](a.store, "SELECT payload FROM teams")
	if err != nil {
		return err
	}
	memberships, err := readAll[Membership](a.store, "SELECT payload FROM team_memberships")
	if err != nil {
		return err
	}
	type memberTeam struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Competition  string   `json:"competition"`
		Competitions []string `json:"competitions"`
		Direction    string   `json:"direction"`
	}
	type memberEntry struct {
		User
		Teams []memberTeam `json:"teams"`
	}
	teamMap := map[string]Team{}
	for _, team := range teams {
		teamMap[team.ID] = team
	}
	result := []memberEntry{}
	for _, member := range items {
		entry := memberEntry{User: member, Teams: []memberTeam{}}
		for _, membership := range memberships {
			if membership.UserID == member.ID {
				team := teamMap[membership.TeamID]
				normalizeTeam(&team)
				entry.Teams = append(entry.Teams, memberTeam{team.ID, team.Name, team.Competition, team.Competitions, membership.Direction})
			}
		}
		result = append(result, entry)
	}
	respond(w, 200, map[string]any{"items": result})
	return nil
}
func (a *App) getSettings(w http.ResponseWriter, r *http.Request, u User) error {
	s, err := a.store.settings()
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"settings": s})
	return nil
}
func (a *App) updateSettings(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可修改系统设置")
	}
	s, err := a.store.settings()
	if err != nil {
		return err
	}
	var in struct {
		Organization        *string   `json:"organization"`
		BrandName           *string   `json:"brandName"`
		LogoURL             *string   `json:"logoUrl"`
		TeamName            *string   `json:"teamName"`
		SecondaryTeamName   *string   `json:"secondaryTeamName"`
		Competitions        *[]string `json:"competitions"`
		Season              *string   `json:"season"`
		ApplicationOpen     *bool     `json:"applicationOpen"`
		ApplicationDeadline *string   `json:"applicationDeadline"`
		HydroURL            *string   `json:"hydroUrl"`
		Announcement        *string   `json:"announcement"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if in.Organization != nil {
		s.Organization = strings.TrimSpace(*in.Organization)
	}
	if in.Season != nil {
		s.Season = strings.TrimSpace(*in.Season)
	}
	if in.ApplicationOpen != nil {
		s.ApplicationOpen = *in.ApplicationOpen
	}
	if in.ApplicationDeadline != nil {
		s.ApplicationDeadline = *in.ApplicationDeadline
	}
	if in.HydroURL != nil {
		s.HydroURL = *in.HydroURL
	}
	if in.Announcement != nil {
		s.Announcement = *in.Announcement
	}
	if in.BrandName != nil {
		s.BrandName = strings.TrimSpace(*in.BrandName)
	}
	if in.LogoURL != nil {
		if strings.HasPrefix(strings.TrimSpace(*in.LogoURL), "/api/public/logo/") && strings.TrimSpace(*in.LogoURL) != s.LogoURL {
			return fail(400, "请通过标志上传入口设置本站品牌图片")
		}
		s.LogoURL = strings.TrimSpace(*in.LogoURL)
	}
	if in.TeamName != nil {
		s.TeamName = strings.TrimSpace(*in.TeamName)
	}
	if in.SecondaryTeamName != nil {
		s.SecondaryTeamName = strings.TrimSpace(*in.SecondaryTeamName)
	}
	if in.Competitions != nil {
		s.Competitions = normalizeCompetitions(*in.Competitions)
	}
	if err := validateBrand(s); err != nil {
		return err
	}
	if s.Organization == "" || len(s.Organization) > 150 || s.Season == "" || len(s.Season) > 80 || !validDate(s.ApplicationDeadline) || !validURL(s.HydroURL) || len(s.Announcement) > 10000 {
		return fail(400, "设置内容不合法，请检查名称、日期和链接")
	}
	if err = a.store.saveSettings(s); err != nil {
		return err
	}
	a.activity(u, "更新系统设置", s.Organization)
	respond(w, 200, map[string]any{"settings": s})
	return nil
}
func taskVisible(t Task, u User) bool {
	if isStaff(u) {
		return true
	}
	return (t.Team == "" || u.Team == "" || sameCompetition(t.Team, u.Team)) && (t.Direction == "" || u.Direction == "" || t.Direction == u.Direction)
}
func (a *App) tasksFor(u User) ([]Task, error) {
	tasks, err := readAll[Task](a.store, "SELECT payload FROM tasks")
	if err != nil {
		return nil, err
	}
	subs, err := readAll[Submission](a.store, "SELECT payload FROM submissions ORDER BY version DESC")
	if err != nil {
		return nil, err
	}
	runs, err := readAll[JudgeRun](a.store, "SELECT payload FROM judge_runs")
	if err != nil {
		return nil, err
	}
	result := []Task{}
	for _, t := range tasks {
		normalizeTask(&t)
		t.SubmissionCount, t.AttemptCount, t.AcceptedCount = 0, 0, 0
		t.MySubmission = nil
		t.MyAccepted = false
		for _, run := range runs {
			if run.TaskID == t.ID && run.Status == "completed" {
				t.AttemptCount++
				if run.Total > 0 && run.Passed == run.Total {
					t.AcceptedCount++
					if run.UserID == u.ID {
						t.MyAccepted = true
					}
				}
			}
		}
		visible, visibilityErr := a.taskVisibleTo(t, u)
		if visibilityErr != nil {
			return nil, visibilityErr
		}
		if !visible {
			continue
		}
		seen := map[string]bool{}
		for _, s := range subs {
			if s.TaskID != t.ID {
				continue
			}
			if !seen[s.UserID] {
				if isStaff(u) {
					t.SubmissionCount++
				}
				seen[s.UserID] = true
			}
			if s.UserID == u.ID && t.MySubmission == nil {
				copy := s
				t.MySubmission = &copy
			}
		}
		result = append(result, publicTask(t, u))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}
func (a *App) listTasks(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := a.tasksFor(u)
	if err != nil {
		return err
	}
	filtered := []Task{}
	query := r.URL.Query()
	for _, t := range items {
		if category := query.Get("category"); category != "" && t.Category != category {
			continue
		}
		if direction := query.Get("direction"); direction != "" && t.Direction != "" && t.Direction != direction {
			continue
		}
		if tag := query.Get("tag"); tag != "" && !includesID(t.Tags, tag) {
			continue
		}
		if search := strings.ToLower(strings.TrimSpace(query.Get("search"))); search != "" && !strings.Contains(strings.ToLower(t.Title+" "+t.Description+" "+strings.Join(t.Tags, " ")), search) {
			continue
		}
		filtered = append(filtered, t)
	}
	respond(w, 200, map[string]any{"items": filtered})
	return nil
}
func (a *App) createTask(w http.ResponseWriter, r *http.Request, u User) error {
	if !canManage(u) {
		return fail(403, "只有管理员或导师可发布任务")
	}
	var t Task
	if err := decode(r, &t); err != nil {
		return err
	}
	t.Title = strings.TrimSpace(t.Title)
	t.Description = strings.TrimSpace(t.Description)
	if t.Title == "" || len(t.Title) > 250 || t.Description == "" || len(t.Description) > 30000 || (t.Team != "" && !validTeam(t.Team)) || (t.Direction != "" && !validDirection(t.Direction)) || !validDate(t.DueAt) || !validURL(t.ResourcesURL) || !finite(t.MaxScore) || t.MaxScore <= 0 || t.MaxScore > 1000 {
		return fail(400, "请填写有效的任务内容、截止日期、分值和适用范围")
	}
	switch t.Kind {
	case "code", "document", "hardware", "quiz":
	default:
		return fail(400, "无效的任务类型")
	}
	switch t.Difficulty {
	case "入门", "进阶", "挑战":
	default:
		return fail(400, "无效的任务难度")
	}
	if err := a.validateTaskPlatform(&t, u); err != nil {
		return err
	}
	t.ID = randomID()
	t.CreatedBy = u.ID
	t.CreatedAt = now()
	t.MySubmission = nil
	t.SubmissionCount, t.AcceptedCount, t.AttemptCount = 0, 0, 0
	t.Locked, t.MyAccepted = false, false
	if err := a.store.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", t.ID, encoded(t)); err != nil {
		return err
	}
	a.activity(u, "发布培养任务", t.Title)
	respond(w, 201, map[string]any{"item": t})
	return nil
}
func (a *App) listSubmissions(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role == "interviewer" {
		return fail(403, "面试官没有作业评阅权限")
	}
	if _, err := a.store.task(r.PathValue("id")); err != nil {
		return err
	}
	items, err := a.store.submissions(r.PathValue("id"))
	if err != nil {
		return err
	}
	if u.Role == "student" {
		filtered := []Submission{}
		for _, s := range items {
			if s.UserID == u.ID {
				filtered = append(filtered, s)
			}
		}
		items = filtered
	}
	respond(w, 200, map[string]any{"items": items})
	return nil
}
func (a *App) submitTask(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "student" {
		return fail(403, "只有录取成员可提交任务")
	}
	t, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = a.requireTaskSubmission(t, u); err != nil {
		return err
	}
	var in struct {
		FileIDs     []string `json:"fileIds"`
		JudgeRunID  string   `json:"judgeRunId"`
		Content     string   `json:"content"`
		RepoURL     string   `json:"repoUrl"`
		ArtifactURL string   `json:"artifactUrl"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" && in.RepoURL == "" && in.ArtifactURL == "" && len(in.FileIDs) == 0 {
		return fail(400, "请填写提交说明或成果链接")
	}
	if len(in.Content) > 50000 || !validURL(in.RepoURL) || !validURL(in.ArtifactURL) {
		return fail(400, "提交说明过长或链接无效")
	}
	if err = a.validateSubmissionFiles(t, u, in.FileIDs, in.JudgeRunID); err != nil {
		return err
	}
	var version int
	if err = a.store.row("SELECT COALESCE(MAX(version),0) FROM submissions WHERE task_id=? AND user_id=?", t.ID, u.ID).Scan(&version); err != nil {
		return err
	}
	s := Submission{FileIDs: normalizedIDs(in.FileIDs), JudgeRunID: in.JudgeRunID, ID: randomID(), TaskID: t.ID, UserID: u.ID, UserName: u.Name, Content: in.Content, RepoURL: in.RepoURL, ArtifactURL: in.ArtifactURL, Version: version + 1, Status: "pending", CreatedAt: now()}
	if err = a.store.exec("INSERT INTO submissions(id,task_id,user_id,version,payload) VALUES(?,?,?,?,?)", s.ID, s.TaskID, s.UserID, s.Version, encoded(s)); err != nil {
		return err
	}
	a.activity(u, "提交任务成果", t.Title)
	respond(w, 201, map[string]any{"item": s})
	return nil
}
func (a *App) reviewSubmission(w http.ResponseWriter, r *http.Request, u User) error {
	if !canManage(u) {
		return fail(403, "只有管理员或导师可审核任务")
	}
	s, err := readOne[Submission](a.store, "SELECT payload FROM submissions WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	t, err := a.store.task(s.TaskID)
	if err != nil {
		return err
	}
	var latest int
	if err = a.store.row("SELECT MAX(version) FROM submissions WHERE task_id=? AND user_id=?", s.TaskID, s.UserID).Scan(&latest); err != nil {
		return err
	}
	if latest != s.Version {
		return fail(409, "已有新版本，请审核最新提交")
	}
	var in struct {
		Status   string   `json:"status"`
		Score    *float64 `json:"score"`
		Feedback string   `json:"feedback"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if (in.Status != "approved" && in.Status != "changes_requested") || in.Score == nil || !finite(*in.Score) || *in.Score < 0 || *in.Score > t.MaxScore || len(in.Feedback) > 20000 {
		return fail(400, "审核状态或分值不合法")
	}
	if in.Status == "changes_requested" && strings.TrimSpace(in.Feedback) == "" {
		return fail(400, "请说明需要修改的内容")
	}
	s.Status = in.Status
	s.Score = in.Score
	s.Feedback = in.Feedback
	s.ReviewedBy = u.ID
	s.ReviewedAt = now()
	if err = a.store.exec("UPDATE submissions SET payload=? WHERE id=?", encoded(s), s.ID); err != nil {
		return err
	}
	a.activity(u, "审核任务成果", t.Title+" · "+s.UserName)
	respond(w, 200, map[string]any{"item": s})
	return nil
}
func (a *App) dashboard(w http.ResponseWriter, r *http.Request, u User) error {
	apps, err := a.scopedApplicants(u)
	if err != nil {
		return err
	}
	tasks, err := a.tasksFor(u)
	if err != nil {
		return err
	}
	members, err := a.memberList()
	if err != nil {
		return err
	}
	subs, err := readAll[Submission](a.store, "SELECT payload FROM submissions ORDER BY version DESC")
	if err != nil {
		return err
	}
	pending := 0
	seen := map[string]bool{}
	for _, s := range subs {
		key := s.TaskID + "/" + s.UserID
		if seen[key] {
			continue
		}
		seen[key] = true
		if (u.Role != "student" || s.UserID == u.ID) && s.Status == "pending" {
			pending++
		}
	}
	if u.Role == "interviewer" {
		pending = 0
	}
	active := 0
	for _, t := range tasks {
		if !expired(t.DueAt) {
			active++
		}
	}
	memberCount := len(members)
	if u.Role == "student" {
		memberCount = 0
		for _, m := range members {
			if m.ID == u.ID {
				memberCount = 1
			}
		}
	}
	labels := []struct {
		Status string
		Label  string
	}{{"submitted", "待筛选"}, {"screening", "筛选中"}, {"interview", "待面试"}, {"accepted", "已录取"}, {"rejected", "未录取"}}
	pipeline := []map[string]any{}
	for _, label := range labels {
		count := 0
		for _, ap := range apps {
			if ap.Status == label.Status {
				count++
			}
		}
		pipeline = append(pipeline, map[string]any{"status": label.Status, "label": label.Label, "count": count})
	}
	upcoming := []Applicant{}
	for _, ap := range apps {
		if ap.Status == "interview" && ap.InterviewAt != "" && !expired(ap.InterviewAt) {
			upcoming = append(upcoming, ap)
		}
	}
	sort.Slice(upcoming, func(i, j int) bool { return upcoming[i].InterviewAt < upcoming[j].InterviewAt })
	if len(upcoming) > 8 {
		upcoming = upcoming[:8]
	}
	q := "SELECT payload FROM activities"
	var args []any
	if u.Role == "student" || u.Role == "interviewer" {
		q += " WHERE user_id=?"
		args = append(args, u.ID)
	}
	q += " ORDER BY created_at DESC LIMIT 8"
	activities, err := readAll[Activity](a.store, q, args...)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"stats": map[string]int{"applicants": len(apps), "pendingReviews": pending, "activeTasks": active, "members": memberCount}, "pipeline": pipeline, "recentActivity": activities, "upcomingInterviews": upcoming})
	return nil
}

func newApp(store *Store, config Config) *App {
	return &App{store: store, config: config, attempts: map[string]attempt{}, judgeSlots: make(chan struct{}, 2)}
}
func env(name, fallback string) string {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	return v
}
func main() {
	store, err := openStore(os.Getenv("DATABASE_URL"), env("DATA_DIR", "./data"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.db.Close()
	demo := os.Getenv("DEMO_MODE") == "true"
	if err = initialize(store, demo); err != nil {
		log.Fatal(err)
	}
	mailer, emailConfig, err := smtpMailerFromEnv(demo)
	if err != nil {
		log.Fatal(err)
	}
	var noticeMailer NoticeMailer
	if value, ok := mailer.(NoticeMailer); ok {
		noticeMailer = value
	}
	app := newApp(store, Config{Mailer: mailer, NoticeMailer: noticeMailer, EmailFrom: emailConfig.From, EmailSecurity: emailConfig.Security, Demo: demo, Secure: os.Getenv("SESSION_SECURE") == "true", PublicOrigin: os.Getenv("PUBLIC_ORIGIN"), WebDir: env("WEB_DIR", "../frontend/dist"), UploadDir: env("UPLOAD_DIR", filepath.Join(env("DATA_DIR", "./data"), "uploads")), JudgeURL: os.Getenv("JUDGE_URL"), JudgeToken: os.Getenv("JUDGE_TOKEN")})
	if err = validateJudgeURL(app.config.JudgeURL); err != nil {
		log.Fatal(err)
	}
	if err = app.recoverPlatformJobs(); err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: env("ADDR", "127.0.0.1:8080"), Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 120 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	log.Printf("Robot Team Hub listening on %s (demo=%v)", server.Addr, demo)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(fmt.Errorf("serve: %w", err))
	}
}

func (a *App) createMember(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可创建工作人员账号")
	}
	var in struct {
		Name      string `json:"name"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		Role      string `json:"role"`
		Team      string `json:"team"`
		Direction string `json:"direction"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	email, valid := normalizedEmail(in.Email)
	in.Name = strings.TrimSpace(in.Name)
	if !valid || in.Name == "" || len(in.Name) > 100 || len(in.Password) < 12 || len(in.Password) > 72 || (in.Role != "mentor" && in.Role != "interviewer") || (in.Team != "" && !validTeam(in.Team)) || (in.Direction != "" && !validDirection(in.Direction)) {
		return fail(400, "请填写有效的姓名、邮箱、角色与 12–72 字节密码")
	}
	var count int
	if err := a.store.row("SELECT COUNT(*) FROM users WHERE email=?", email).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fail(409, "该邮箱已注册")
	}
	member, err := createUser(a.store, in.Name, email, in.Password, in.Role, in.Team, in.Direction)
	if err != nil {
		return err
	}
	a.activity(u, "创建工作人员账号", member.Name)
	respond(w, 201, map[string]any{"item": member})
	return nil
}
