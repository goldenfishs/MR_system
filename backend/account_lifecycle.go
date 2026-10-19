package main

import (
	"net/http"
	"strings"
)

// Account closure is reversible. Identity, memberships, files and learning
// history remain intact, and the email stays reserved to its original account.
func (a *App) deactivateUser(w http.ResponseWriter, r *http.Request, actor User) error {
	if actor.Role != "admin" || actor.DeactivatedAt != "" {
		return fail(403, "只有系统管理员可以注销账号")
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || len(in.Reason) > 2000 || strings.ContainsRune(in.Reason, '\x00') {
		return fail(400, "请填写注销原因，且不超过 2000 字节")
	}
	if r.PathValue("id") == actor.ID {
		return fail(409, "不能注销当前登录的管理员账号")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	target, err := s.user(r.PathValue("id"))
	if err != nil {
		return err
	}
	if target.DeactivatedAt != "" {
		return fail(409, "该账号已经注销")
	}
	if target.Role == "admin" {
		users, err := readAll[User](s, "SELECT payload FROM users")
		if err != nil {
			return err
		}
		active := 0
		for _, u := range users {
			if u.Role == "admin" && u.DeactivatedAt == "" {
				active++
			}
		}
		if active <= 1 {
			return fail(409, "不能注销最后一位有效管理员")
		}
	}
	target.DeactivatedAt, target.DeactivatedBy, target.DeactivationReason = now(), actor.ID, in.Reason
	if err = s.exec("UPDATE users SET payload=? WHERE id=?", encoded(target), target.ID); err != nil {
		return err
	}
	if err = s.exec("DELETE FROM sessions WHERE user_id=?", target.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(actor, "注销用户账号", target.Name)
	respond(w, 200, map[string]any{"item": target})
	return nil
}

func (a *App) reactivateUser(w http.ResponseWriter, r *http.Request, actor User) error {
	if actor.Role != "admin" || actor.DeactivatedAt != "" {
		return fail(403, "只有系统管理员可以恢复账号")
	}
	var in struct{}
	if err := decode(r, &in); err != nil {
		return err
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	target, err := s.user(r.PathValue("id"))
	if err != nil {
		return err
	}
	if target.DeactivatedAt == "" {
		return fail(409, "该账号处于正常状态，无需恢复")
	}
	target.DeactivatedAt, target.DeactivatedBy, target.DeactivationReason = "", "", ""
	if err = s.exec("UPDATE users SET payload=? WHERE id=?", encoded(target), target.ID); err != nil {
		return err
	}
	// Defense in depth: even an independently restored stale session cannot return.
	if err = s.exec("DELETE FROM sessions WHERE user_id=?", target.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.activity(actor, "恢复用户账号", target.Name)
	respond(w, 200, map[string]any{"item": target})
	return nil
}
