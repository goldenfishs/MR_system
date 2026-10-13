package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const verificationLifetime = 10 * time.Minute
const verificationCooldown = time.Minute

// VerificationMailer is also injectable in tests. Production only wires SMTP;
// neither demo mode nor an HTTP response exposes a verification code.
type VerificationMailer interface {
	SendVerification(context.Context, string, string) error
}

func rateLimit(message string, seconds int) error {
	return &apiError{status: http.StatusTooManyRequests, message: message, retryAfterSeconds: max(1, seconds)}
}

func registrationUnavailable() error {
	return fail(http.StatusServiceUnavailable, "邮箱验证服务尚未配置，暂时无法注册，请联系管理员")
}

func newVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func validVerificationCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (a *App) sendVerificationCode(w http.ResponseWriter, r *http.Request, _ User) error {
	var in struct {
		Email string `json:"email"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	email, valid := normalizedEmail(in.Email)
	if !valid {
		return fail(400, "请填写有效邮箱")
	}
	if a.config.Mailer == nil {
		return registrationUnavailable()
	}
	code, hash, err := a.prepareVerificationSend(r.Context(), email, tokenHash(rateKey(r, "")))
	if err != nil {
		return err
	}
	if code != "" {
		// SMTP runs outside the mutation lock. A slow mail server must not block
		// login, account management, or any other write request.
		if err = a.config.Mailer.SendVerification(r.Context(), email, code); err != nil {
			// Do not log SMTP errors: a server may echo message content or credentials.
			return fail(http.StatusServiceUnavailable, "验证码邮件发送失败，请稍后重试或联系管理员")
		}
		if err = a.activateVerification(email, hash); err != nil {
			return err
		}
	}
	respond(w, 200, map[string]any{"message": "若该邮箱可用于注册，验证码将发送至你的邮箱，请注意查收", "cooldownSeconds": int(verificationCooldown.Seconds()), "expiresInSeconds": int(verificationLifetime.Seconds())})
	return nil
}

// prepareVerificationSend protects only the database reservation and hash write.
// The challenge stays inactive while SMTP is in flight.
func (a *App) prepareVerificationSend(ctx context.Context, email, ipHash string) (string, string, error) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	timestamp := time.Now().Unix()
	if err := a.reserveVerificationSend(ctx, email, ipHash, timestamp); err != nil {
		return "", "", err
	}
	var count int
	if err := a.store.row("SELECT COUNT(*) FROM users WHERE email=?", email).Scan(&count); err != nil {
		return "", "", err
	}
	// Match the successful response for registered addresses without mailing a
	// registration code or disclosing whether an account exists.
	if count > 0 {
		return "", "", nil
	}
	code, err := newVerificationCode()
	if err != nil {
		return "", "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	if err = a.store.exec("UPDATE email_verifications SET code_hash=? WHERE email=?", string(hash), email); err != nil {
		return "", "", err
	}
	return code, string(hash), nil
}

func (a *App) activateVerification(email, hash string) error {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	result, err := a.store.db.Exec(a.store.q("UPDATE email_verifications SET delivered=1 WHERE email=? AND code_hash=? AND delivered=0 AND expires_at>?"), email, hash, time.Now().Unix())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fail(409, "验证码已更新或过期，请使用最新邮件中的验证码")
	}
	return nil
}

func (a *App) reserveVerificationSend(ctx context.Context, email, ipHash string, timestamp int64) error {
	tx, err := a.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = s.exec("DELETE FROM verification_send_events WHERE created_at<=?", timestamp-3600); err != nil {
		return err
	}
	// Expired challenges retain no usable secret and can be discarded. Recent
	// failed sends still enforce their cooldown through last_sent_at.
	if err = s.exec("DELETE FROM email_verifications WHERE expires_at<=? AND last_sent_at<=?", timestamp, timestamp-60); err != nil {
		return err
	}
	var lastSent int64
	err = s.row("SELECT last_sent_at FROM email_verifications WHERE email=?", email).Scan(&lastSent)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && timestamp < lastSent+60 {
		return rateLimit("验证码发送过于频繁，请稍后重试", int(lastSent+60-timestamp))
	}
	limits := []struct {
		column  string
		value   string
		window  int64
		limit   int
		message string
	}{
		{"email", email, 3600, 5, "该邮箱验证码请求过多，请稍后重试"},
		{"ip_hash", ipHash, 900, 10, "当前网络验证码请求过多，请稍后重试"},
	}
	for _, limit := range limits {
		var count int
		var oldest sql.NullInt64
		// Only the fixed column names above are interpolated; values are bound.
		if err = s.row("SELECT COUNT(*), MIN(created_at) FROM verification_send_events WHERE "+limit.column+"=? AND created_at>?", limit.value, timestamp-limit.window).Scan(&count, &oldest); err != nil {
			return err
		}
		if count >= limit.limit {
			return rateLimit(limit.message, int(oldest.Int64+limit.window-timestamp))
		}
	}
	if err = s.exec("INSERT INTO verification_send_events(id,email,ip_hash,created_at) VALUES(?,?,?,?)", randomID(), email, ipHash, timestamp); err != nil {
		return err
	}
	// Reserving a new send invalidates the previous code before contacting SMTP.
	// If delivery or activation fails, delivered=0 prevents registration.
	if err = s.exec(`INSERT INTO email_verifications(email,code_hash,expires_at,attempts,last_sent_at,delivered) VALUES(?,?,?,0,?,0) ON CONFLICT(email) DO UPDATE SET code_hash=excluded.code_hash, expires_at=excluded.expires_at, attempts=0, last_sent_at=excluded.last_sent_at, delivered=0`, email, "", timestamp+600, timestamp); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) register(w http.ResponseWriter, r *http.Request, _ User) error {
	var in struct {
		Name             string        `json:"name"`
		Email            string        `json:"email"`
		Password         string        `json:"password"`
		VerificationCode string        `json:"verificationCode"`
		Profile          ProfileFields `json:"profile"`
		Photo            PhotoInput    `json:"photo"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	email, valid := normalizedEmail(in.Email)
	in.Name = strings.TrimSpace(in.Name)
	if !valid || in.Name == "" || len(in.Name) > 100 || len(in.Password) < 10 || len(in.Password) > 72 {
		return fail(400, "请填写有效姓名、邮箱和 10–72 字节密码")
	}
	if a.config.Mailer == nil {
		return registrationUnavailable()
	}
	if !validVerificationCode(in.VerificationCode) {
		return fail(400, "请填写 6 位邮箱验证码")
	}
	profile := profileFromFields(in.Name, in.Profile)
	if err := validateProfile(profile); err != nil {
		return err
	}
	if !a.rateAllowed(r, email) {
		return rateLimit("尝试次数过多，请 15 分钟后重试", 900)
	}
	photo, photoData, err := preparePhoto(in.Photo, "")
	if err != nil {
		return err
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	var hash string
	var expires int64
	var attempts, delivered int
	err = s.row("SELECT code_hash,expires_at,attempts,delivered FROM email_verifications WHERE email=?", email).Scan(&hash, &expires, &attempts, &delivered)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(400, "验证码无效或已过期，请重新获取")
	}
	if err != nil {
		return err
	}
	if delivered != 1 || time.Now().Unix() >= expires {
		return fail(400, "验证码无效或已过期，请重新获取")
	}
	if attempts >= 5 {
		return rateLimit("验证码错误次数过多，请重新获取验证码", int(expires-time.Now().Unix()))
	}
	if err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.VerificationCode)); err != nil {
		if err = s.exec("UPDATE email_verifications SET attempts=attempts+1 WHERE email=?", email); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if attempts+1 >= 5 {
			return rateLimit("验证码错误次数过多，请重新获取验证码", int(expires-time.Now().Unix()))
		}
		return fail(400, "验证码不正确，请检查后重试")
	}
	var count int
	if err = s.row("SELECT COUNT(*) FROM users WHERE email=?", email).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fail(409, "该邮箱已注册，请直接登录")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	verifiedAt := now()
	u := User{ID: randomID(), Name: in.Name, Email: email, Role: "student", JoinedAt: verifiedAt, EmailVerifiedAt: verifiedAt}
	if err = s.exec("INSERT INTO users(id,email,password_hash,payload) VALUES(?,?,?,?)", u.ID, email, string(passwordHash), encoded(u)); err != nil {
		return err
	}
	photo.OwnerID = u.ID
	photoPath, err := a.persistAsset(s, photo, photoData)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.Remove(photoPath)
		}
	}()
	profile.UserID = u.ID
	profile.PhotoFileID = photo.ID
	profile.UpdatedAt = verifiedAt
	profile.Complete = true
	if err = s.saveProfile(profile); err != nil {
		return err
	}
	if err = s.exec("DELETE FROM email_verifications WHERE email=?", email); err != nil {
		return err
	}
	cookie, err := a.createSession(s, r, u)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	http.SetCookie(w, cookie)
	a.clearRate(r, email)
	a.activity(u, "验证邮箱并注册账号", u.Name)
	respond(w, 201, map[string]any{"user": u})
	return nil
}

func (a *App) emailSettings(w http.ResponseWriter, _ *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可查看邮箱服务配置")
	}
	respond(w, 200, map[string]any{"configured": a.config.Mailer != nil, "from": a.config.EmailFrom, "security": a.config.EmailSecurity, "supportsBulk": a.config.NoticeMailer != nil})
	return nil
}
