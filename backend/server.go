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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Demo                bool
	Secure              bool
	PublicOrigin        string
	TrustedProxyIPs     string
	WebDir              string
	Mailer              VerificationMailer
	EmailFrom           string
	EmailSecurity       string
	UploadDir           string
	NoticeMailer        NoticeMailer
	JudgeURL            string
	JudgeToken          string
	CertificateRenderer CertificateRenderer
}
type attempt struct {
	count int
	since time.Time
}
type App struct {
	store       *Store
	config      Config
	attempts    map[string]attempt
	rateMu      sync.Mutex
	platformMu  sync.Mutex
	judgeSlots  chan struct{}
	resetSlots  chan struct{}
	mailMu      sync.RWMutex
	mailService *mailServiceSnapshot
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
	limit := int64(4 << 20)
	if r.Method == http.MethodPatch && r.URL.Path == "/api/profile" {
		// A profile update may contain both independent 2 MiB images encoded as
		// base64. Keep this larger limit scoped to that endpoint.
		limit = 6 << 20
	}
	d := json.NewDecoder(io.LimitReader(r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fail(400, "请求数据格式不正确")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fail(400, "请求只能包含一个 JSON 对象")
	}
	return nil
}
func isStaff(u User) bool {
	return u.DeactivatedAt == "" && (u.Role == "admin" || u.Role == "group_admin")
}
func canManage(u User) bool {
	return hasAnyPermission(u, "tasks") || hasAnyPermission(u, "knowledge") || hasAnyPermission(u, "discussions")
}
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
			} else if r.Method == http.MethodPatch && r.URL.Path == "/api/profile" {
				limit = 6 << 20
			} else if r.Method == http.MethodPatch && r.URL.Path == "/api/profile/avatar" {
				limit = 4 << 20
			} else if r.URL.Path == "/api/profile" || r.URL.Path == "/api/settings/logo" {
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
	m.HandleFunc("POST /api/auth/password-reset/request", a.wrapWithMutationLock(false, false, a.requestPasswordReset))
	m.HandleFunc("POST /api/auth/password-reset/confirm", a.wrap(false, a.confirmPasswordReset))
	m.HandleFunc("POST /api/auth/verification-status", a.wrapWithMutationLock(false, false, a.verificationSendStatus))
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
	m.HandleFunc("PATCH /api/members/{id}", a.wrap(true, a.updateMember))
	m.HandleFunc("PUT /api/members/{id}/password", a.wrap(true, a.setMemberPassword))
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
	a.registerQuestionBankRoutes(m)
	a.registerCertificateRoutes(m)
	a.registerAdminSettingsRoutes(m)
	a.registerFinanceRoutes(m)
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
		if err != nil && a.config.UploadDir != "" && retainedAssetName(r.URL.Path) != "" {
			// A tab from the previous release may still import its content-hashed
			// chunks. Keep those immutable assets in the existing data volume.
			if retained, openErr := os.OpenRoot(filepath.Join(a.config.UploadDir, "..", "web-assets")); openErr == nil {
				defer retained.Close()
				f, err = retained.Open(retainedAssetName(r.URL.Path))
			}
		}
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
		} else if retainedAssetName(r.URL.Path) != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
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
	u, err := readOne[User](a.store, `SELECT u.payload FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash(c.Value), now())
	if err != nil {
		return User{}, err
	}
	if u.DeactivatedAt != "" {
		return User{}, sql.ErrNoRows
	}
	return u, nil
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
	// Read current status rather than trusting a caller's potentially stale User.
	current, err := s.user(u.ID)
	if err != nil {
		return nil, err
	}
	if current.DeactivatedAt != "" {
		return nil, fail(403, "该账号已注销，请联系管理员恢复")
	}
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
func (a *App) rateKey(r *http.Request, email string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// Only explicitly configured proxy peers may supply a client address.
	// The ingress must overwrite X-Real-IP rather than forwarding user input.
	peer := net.ParseIP(host)
	for _, trusted := range strings.Split(a.config.TrustedProxyIPs, ",") {
		ip := net.ParseIP(strings.TrimSpace(trusted))
		if peer != nil && ip != nil && peer.Equal(ip) {
			if client := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); client != nil {
				host = client.String()
			}
			break
		}
	}
	return host + "|" + email
}
func (a *App) clearRate(r *http.Request, email string) {
	a.rateMu.Lock()
	delete(a.attempts, a.rateKey(r, email))
	a.rateMu.Unlock()
}
func (a *App) rateAllowed(r *http.Request, email string) bool {
	host := a.rateKey(r, email)
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
	if !valid || len(in.Password) < 1 {
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
	match := comparePassword(hash, in.Password)
	if err != nil || match != nil {
		return fail(401, "邮箱或密码不正确")
	}
	var u User
	if err = json.Unmarshal([]byte(raw), &u); err != nil {
		return err
	}
	normalizeUser(&u)
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
	respond(w, 200, map[string]any{"organization": s.Organization, "season": s.Season, "demoMode": a.config.Demo, "applicationOpen": s.ApplicationOpen, "applicationDeadline": s.ApplicationDeadline, "emailVerificationRequired": true, "registrationEnabled": a.currentMailService().verification != nil, "brandName": s.BrandName, "logoUrl": s.LogoURL, "teamName": s.TeamName, "secondaryTeamName": s.SecondaryTeamName, "competitions": s.Competitions, "description": s.Description})
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
	items, err := readAll[Applicant](a.store, "SELECT payload FROM applicants")
	if err != nil {
		return nil, err
	}
	out := []Applicant{}
	for _, v := range items {
		manage := hasPermission(u, v.Direction, "recruitment")
		if v.UserID != u.ID && !manage {
			continue
		}
		if !manage {
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
	s, err := a.store.settings()
	if err != nil {
		return err
	}
	if !s.ApplicationOpen || expired(s.ApplicationDeadline) {
		return fail(409, "本轮报名已关闭")
	}
	profile, err := a.store.profile(u)
	if err != nil {
		return err
	}
	if !profile.Complete {
		return fail(400, "请先补全个人资料和照片")
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
	if !hasPermission(u, in.Direction, "recruitment") {
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
	if item.UserID == u.ID && u.Role != "admin" {
		return fail(403, "组管理员不能审核自己的报名")
	}
	if !hasPermission(u, item.Direction, "recruitment") {
		return fail(403, "没有评价该候选人的权限")
	}
	var in struct {
		Status         *string  `json:"status"`
		InterviewAt    *string  `json:"interviewAt"`
		InterviewerID  *string  `json:"interviewerId"`
		Score          *float64 `json:"score"`
		Notes          *string  `json:"notes"`
		ReopenTraining bool     `json:"reopenTraining"`
	}
	if err = decode(r, &in); err != nil {
		return err
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
			if e != nil || !hasPermission(interviewer, item.Direction, "recruitment") {
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
	} else if owner.MemberType == "freshman" {
		owner.Team = ""
		owner.Direction = ""
	}
	tx, err := a.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if item.Status == "accepted" {
		if err = admitTraining(s, u, item.UserID, "", item.Direction, in.ReopenTraining); err != nil {
			return err
		}
	}
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
		if eliminated, err := trainingEliminated(a.store, u); err != nil {
			return nil, err
		} else if eliminated {
			continue
		}
		if isStaff(u) || u.MemberType != "freshman" || admitted[u.ID] {
			items = append(items, u)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].JoinedAt < items[j].JoinedAt })
	return items, nil
}
func (a *App) members(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "members") {
		return fail(403, "没有成员管理权限")
	}
	items, err := a.memberList()
	if u.Role == "admin" {
		items, err = readAll[User](a.store, "SELECT payload FROM users")
	}
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
		Teams      []memberTeam `json:"teams"`
		Directions []string     `json:"directions"`
	}
	teamMap := map[string]Team{}
	for _, team := range teams {
		teamMap[team.ID] = team
	}
	result := []memberEntry{}
	for _, member := range items {
		allowed, e := a.recipientAllowed(u, member.ID, "members")
		if e != nil {
			return e
		}
		if !allowed {
			continue
		}
		directions, err := a.memberDirections(member.ID)
		if err != nil {
			return err
		}
		visibleDirections := []string{}
		for _, direction := range directions {
			if hasPermission(u, direction, "members") {
				visibleDirections = append(visibleDirections, direction)
			}
		}
		entry := memberEntry{User: member, Teams: []memberTeam{}, Directions: visibleDirections}
		for _, membership := range memberships {
			if membership.UserID == member.ID && (u.Role == "admin" || hasPermission(u, membership.Direction, "members")) {
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
		Description         *string   `json:"description"`
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
	if in.Description != nil {
		s.Description = strings.TrimSpace(*in.Description)
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
	if s.Organization == "" || len(s.Organization) > 150 || s.Season == "" || len(s.Season) > 80 || !validDate(s.ApplicationDeadline) || !validURL(s.HydroURL) || len(s.Announcement) > 10000 || len(s.Description) > 1500 {
		return fail(400, "设置内容不合法，请检查名称、日期和链接")
	}
	if err = a.store.saveSettings(s); err != nil {
		return err
	}
	a.activity(u, "更新系统设置", s.Organization)
	respond(w, 200, map[string]any{"settings": s})
	return nil
}
func (a *App) tasksFor(u User) ([]Task, error) {
	allowed, err := a.learningAllowed(u)
	if err != nil {
		return nil, err
	}
	if !allowed && !hasAnyPermission(u, "tasks") {
		return []Task{}, nil
	}
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
			if !run.Preview && run.TaskID == t.ID && run.Status == "completed" {
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
			if t.MechanicalProblem != nil && t.MechanicalProblem.Type != "drawing" && (s.Status == "approved" || s.Status == "changes_requested") {
				t.AttemptCount++
				if s.Status == "approved" {
					t.AcceptedCount++
					if s.UserID == u.ID {
						t.MyAccepted = true
					}
				}
			}
			if !seen[s.UserID] {
				if hasPermission(u, t.Direction, "tasks") {
					t.SubmissionCount++
				}
				seen[s.UserID] = true
			}
			if s.UserID == u.ID && t.MySubmission == nil {
				copy := s
				t.MySubmission = &copy
			}
		}
		t.Assigned = a.requireTaskEnrollment(t, u) == nil
		t.CanSubmit = a.requireTaskSubmission(t, u) == nil
		if t.MechanicalProblem != nil && t.MechanicalProblem.Type == "drawing" {
			t.CanSubmit = false
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
		if visibility := query.Get("bankVisibility"); visibility != "" && t.BankVisibility != visibility {
			continue
		}
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
	if !hasAnyPermission(u, "tasks") {
		return fail(403, "没有任务管理权限")
	}
	var t Task
	if err := decode(r, &t); err != nil {
		return err
	}
	if err := a.validateTaskContent(&t, u); err != nil {
		return err
	}
	t.ID = randomID()
	t.CreatedBy = u.ID
	t.CreatedAt = now()
	t.MySubmission = nil
	t.SubmissionCount, t.AcceptedCount, t.AttemptCount = 0, 0, 0
	t.Locked, t.MyAccepted, t.Assigned, t.CanSubmit = false, false, false, false
	if t.Status == "draft" {
		t.PublishedAt = ""
	}
	if err := a.store.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", t.ID, encoded(t)); err != nil {
		return err
	}
	if t.Status == "draft" {
		a.activity(u, "创建培养任务草稿", t.Title)
	} else {
		if t.Status == "draft" {
			a.activity(u, "保存培养作业草稿", t.Title)
		} else {
			a.activity(u, "发布培养任务", t.Title)
		}
	}
	respond(w, 201, map[string]any{"item": t})
	return nil
}
func (a *App) listSubmissions(w http.ResponseWriter, r *http.Request, u User) error {
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	visible, err := a.taskVisibleTo(task, u)
	if err != nil {
		return err
	}
	if !visible {
		return fail(403, "没有权限查看此任务的提交记录")
	}
	items, err := a.store.submissions(r.PathValue("id"))
	if err != nil {
		return err
	}
	if !hasPermission(u, task.Direction, "tasks") {
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

// Serialize a student's replacement and a teacher's confirmation on the task row.
// Taking a database write lock (also on SQLite) before reading submissions avoids
// stale reviews and duplicate versions across independent server processes.
func (a *App) beginSubmissionWrite(r *http.Request, taskID string) (*App, *sql.Tx, error) {
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, nil, err
	}
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx, fileDir: a.store.fileDir}
	if err = s.exec("UPDATE tasks SET payload=payload WHERE id=?", taskID); err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	return &App{store: s, config: a.config}, tx, nil
}

func (a *App) submitTask(w http.ResponseWriter, r *http.Request, u User) error {
	scoped, tx, err := a.beginSubmissionWrite(r, r.PathValue("id"))
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a = scoped
	t, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	normalizeTask(&t)
	if len(t.Questions) > 0 {
		return fail(400, "多题作业请先逐题保存答案，再提交整份作业")
	}
	if t.MechanicalProblem != nil {
		return fail(400, "选择或填空题请直接作答；图纸题供自由练习，无需提交")
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
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"item": s})
	return nil
}

func normalizeSubmissionAnswers(t Task, input []SubmissionAnswer, requireComplete bool) ([]SubmissionAnswer, []string, error) {
	if len(input) != len(t.Questions) {
		return nil, nil, fail(400, "请为每道题提供一份答案记录")
	}
	byID := make(map[string]SubmissionAnswer, len(input))
	for _, answer := range input {
		if answer.QuestionID == "" || byID[answer.QuestionID].QuestionID != "" {
			return nil, nil, fail(400, "题目答案编号缺失或重复")
		}
		if answer.AutoGraded || answer.Score != nil || answer.Status != "" || strings.TrimSpace(answer.Feedback) != "" {
			return nil, nil, fail(400, "提交答案不能包含评分信息")
		}
		byID[answer.QuestionID] = answer
	}
	answers := make([]SubmissionAnswer, 0, len(t.Questions))
	allFiles := []string{}
	seenFiles := map[string]bool{}
	for _, question := range t.Questions {
		answer, ok := byID[question.ID]
		if !ok {
			return nil, nil, fail(400, "请为每道题提供一份答案记录")
		}
		if len(answer.FileIDs) > 20 {
			return nil, nil, fail(400, "每道题最多上传 20 个答案文件")
		}
		answer.Response = strings.TrimSpace(answer.Response)
		if len(answer.Response) > 20000 {
			return nil, nil, fail(400, "单题文字答案不能超过 20000 字节")
		}
		switch question.Type {
		case "single_choice":
			if answer.Response != "" {
				index, err := strconv.Atoi(answer.Response)
				if err != nil || index < 0 || index >= len(question.Options) || strconv.Itoa(index) != answer.Response {
					return nil, nil, fail(400, "单选题答案编号无效")
				}
			}
			if requireComplete && answer.Response == "" {
				return nil, nil, fail(400, "请完成每一道选择题")
			}
		case "multiple_choice":
			choices := []string{}
			if answer.Response != "" {
				if err := json.Unmarshal([]byte(answer.Response), &choices); err != nil {
					return nil, nil, fail(400, "多选题答案格式无效")
				}
			}
			seenChoices := map[string]bool{}
			for _, choice := range choices {
				index, err := strconv.Atoi(choice)
				if err != nil || index < 0 || index >= len(question.Options) || strconv.Itoa(index) != choice || seenChoices[choice] {
					return nil, nil, fail(400, "多选题答案编号无效或重复")
				}
				seenChoices[choice] = true
			}
			if requireComplete && len(choices) == 0 {
				return nil, nil, fail(400, "请完成每一道选择题")
			}
			sort.Slice(choices, func(i, j int) bool { return choices[i] < choices[j] })
			answer.Response = encoded(choices)
		case "fill_blank":
			if requireComplete && answer.Response == "" {
				return nil, nil, fail(400, "请完成每一道填空题")
			}
		case "code":
			if answer.Response != "" || len(answer.FileIDs) > 0 {
				return nil, nil, fail(400, "代码题答案只能使用在线判题记录提交")
			}
			if requireComplete && answer.JudgeRunID == "" {
				return nil, nil, fail(400, "请先运行并提交每一道代码题")
			}
		case "short_answer":
			if requireComplete && answer.Response == "" && len(answer.FileIDs) == 0 {
				return nil, nil, fail(400, "请填写简答题答案或上传成果文件")
			}
		case "drawing":
			if requireComplete && len(answer.FileIDs) == 0 {
				return nil, nil, fail(400, "请为每道绘图题上传成果文件")
			}
		default:
			return nil, nil, fail(400, "作业题型无效，请联系管理员")
		}
		if question.Type != "code" && answer.JudgeRunID != "" {
			return nil, nil, fail(400, "只有代码题可以关联在线判题记录")
		}
		normalized := normalizedIDs(answer.FileIDs)
		if len(normalized) != len(answer.FileIDs) {
			return nil, nil, fail(400, "答案文件不能重复")
		}
		for _, fileID := range normalized {
			if seenFiles[fileID] {
				return nil, nil, fail(400, "同一答案文件不能重复关联到多道题")
			}
			seenFiles[fileID] = true
			allFiles = append(allFiles, fileID)
		}
		answers = append(answers, SubmissionAnswer{QuestionID: question.ID, FileIDs: normalized, Response: answer.Response, JudgeRunID: answer.JudgeRunID})
	}
	return answers, allFiles, nil
}

func latestSubmissionForUser(items []Submission, userID string) *Submission {
	for i := range items {
		if items[i].UserID == userID {
			return &items[i]
		}
	}
	return nil
}

func (a *App) saveTaskSubmissionDraft(w http.ResponseWriter, r *http.Request, u User) error {
	scoped, tx, err := a.beginSubmissionWrite(r, r.PathValue("id"))
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a = scoped
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	normalizeTask(&task)
	if task.Category != "assignment" || len(task.Questions) == 0 {
		return fail(400, "此接口仅用于多题作业")
	}
	if err = a.requireTaskSubmission(task, u); err != nil {
		return err
	}
	var in struct {
		Answers []SubmissionAnswer `json:"answers"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	answers, fileIDs, err := normalizeSubmissionAnswers(task, in.Answers, false)
	if err != nil {
		return err
	}
	for _, answer := range answers {
		if err = requireOwnedFiles(a.store, u, answer.FileIDs); err != nil {
			return err
		}
		if answer.JudgeRunID != "" {
			question := taskQuestionByID(task, answer.QuestionID)
			if question == nil {
				return fail(400, "代码题目不存在")
			}
			if _, err = a.validateQuestionJudgeRun(task, *question, u, answer.JudgeRunID, false); err != nil {
				return err
			}
		}
	}
	items, err := a.store.submissions(task.ID)
	if err != nil {
		return err
	}
	latest := latestSubmissionForUser(items, u.ID)
	if latest != nil && latest.Status == "approved" {
		return fail(409, "作业已批改确认，请联系管理员打回重做后再提交")
	}
	submission := Submission{Answers: answers, FileIDs: fileIDs, ID: randomID(), TaskID: task.ID, UserID: u.ID, UserName: u.Name, Status: "draft", CreatedAt: now()}
	if latest != nil {
		submission.RedoAllowed = latest.Status == "changes_requested" || (latest.Status == "draft" && latest.RedoAllowed)
		if latest.Status == "draft" && latest.FinalizedAt == "" {
			submission.ID = latest.ID
			submission.Version = latest.Version
			if err = a.store.exec("UPDATE submissions SET payload=? WHERE id=?", encoded(submission), latest.ID); err != nil {
				return err
			}
		} else if latest.Status == "changes_requested" || latest.Status == "pending" {
			submission.Version = latest.Version + 1
			if err = a.store.exec("INSERT INTO submissions(id,task_id,user_id,version,payload) VALUES(?,?,?,?,?)", submission.ID, submission.TaskID, submission.UserID, submission.Version, encoded(submission)); err != nil {
				return err
			}
		} else {
			return fail(409, "当前作业状态不允许暂存新答案")
		}
	} else {
		submission.Version = 1
		if err = a.store.exec("INSERT INTO submissions(id,task_id,user_id,version,payload) VALUES(?,?,?,?,?)", submission.ID, submission.TaskID, submission.UserID, submission.Version, encoded(submission)); err != nil {
			return err
		}
	}
	a.activity(u, "保存作业答案草稿", task.Title)
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": submission})
	return nil
}

func (a *App) finalizeTaskSubmission(w http.ResponseWriter, r *http.Request, u User) error {
	scoped, tx, err := a.beginSubmissionWrite(r, r.PathValue("id"))
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a = scoped
	task, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	normalizeTask(&task)
	if task.Category != "assignment" || len(task.Questions) == 0 {
		return fail(400, "此接口仅用于多题作业")
	}
	if err = a.requireTaskSubmission(task, u); err != nil {
		return err
	}
	items, err := a.store.submissions(task.ID)
	if err != nil {
		return err
	}
	latest := latestSubmissionForUser(items, u.ID)
	if latest == nil || latest.Status != "draft" || latest.FinalizedAt != "" {
		return fail(409, "请先保存作业答案草稿后再提交")
	}
	answers, fileIDs, err := normalizeSubmissionAnswers(task, latest.Answers, true)
	if err != nil {
		return err
	}
	for _, answer := range answers {
		if err = requireOwnedFiles(a.store, u, answer.FileIDs); err != nil {
			return err
		}
	}
	verifiedRuns := map[string]JudgeRun{}
	for i := range answers {
		question := taskQuestionByID(task, answers[i].QuestionID)
		if question == nil {
			return fail(400, "作业题目不存在")
		}
		if question.Type == "code" {
			run, verifyErr := a.validateQuestionJudgeRun(task, *question, u, answers[i].JudgeRunID, true)
			if verifyErr != nil {
				return verifyErr
			}
			verifiedRuns[question.ID] = run
		}
	}
	latest.Answers = answers
	latest.FileIDs = fileIDs
	latest.Status = "pending"
	latest.RedoAllowed = false
	latest.FinalizedAt = now()
	autoGradeTaskSubmission(task, latest, verifiedRuns)
	if err = a.store.exec("UPDATE submissions SET payload=? WHERE id=?", encoded(*latest), latest.ID); err != nil {
		return err
	}
	a.activity(u, "提交作业", task.Title)
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": latest})
	return nil
}

func (a *App) reviewSubmission(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "tasks") {
		return fail(403, "没有任务评阅权限")
	}
	s, err := readOne[Submission](a.store, "SELECT payload FROM submissions WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	scoped, tx, err := a.beginSubmissionWrite(r, s.TaskID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a = scoped
	s, err = readOne[Submission](a.store, "SELECT payload FROM submissions WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	t, err := a.store.task(s.TaskID)
	if err != nil {
		return err
	}
	if err = requirePermission(u, t.Direction, "tasks"); err != nil {
		return err
	}
	if t.MechanicalProblem != nil {
		return fail(400, "客观题由系统自动判定，不支持人工修改成绩")
	}
	if u.Role != "admin" && s.UserID == u.ID {
		return fail(403, "不能评阅本人提交的成果")
	}
	var latest int
	if err = a.store.row("SELECT MAX(version) FROM submissions WHERE task_id=? AND user_id=?", s.TaskID, s.UserID).Scan(&latest); err != nil {
		return err
	}
	if latest != s.Version {
		return fail(409, "已有新版本，请审核最新提交")
	}
	normalizeTask(&t)
	var in submissionReviewInput
	if err = decode(r, &in); err != nil {
		return err
	}
	if s.Status == "draft" || (len(t.Questions) > 0 && s.FinalizedAt == "") {
		return fail(409, "作业尚未正式提交，不能审核")
	}
	if in.ReturnForRedo {
		if t.Category != "assignment" {
			return fail(400, "只有作业可以打回重做")
		}
		if in.Status != "" || in.Score != nil || len(in.QuestionReviews) > 0 {
			return fail(400, "打回重做不能同时提交评分")
		}
		in.Feedback = strings.TrimSpace(in.Feedback)
		if in.Feedback == "" || len(in.Feedback) > 20000 {
			return fail(400, "请填写打回重做原因（不超过 20000 字节）")
		}
		s.Status, s.Feedback = "changes_requested", in.Feedback
	} else if len(t.Questions) > 0 {
		if in.Status != "" || in.Score != nil || in.Feedback != "" {
			return fail(400, "多题作业请逐题评分")
		}
		s, err = reviewedQuestionSubmission(t, s, in)
		if err != nil {
			return err
		}
	} else {
		if len(in.QuestionReviews) > 0 || (in.Status != "approved" && in.Status != "changes_requested") || in.Score == nil || !finite(*in.Score) || *in.Score < 0 || *in.Score > t.MaxScore || len(in.Feedback) > 20000 {
			return fail(400, "审核状态或分值不合法")
		}
		if in.Status == "changes_requested" && strings.TrimSpace(in.Feedback) == "" {
			return fail(400, "请说明需要修改的内容")
		}
		s.Status, s.Score, s.Feedback = in.Status, in.Score, in.Feedback
	}
	s.ReviewedBy = u.ID
	s.ReviewedAt = now()
	if err = a.store.exec("UPDATE submissions SET payload=? WHERE id=?", encoded(s), s.ID); err != nil {
		return err
	}
	action := "审核任务成果"
	if in.ReturnForRedo {
		action = "打回作业重做"
	}
	a.activity(u, action, t.Title+" · "+s.UserName)
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": s})
	return nil
}

// Drafts never call this function. Only a finalized complete submission is graded.
func taskQuestionByID(task Task, questionID string) *TaskQuestion {
	for i := range task.Questions {
		if task.Questions[i].ID == questionID {
			return &task.Questions[i]
		}
	}
	return nil
}

func autoGradeTaskSubmission(task Task, submission *Submission, verifiedRuns ...map[string]JudgeRun) {
	questions := map[string]TaskQuestion{}
	for _, q := range task.Questions {
		questions[q.ID] = q
	}
	runs := map[string]JudgeRun{}
	if len(verifiedRuns) > 0 && verifiedRuns[0] != nil {
		runs = verifiedRuns[0]
	}
	graded := false
	for i := range submission.Answers {
		answer := &submission.Answers[i]
		q, ok := questions[answer.QuestionID]
		if !ok {
			continue
		}
		if q.Type == "code" {
			run, exists := runs[q.ID]
			if !exists {
				continue
			}
			score := run.Score
			answer.Score = &score
			answer.Status = "approved"
			answer.AutoGraded = true
			answer.Feedback = fmt.Sprintf("在线判题：通过 %d / %d 个测试用例", run.Passed, run.Total)
			graded = true
			continue
		}
		if !q.AutoGrade || len(q.Answers) == 0 {
			continue
		}
		correct := false
		switch q.Type {
		case "single_choice", "fill_blank":
			correct = includesID(q.Answers, strings.TrimSpace(answer.Response))
		case "multiple_choice":
			var chosen []string
			if json.Unmarshal([]byte(answer.Response), &chosen) == nil && len(chosen) == len(q.Answers) {
				correct = true
				seen := map[string]bool{}
				for _, choice := range chosen {
					if seen[choice] || !includesID(q.Answers, choice) {
						correct = false
					}
					seen[choice] = true
				}
			}
		default:
			continue
		}
		score := 0.0
		answer.Feedback = "自动判定：答案不正确"
		if correct {
			score = q.MaxScore
			answer.Feedback = "自动判定：答案正确"
		}
		answer.Score, answer.Status, answer.AutoGraded = &score, "approved", true
		graded = true
	}
	if graded {
		aggregateTaskSubmission(task, submission)
		if submission.Status == "approved" {
			submission.ReviewedAt = now()
		}
	}
}

// Automatic and manual review share the same total/state computation.
func aggregateTaskSubmission(task Task, submission *Submission) {
	answers := map[string]SubmissionAnswer{}
	for _, answer := range submission.Answers {
		answers[answer.QuestionID] = answer
	}
	total := 0.0
	allApproved := len(submission.Answers) == len(task.Questions)
	hasChangesRequested := false
	feedback := []string{}
	for _, question := range task.Questions {
		answer, ok := answers[question.ID]
		if !ok {
			allApproved = false
			continue
		}
		if answer.Score != nil {
			total += *answer.Score
			if answer.Feedback != "" {
				feedback = append(feedback, question.Title+"："+answer.Feedback)
			}
		}
		if answer.Status == "changes_requested" {
			hasChangesRequested = true
		}
		if answer.Status != "approved" || answer.Score == nil {
			allApproved = false
		}
	}
	submission.Score = &total
	submission.Feedback = strings.Join(feedback, "\n")
	switch {
	case hasChangesRequested:
		submission.Status = "changes_requested"
	case allApproved:
		submission.Status = "approved"
	default:
		submission.Status = "pending"
	}
}

type submissionReviewInput struct {
	ReturnForRedo   bool     `json:"returnForRedo"`
	Status          string   `json:"status"`
	Score           *float64 `json:"score"`
	Feedback        string   `json:"feedback"`
	QuestionReviews []struct {
		QuestionID string  `json:"questionId"`
		Score      float64 `json:"score"`
		Feedback   string  `json:"feedback"`
		Status     string  `json:"status"`
	} `json:"questionReviews"`
}

func reviewedQuestionSubmission(task Task, submission Submission, in submissionReviewInput) (Submission, error) {
	if len(in.QuestionReviews) == 0 || len(in.QuestionReviews) > len(task.Questions) {
		return Submission{}, fail(400, "请至少提交一道题的评分")
	}
	questions := map[string]TaskQuestion{}
	for _, question := range task.Questions {
		questions[question.ID] = question
	}
	answers := map[string]int{}
	for i := range submission.Answers {
		answers[submission.Answers[i].QuestionID] = i
	}
	seen := map[string]bool{}
	for _, review := range in.QuestionReviews {
		question, ok := questions[review.QuestionID]
		answerIndex, answerExists := answers[review.QuestionID]
		if !ok || !answerExists || seen[review.QuestionID] {
			return Submission{}, fail(400, "评分题目不存在或重复")
		}
		if (review.Status != "approved" && review.Status != "changes_requested") || !finite(review.Score) || review.Score < 0 || review.Score > question.MaxScore || len(review.Feedback) > 20000 {
			return Submission{}, fail(400, "题目审核状态或分数无效")
		}
		if review.Status == "changes_requested" && strings.TrimSpace(review.Feedback) == "" {
			return Submission{}, fail(400, "请说明该题需要修改的内容")
		}
		answer := &submission.Answers[answerIndex]
		score := review.Score
		answer.Score = &score
		answer.Feedback = strings.TrimSpace(review.Feedback)
		answer.Status = review.Status
		answer.AutoGraded = false
		seen[review.QuestionID] = true
	}

	aggregateTaskSubmission(task, &submission)
	return submission, nil
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
	visibleTasks := map[string]Task{}
	for _, task := range tasks {
		visibleTasks[task.ID] = task
	}
	pending := 0
	seen := map[string]bool{}
	for _, s := range subs {
		key := s.TaskID + "/" + s.UserID
		if seen[key] {
			continue
		}
		seen[key] = true
		if task, ok := visibleTasks[s.TaskID]; ok && (hasPermission(u, task.Direction, "tasks") || s.UserID == u.ID) && s.Status == "pending" {
			pending++
		}
	}
	active := 0
	for _, t := range tasks {
		if !expired(t.DueAt) && (hasPermission(u, t.Direction, "tasks") || t.Assigned) {
			active++
		}
	}
	memberCount := 0
	for _, m := range members {
		allowed, e := a.recipientAllowed(u, m.ID, "members")
		if e != nil {
			return e
		}
		if allowed || m.ID == u.ID {
			memberCount++
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
	if u.Role != "admin" {
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
	if config.CertificateRenderer == nil {
		config.CertificateRenderer = defaultCertificateRenderer
	}
	return &App{store: store, config: config, attempts: map[string]attempt{}, judgeSlots: make(chan struct{}, 2), resetSlots: make(chan struct{}, 8)}
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
	var mailer VerificationMailer
	var emailConfig SMTPConfig
	var storedMailCount int
	if err = store.row("SELECT COUNT(*) FROM settings WHERE id='mail-service'").Scan(&storedMailCount); err != nil {
		log.Fatal("cannot read mail configuration source")
	}
	if storedMailCount == 0 {
		mailer, emailConfig, err = smtpMailerFromEnv(demo)
		if err != nil {
			log.Fatal(err)
		}
	}
	var noticeMailer NoticeMailer
	if value, ok := mailer.(NoticeMailer); ok {
		noticeMailer = value
	}
	app := newApp(store, Config{TrustedProxyIPs: os.Getenv("TRUSTED_PROXY_IPS"), Mailer: mailer, NoticeMailer: noticeMailer, EmailFrom: emailConfig.From, EmailSecurity: emailConfig.Security, Demo: demo, Secure: os.Getenv("SESSION_SECURE") == "true", PublicOrigin: os.Getenv("PUBLIC_ORIGIN"), WebDir: env("WEB_DIR", "../frontend/dist"), UploadDir: env("UPLOAD_DIR", filepath.Join(env("DATA_DIR", "./data"), "uploads")), JudgeURL: os.Getenv("JUDGE_URL"), JudgeToken: os.Getenv("JUDGE_TOKEN")})
	if err = app.loadStoredMailService(); err != nil {
		log.Fatal(err)
	}
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
		return fail(403, "只有管理员可创建成员账号")
	}
	var in struct {
		Name        string       `json:"name"`
		Email       string       `json:"email"`
		Password    string       `json:"password"`
		Role        string       `json:"role"`
		MemberType  string       `json:"memberType"`
		GroupGrants []GroupGrant `json:"groupGrants"`
		Team        string       `json:"team"`
		Direction   string       `json:"direction"`
		Directions  []string     `json:"directions"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	email, valid := normalizedEmail(in.Email)
	in.Name = strings.TrimSpace(in.Name)
	if !valid || in.Name == "" || len(in.Name) > 100 || !validNewPassword(in.Password) || in.Team != "" && !validTeam(in.Team) {
		return fail(400, "请填写有效的姓名、邮箱与至少 6 个字符的密码")
	}
	if in.Role == "" {
		in.Role = "freshman"
	}
	if !slices.Contains([]string{"admin", "group_admin", "member", "reserve", "freshman"}, in.Role) {
		return fail(400, "无效的账号角色")
	}
	member := User{Role: in.Role, MemberType: in.MemberType, GroupGrants: in.GroupGrants, Direction: in.Direction}
	if in.Directions != nil {
		directions, err := canonicalMemberDirections(in.Directions)
		if err != nil {
			return err
		}
		in.Directions = directions
		member.Direction = ""
		if len(directions) > 0 {
			member.Direction = directions[0]
		}
	}
	if member.MemberType == "" {
		switch member.Role {
		case "admin", "group_admin", "member":
			member.MemberType = "formal"
		case "reserve":
			member.MemberType = "reserve"
		default:
			member.MemberType = "freshman"
		}
	}
	if err := validateUserAccess(&member); err != nil {
		return err
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	var count int
	if err := s.row("SELECT COUNT(*) FROM users WHERE email=?", email).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fail(409, "该邮箱已注册")
	}
	created, err := createUser(s, in.Name, email, in.Password, member.Role, in.Team, member.Direction)
	if err != nil {
		return err
	}
	created.MemberType = member.MemberType
	created.GroupGrants = member.GroupGrants
	if in.Directions != nil {
		if err = syncMemberDirections(s, u, created, in.Directions); err != nil {
			return err
		}
	}
	if err = s.exec("UPDATE users SET payload=? WHERE id=?", encoded(created), created.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "创建成员账号", created.Name)
	respond(w, 201, map[string]any{"item": created})
	return nil
}
