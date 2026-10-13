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
	s := &Store{db: db, postgres: driver == "pgx"}
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
	}
	for _, q := range statements {
		if _, err = db.ExecContext(ctx, q); err != nil {
			db.Close()
			return nil, err
		}
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
	return readOne[Settings](s, "SELECT payload FROM settings WHERE id='default'")
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
