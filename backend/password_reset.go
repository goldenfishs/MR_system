package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const passwordResetMessageText = "如果该邮箱已注册，重置验证码将发送到邮箱，请注意查收。"

func (a *App) requestPasswordReset(w http.ResponseWriter, r *http.Request, _ User) error {
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
	mailer, configured := a.currentMailService().verification.(PasswordResetMailer)
	if !configured {
		return fail(http.StatusServiceUnavailable, "邮箱服务暂未开放，无法发送密码重置邮件")
	}
	// Bound delivery work, and reserve capacity before looking up the email.
	select {
	case a.resetSlots <- struct{}{}:
	default:
		return rateLimit("邮件服务繁忙，请稍后重试", 60)
	}
	code, hash, registered, err := a.preparePasswordResetSend(r.Context(), email, tokenHash(a.rateKey(r, "")))
	if err != nil {
		<-a.resetSlots
		return err
	}
	// Never wait for SMTP on the HTTP response: different delivery latencies
	// must not reveal whether an account exists. Codes stay inactive until sent.
	go func() {
		defer func() { <-a.resetSlots }()
		if !registered {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// Do not log errors: SMTP may echo secrets or message contents.
		if mailer.SendPasswordReset(ctx, email, code) == nil {
			_ = a.activatePasswordReset(email, hash)
		}
	}()

	respond(w, http.StatusOK, map[string]any{"message": passwordResetMessageText, "cooldownSeconds": int(verificationCooldown.Seconds()), "expiresInSeconds": int(verificationLifetime.Seconds())})
	return nil
}

func (a *App) preparePasswordResetSend(ctx context.Context, email, ipHash string) (string, string, bool, error) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	timestamp := time.Now().Unix()
	tx, err := a.store.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", false, err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = s.exec("DELETE FROM password_reset_send_events WHERE created_at<=?", timestamp-3600); err != nil {
		return "", "", false, err
	}
	if err = s.exec("DELETE FROM password_reset_codes WHERE expires_at<=? AND last_sent_at<=?", timestamp, timestamp-int64(verificationCooldown.Seconds())); err != nil {
		return "", "", false, err
	}
	if err = passwordResetSendLimit(s, email, ipHash, timestamp); err != nil {
		return "", "", false, err
	}
	var accountCount int
	if err = s.row("SELECT COUNT(*) FROM users WHERE email=?", email).Scan(&accountCount); err != nil {
		return "", "", false, err
	}
	code, err := newVerificationCode()
	if err != nil {
		return "", "", false, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return "", "", false, err
	}
	if err = s.exec("INSERT INTO password_reset_send_events(id,email,ip_hash,created_at) VALUES(?,?,?,?)", randomID(), email, ipHash, timestamp); err != nil {
		return "", "", false, err
	}
	// Unknown addresses receive the same response and rate limits. Their
	// challenge remains inactive and cannot be used to change an account.
	delivered := 0
	if err = s.exec(`INSERT INTO password_reset_codes(email,code_hash,expires_at,attempts,last_sent_at,delivered) VALUES(?,?,?,0,?,?) ON CONFLICT(email) DO UPDATE SET code_hash=excluded.code_hash, expires_at=excluded.expires_at, attempts=0, last_sent_at=excluded.last_sent_at, delivered=excluded.delivered`, email, string(hash), timestamp+int64(verificationLifetime.Seconds()), timestamp, delivered); err != nil {
		return "", "", false, err
	}
	if err = tx.Commit(); err != nil {
		return "", "", false, err
	}
	return code, string(hash), accountCount > 0, nil
}

func passwordResetSendLimit(s *Store, email, ipHash string, timestamp int64) error {
	var lastSent int64
	err := s.row("SELECT last_sent_at FROM password_reset_codes WHERE email=?", email).Scan(&lastSent)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && timestamp < lastSent+int64(verificationCooldown.Seconds()) {
		return rateLimit("验证码发送过于频繁，请稍后重试", int(lastSent+int64(verificationCooldown.Seconds())-timestamp))
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
		if err = s.row("SELECT COUNT(*), MIN(created_at) FROM password_reset_send_events WHERE "+limit.column+"=? AND created_at>?", limit.value, timestamp-limit.window).Scan(&count, &oldest); err != nil {
			return err
		}
		if count >= limit.limit {
			return rateLimit(limit.message, int(oldest.Int64+limit.window-timestamp))
		}
	}
	return nil
}

func (a *App) activatePasswordReset(email, hash string) error {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	result, err := a.store.db.Exec(a.store.q("UPDATE password_reset_codes SET delivered=1 WHERE email=? AND code_hash=? AND delivered=0 AND expires_at>?"), email, hash, time.Now().Unix())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fail(http.StatusConflict, "验证码已更新或过期，请使用最新邮件中的验证码")
	}
	return nil
}

func (a *App) confirmPasswordReset(w http.ResponseWriter, r *http.Request, _ User) error {
	var in struct {
		Email            string `json:"email"`
		VerificationCode string `json:"verificationCode"`
		Password         string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	email, valid := normalizedEmail(in.Email)
	if !valid || !validVerificationCode(in.VerificationCode) || !validNewPassword(in.Password) {
		return fail(400, "请填写有效邮箱、6 位验证码和至少 6 个字符的密码")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	challengeQuery := "SELECT code_hash,expires_at,attempts,delivered FROM password_reset_codes WHERE email=?"
	if s.postgres {
		challengeQuery += " FOR UPDATE"
	}
	var hash string
	var expires int64
	var attempts, delivered int
	err = s.row(challengeQuery, email).Scan(&hash, &expires, &attempts, &delivered)
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
		if err = s.exec("UPDATE password_reset_codes SET attempts=attempts+1 WHERE email=? AND code_hash=?", email, hash); err != nil {
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
	var userID string
	err = s.row("SELECT id FROM users WHERE email=?", email).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(400, "验证码无效或已过期，请重新获取")
	}
	if err != nil {
		return err
	}
	passwordHash, err := hashPassword(in.Password)
	if err != nil {
		return err
	}
	if err = s.exec("UPDATE users SET password_hash=? WHERE id=?", string(passwordHash), userID); err != nil {
		return err
	}
	if err = s.exec("DELETE FROM sessions WHERE user_id=?", userID); err != nil {
		return err
	}
	if err = s.exec("DELETE FROM password_reset_codes WHERE email=? AND code_hash=?", email, hash); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, http.StatusOK, map[string]string{"message": "密码已重置，请使用新密码登录。"})
	return nil
}

func (a *App) setMemberPassword(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(http.StatusForbidden, "只有系统管理员可手动设置成员密码")
	}
	var in struct {
		Password        string `json:"password"`
		CurrentPassword string `json:"currentPassword"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if !validNewPassword(in.Password) {
		return fail(400, "密码至少需要 6 个字符")
	}
	if r.PathValue("id") == u.ID {
		if !a.rateAllowed(r, "self-password:"+u.ID) {
			return rateLimit("尝试次数过多，请稍后重试", 900)
		}
		var currentHash string
		if err := a.store.row("SELECT password_hash FROM users WHERE id=?", u.ID).Scan(&currentHash); err != nil {
			return err
		}
		if comparePassword(currentHash, in.CurrentPassword) != nil {
			return fail(400, "当前密码不正确；忘记密码可通过邮箱重置")
		}
	}
	passwordHash, err := hashPassword(in.Password)
	if err != nil {
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
	if err = s.exec("UPDATE users SET password_hash=? WHERE id=?", string(passwordHash), member.ID); err != nil {
		return err
	}
	if err = s.exec("DELETE FROM sessions WHERE user_id=?", member.ID); err != nil {
		return err
	}
	if err = s.exec("DELETE FROM password_reset_codes WHERE email=?", member.Email); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(u, "管理员修改成员密码", member.Name)
	respond(w, http.StatusOK, map[string]string{"message": "成员密码已更新，原有登录会话已退出。"})
	return nil
}
