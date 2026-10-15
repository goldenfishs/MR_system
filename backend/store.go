package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Store struct {
	db       *sql.DB
	tx       *sql.Tx
	postgres bool
	fileDir  string
	mu       sync.Mutex
}

func openStore(databaseURL, dataDir string) (*Store, error) {
	driver, dsn := "sqlite", ""
	if databaseURL != "" {
		driver, dsn = "pgx", databaseURL
	} else {
		if err := os.MkdirAll(dataDir, 0700); err != nil {
			return nil, err
		}
		dsn = filepath.Join(dataDir, "robot-hub.db")
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, postgres: driver == "pgx", fileDir: filepath.Join(dataDir, "uploads")}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if driver == "sqlite" {
		if _, err = db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return nil, err
		}
		db.Exec("PRAGMA busy_timeout=5000")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), expires_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS applicants (id TEXT PRIMARY KEY, user_id TEXT NOT NULL UNIQUE REFERENCES users(id), payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS submissions (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), user_id TEXT NOT NULL REFERENCES users(id), version INTEGER NOT NULL, payload TEXT NOT NULL, UNIQUE(task_id,user_id,version))`,
		`CREATE TABLE IF NOT EXISTS activities (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, created_at TEXT NOT NULL, payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS settings (id TEXT PRIMARY KEY, payload TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS submission_task ON submissions(task_id,user_id)`,
		`INSERT INTO schema_migrations(version,applied_at) VALUES (1, CURRENT_TIMESTAMP) ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS email_verifications (email TEXT PRIMARY KEY, code_hash TEXT NOT NULL, expires_at BIGINT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, last_sent_at BIGINT NOT NULL, delivered INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS verification_send_events (id TEXT PRIMARY KEY, email TEXT NOT NULL, ip_hash TEXT NOT NULL, created_at BIGINT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS verification_email_time ON verification_send_events(email,created_at)`,
		`CREATE INDEX IF NOT EXISTS verification_ip_time ON verification_send_events(ip_hash,created_at)`,
		`INSERT INTO schema_migrations(version,applied_at) VALUES (2, CURRENT_TIMESTAMP) ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS profiles (user_id TEXT PRIMARY KEY REFERENCES users(id), payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS files (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id), size BIGINT NOT NULL, payload TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS files_owner ON files(owner_id)`,
		`CREATE TABLE IF NOT EXISTS file_grants (file_id TEXT NOT NULL REFERENCES files(id), user_id TEXT NOT NULL REFERENCES users(id), PRIMARY KEY(file_id,user_id))`,
		`CREATE TABLE IF NOT EXISTS teams (id TEXT PRIMARY KEY, payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS team_applications (id TEXT PRIMARY KEY, team_id TEXT NOT NULL REFERENCES teams(id), user_id TEXT NOT NULL REFERENCES users(id), payload TEXT NOT NULL, UNIQUE(team_id,user_id))`,
		`CREATE TABLE IF NOT EXISTS team_memberships (team_id TEXT NOT NULL REFERENCES teams(id), user_id TEXT NOT NULL REFERENCES users(id), direction TEXT NOT NULL, joined_at TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(team_id,user_id))`,
		`CREATE TABLE IF NOT EXISTS records (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), payload TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS records_user ON records(user_id)`,
		`CREATE TABLE IF NOT EXISTS certificate_templates (id TEXT PRIMARY KEY, created_by TEXT NOT NULL REFERENCES users(id), payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS certificate_batches (id TEXT PRIMARY KEY, created_by TEXT NOT NULL REFERENCES users(id), request_id TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE(created_by,request_id))`,
		`CREATE TABLE IF NOT EXISTS mail_campaigns(id TEXT PRIMARY KEY, status TEXT NOT NULL, created_by TEXT NOT NULL, payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS judge_runs(id TEXT PRIMARY KEY, task_id TEXT NOT NULL, user_id TEXT NOT NULL, status TEXT NOT NULL, payload TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS judge_runs_owner ON judge_runs(user_id,task_id)`,
		`INSERT INTO schema_migrations(version,applied_at) VALUES (3, CURRENT_TIMESTAMP) ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS knowledge_resources (id TEXT PRIMARY KEY, payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS discussions (id TEXT PRIMARY KEY, created_by TEXT NOT NULL REFERENCES users(id), payload TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS discussions_author ON discussions(created_by)`,
		`INSERT INTO schema_migrations(version,applied_at) VALUES (4, CURRENT_TIMESTAMP) ON CONFLICT(version) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS training_plans (id TEXT PRIMARY KEY, team_id TEXT NOT NULL, direction TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE(team_id,direction))`,
		`CREATE TABLE IF NOT EXISTS training_progress (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), team_id TEXT NOT NULL, direction TEXT NOT NULL, cycle INTEGER NOT NULL, status TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE(user_id,cycle))`,
		`CREATE INDEX IF NOT EXISTS training_scope ON training_progress(direction,status)`,
		`INSERT INTO schema_migrations(version,applied_at) VALUES (6, CURRENT_TIMESTAMP) ON CONFLICT(version) DO NOTHING`,
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, q := range statements {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			tx.Rollback()
			db.Close()
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) q(q string) string {
	if !s.postgres {
		return q
	}
	n := 0
	var b strings.Builder
	for _, r := range q {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func (s *Store) exec(q string, args ...any) error {
	if s.tx != nil {
		_, err := s.tx.Exec(s.q(q), args...)
		return err
	}
	_, err := s.db.Exec(s.q(q), args...)
	return err
}
func (s *Store) row(q string, args ...any) *sql.Row {
	if s.tx != nil {
		return s.tx.QueryRow(s.q(q), args...)
	}
	return s.db.QueryRow(s.q(q), args...)
}
func readOne[T any](s *Store, q string, args ...any) (T, error) {
	var out T
	var raw string
	err := s.row(q, args...).Scan(&raw)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(raw), &out)
	if u, ok := any(&out).(*User); ok && err == nil {
		normalizeUser(u)
	}
	return out, err
}
func readAll[T any](s *Store, q string, args ...any) ([]T, error) {
	out := []T{}
	var rows *sql.Rows
	var err error
	if s.tx != nil {
		rows, err = s.tx.Query(s.q(q), args...)
	} else {
		rows, err = s.db.Query(s.q(q), args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var item T
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		if u, ok := any(&item).(*User); ok {
			normalizeUser(u)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func encoded(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func (s *Store) saveSettings(v Settings) error {
	return s.exec(`INSERT INTO settings(id,payload) VALUES('default',?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, encoded(v))
}
func (s *Store) settings() (Settings, error) {
	v, err := readOne[Settings](s, "SELECT payload FROM settings WHERE id='default'")
	if err == nil && v.BrandName == "" {
		defaultBrand(&v)
	}
	return v, err
}
func (s *Store) user(id string) (User, error) {
	return readOne[User](s, "SELECT payload FROM users WHERE id=?", id)
}
func (s *Store) applicant(id string) (Applicant, error) {
	return readOne[Applicant](s, "SELECT payload FROM applicants WHERE id=?", id)
}
func (s *Store) task(id string) (Task, error) {
	return readOne[Task](s, "SELECT payload FROM tasks WHERE id=?", id)
}
func (s *Store) submissions(taskID string) ([]Submission, error) {
	return readAll[Submission](s, "SELECT payload FROM submissions WHERE task_id=? ORDER BY version DESC", taskID)
}
