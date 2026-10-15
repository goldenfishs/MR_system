package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Runtime mail settings are immutable snapshots. An in-flight send keeps the
// configuration it started with; saving settings only affects subsequent sends.
type mailServiceSnapshot struct {
	config       SMTPConfig
	enabled      bool
	source       string
	verification VerificationMailer
	notice       NoticeMailer
}

// SMTP credentials are separate from public site settings and encrypted at rest.
// The key lives alongside uploaded data, so database-only backups contain no key.
type storedMailService struct {
	Enabled            bool       `json:"enabled"`
	Config             SMTPConfig `json:"config"`
	PasswordCiphertext string     `json:"passwordCiphertext,omitempty"`
}

func (a *App) registerAdminSettingsRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/settings/mail-service", a.wrap(true, a.getMailService))
	m.HandleFunc("PATCH /api/settings/mail-service", a.wrap(true, a.updateMailService))
}

func (a *App) currentMailService() mailServiceSnapshot {
	a.mailMu.RLock()
	defer a.mailMu.RUnlock()
	if a.mailService != nil {
		return *a.mailService
	}
	cfg := SMTPConfig{From: a.config.EmailFrom, Security: a.config.EmailSecurity, Port: 587, FromName: "QUT机器人战队", Demo: a.config.Demo}
	if smtp, ok := a.config.Mailer.(*smtpMailer); ok {
		cfg = smtp.config
	}
	if cfg.Security == "" {
		cfg.Security = "starttls"
	}
	return mailServiceSnapshot{config: cfg, enabled: a.config.Mailer != nil, source: "environment", verification: a.config.Mailer, notice: a.config.NoticeMailer}
}

func mailServiceResponse(s mailServiceSnapshot) map[string]any {
	return map[string]any{
		"enabled": s.enabled, "configured": s.verification != nil, "supportsBulk": s.notice != nil,
		"host": s.config.Host, "port": s.config.Port, "username": s.config.Username,
		"from": s.config.From, "fromName": s.config.FromName, "security": s.config.Security,
		"passwordSet": s.config.Password != "", "source": s.source,
	}
}

func (a *App) getMailService(w http.ResponseWriter, _ *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可查看邮箱服务配置")
	}
	respond(w, 200, mailServiceResponse(a.currentMailService()))
	return nil
}

func (a *App) updateMailService(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可修改邮箱服务配置")
	}
	var in struct {
		Enabled       *bool   `json:"enabled"`
		Host          *string `json:"host"`
		Port          *int    `json:"port"`
		Username      *string `json:"username"`
		Password      *string `json:"password"`
		ClearPassword bool    `json:"clearPassword"`
		From          *string `json:"from"`
		FromName      *string `json:"fromName"`
		Security      *string `json:"security"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	next := a.currentMailService()
	next.source = "database"
	if in.Enabled != nil {
		next.enabled = *in.Enabled
	}
	for _, field := range []struct {
		dst *string
		src *string
	}{
		{&next.config.Host, in.Host}, {&next.config.Username, in.Username},
		{&next.config.From, in.From}, {&next.config.FromName, in.FromName}, {&next.config.Security, in.Security},
	} {
		if field.src != nil {
			*field.dst = strings.TrimSpace(*field.src)
		}
	}
	if in.Port != nil {
		next.config.Port = *in.Port
	}
	if in.ClearPassword {
		next.config.Password = ""
	} else if in.Password != nil && *in.Password != "" {
		next.config.Password = *in.Password
	}
	next.config.Demo = a.config.Demo
	if len(next.config.Host) > 253 || len(next.config.Username) > 254 || strings.ContainsAny(next.config.Username, "\r\n") || len(next.config.Password) > 4096 {
		return fail(400, "邮件服务器、账号或授权码格式不正确")
	}
	next.verification, next.notice = nil, nil
	// Validate saved non-empty configurations even when sending is disabled.
	if next.enabled || next.config.Host != "" {
		mailer, err := newSMTPMailer(next.config)
		if err != nil {
			return fail(400, "请检查 SMTP 主机、端口、发件邮箱、加密方式及账号授权码；账号与授权码需同时填写")
		}
		if next.enabled {
			next.verification, next.notice = mailer, mailer
		}
	} else if next.config.Username != "" || next.config.Password != "" || next.config.From != "" {
		return fail(400, "填写邮件账号或发件邮箱时，也需要填写 SMTP 服务器")
	}
	stored := storedMailService{Enabled: next.enabled, Config: next.config}
	stored.Config.Password, stored.Config.Demo = "", false
	if next.config.Password != "" {
		key, err := a.mailSettingsKey(true)
		if err != nil {
			return fail(500, "无法保存邮件配置密钥，请检查服务器数据目录权限")
		}
		stored.PasswordCiphertext, err = encryptMailPassword(key, next.config.Password)
		if err != nil {
			return fail(500, "无法加密邮件配置")
		}
	}
	if err := a.store.exec("INSERT INTO settings(id,payload) VALUES('mail-service',?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", encoded(stored)); err != nil {
		return err
	}
	a.mailMu.Lock()
	a.mailService = &next
	a.mailMu.Unlock()
	a.activity(u, "更新邮件服务设置", "SMTP 配置")
	respond(w, 200, mailServiceResponse(next))
	return nil
}

func (a *App) loadStoredMailService() error {
	stored, err := readOne[storedMailService](a.store, "SELECT payload FROM settings WHERE id='mail-service'")
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errors.New("cannot read persisted mail settings")
	}
	stored.Config.Demo = a.config.Demo
	if stored.PasswordCiphertext != "" {
		key, err := a.mailSettingsKey(false)
		if err != nil {
			return errors.New("mail settings key is unavailable in the data directory")
		}
		stored.Config.Password, err = decryptMailPassword(key, stored.PasswordCiphertext)
		if err != nil {
			return errors.New("cannot decrypt persisted mail settings")
		}
	}
	next := mailServiceSnapshot{config: stored.Config, enabled: stored.Enabled, source: "database"}
	if next.enabled {
		mailer, err := newSMTPMailer(next.config)
		if err != nil {
			return errors.New("persisted mail settings are invalid")
		}
		next.verification, next.notice = mailer, mailer
	}
	a.mailMu.Lock()
	a.mailService = &next
	a.mailMu.Unlock()
	return nil
}

func (a *App) mailSettingsKey(create bool) ([]byte, error) {
	path := filepath.Join(filepath.Dir(a.store.fileDir), "mail-settings.key")
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(openErr, os.ErrExist) {
			return a.mailSettingsKey(false)
		}
		if openErr != nil {
			return nil, openErr
		}
		_, err = file.Write(key)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid mail settings key length")
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func encryptMailPassword(key []byte, password string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	encrypted := gcm.Seal(nonce, nonce, []byte(password), []byte("mr-system/mail-service/v1"))
	return base64.RawStdEncoding.EncodeToString(encrypted), nil
}

func decryptMailPassword(key []byte, value string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	encrypted, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(encrypted) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted mail password")
	}
	plain, err := gcm.Open(nil, encrypted[:gcm.NonceSize()], encrypted[gcm.NonceSize():], []byte("mr-system/mail-service/v1"))
	return string(plain), err
}
