package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/sha3"
)

type Store struct {
	db       *sql.DB
	key      []byte
	unlocked bool
}

type Session struct {
	ID           string
	Title        string
	Provider     string
	Model        string
	ProjectRoot  string
	InputTokens  int
	OutputTokens int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Message struct {
	ID         int64
	SessionID  string
	Role       string
	Content    string
	ToolCalls  string // JSON-encoded tool calls (for assistant messages)
	ToolCallID string // Tool call ID (for tool result messages)
	CreatedAt  time.Time
}

type WorkspaceSave struct {
	ID               int64
	Name             string
	SessionID        string
	ThroughMessageID int64
	CreatedAt        time.Time
}

type Memory struct {
	Key       string
	Value     string
	Tags      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ProjectMemory struct {
	ID          int64
	ProjectRoot string
	Key         string
	Value       string
	Tags        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ContextCheckpoint struct {
	ID               int64
	SessionID        string
	ThroughMessageID int64
	Summary          string
	CreatedAt        time.Time
}

func Open(path string) (*Store, error) {
	if err := mkdirFor(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate() error {
	stmts := []string{
		`create table if not exists vault (
			id integer primary key check (id = 1),
			password_hash text not null,
			created_at datetime not null default current_timestamp
		)`,
		`create table if not exists sessions (
			id text primary key,
			title text not null,
			provider text not null,
			model text not null,
			project_root text not null default '',
			created_at datetime not null default current_timestamp,
			updated_at datetime not null default current_timestamp
		)`,
		`create table if not exists messages (
			id integer primary key autoincrement,
			session_id text not null references sessions(id) on delete cascade,
			role text not null,
			content text not null,
			tool_calls text,
			tool_call_id text,
			created_at datetime not null default current_timestamp
		)`,
		`create table if not exists workspace_saves (
			id integer primary key autoincrement,
			name text not null,
			session_id text not null references sessions(id) on delete cascade,
			snapshot text not null,
			created_at datetime not null default current_timestamp
		)`,
		`create table if not exists memories (
			key text primary key,
			value text not null,
			tags text,
			created_at datetime not null default current_timestamp,
			updated_at datetime not null default current_timestamp
		)`,
		`create table if not exists project_memories (
			id integer primary key autoincrement,
			project_root text not null,
			key text not null,
			value text not null,
			tags text,
			created_at datetime not null default current_timestamp,
			updated_at datetime not null default current_timestamp,
			unique(project_root, key)
		)`,
		`create table if not exists context_checkpoints (
			id integer primary key autoincrement,
			session_id text not null references sessions(id) on delete cascade,
			through_message_id integer not null,
			summary text not null,
			created_at datetime not null default current_timestamp
		)`,
		`create table if not exists plans (
			id text primary key,
			session_id text not null references sessions(id) on delete cascade,
			project_root text not null default '',
			title text not null,
			summary text not null,
			status text not null,
			created_at datetime not null default current_timestamp,
			updated_at datetime not null default current_timestamp
		)`,
		`create table if not exists tasks (
			id text primary key,
			plan_id text not null references plans(id) on delete cascade,
			title text not null,
			goal text not null,
			status text not null,
			allowed_paths text not null default '[]',
			forbidden_paths text not null default '[]',
			context_files text not null default '[]',
			skills text not null default '[]',
			verification text not null default '[]',
			acceptance_checks text not null default '[]',
			created_at datetime not null default current_timestamp,
			updated_at datetime not null default current_timestamp
		)`,
		`create table if not exists task_events (
			id integer primary key autoincrement,
			task_id text not null references tasks(id) on delete cascade,
			type text not null,
			message text not null,
			payload text,
			created_at datetime not null default current_timestamp
		)`,
		`create index if not exists idx_messages_session on messages(session_id, id)`,
		`create index if not exists idx_sessions_updated on sessions(updated_at desc)`,
		`create index if not exists idx_memories_updated on memories(updated_at desc)`,
		`create index if not exists idx_project_memories_root on project_memories(project_root, updated_at desc)`,
		`create index if not exists idx_context_checkpoints_session on context_checkpoints(session_id, id desc)`,
		`create index if not exists idx_plans_session on plans(session_id, updated_at desc)`,
		`create index if not exists idx_tasks_plan on tasks(plan_id, created_at)`,
		`create index if not exists idx_task_events_task on task_events(task_id, id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("sessions", "input_tokens", "integer not null default 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("sessions", "output_tokens", "integer not null default 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("sessions", "project_root", "text not null default ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("messages", "tool_calls", "text"); err != nil {
		return err
	}
	if err := s.ensureColumn("messages", "tool_call_id", "text"); err != nil {
		return err
	}
	if err := s.ensureColumn("workspace_saves", "through_message_id", "integer not null default 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("tasks", "skills", "text not null default '[]'"); err != nil {
		return err
	}
	return nil
}

func (s *Store) HasVault() (bool, error) {
	var count int
	if err := s.db.QueryRow(`select count(*) from vault where id = 1`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) CreateVault(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`insert into vault (id, password_hash) values (1, ?)`, string(hash))
	if err != nil {
		return err
	}
	s.unlockWith(password)
	return nil
}

func (s *Store) Unlock(password string) error {
	var hash string
	if err := s.db.QueryRow(`select password_hash from vault where id = 1`).Scan(&hash); err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return errors.New("bad database password")
	}
	s.unlockWith(password)
	return nil
}

func (s *Store) Unlocked() bool {
	return s.unlocked
}

func (s *Store) CreateSession(id, title, provider, model string) error {
	return s.CreateProjectSession(id, title, provider, model, "")
}

func (s *Store) CreateProjectSession(id, title, provider, model, projectRoot string) error {
	_, err := s.db.Exec(
		`insert into sessions (id, title, provider, model, project_root, updated_at) values (?, ?, ?, ?, ?, current_timestamp)`,
		id, title, provider, model, projectRoot,
	)
	return err
}

func (s *Store) TouchSession(id, title string) error {
	_, err := s.db.Exec(`update sessions set title = ?, updated_at = current_timestamp where id = ?`, title, id)
	return err
}

func (s *Store) AddSessionTokens(id string, inputTokens, outputTokens int) error {
	_, err := s.db.Exec(
		`update sessions
		 set input_tokens = input_tokens + ?,
		     output_tokens = output_tokens + ?,
		     updated_at = current_timestamp
		 where id = ?`,
		inputTokens, outputTokens, id,
	)
	return err
}

func (s *Store) LatestSession() (Session, bool, error) {
	rows, err := s.db.Query(`select id, title, provider, model, project_root, input_tokens, output_tokens, created_at, updated_at from sessions order by updated_at desc limit 1`)
	if err != nil {
		return Session{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Session{}, false, rows.Err()
	}
	sess, err := scanSession(rows)
	return sess, err == nil, err
}

func (s *Store) ListSessions(limit int) ([]Session, error) {
	rows, err := s.db.Query(`select id, title, provider, model, project_root, input_tokens, output_tokens, created_at, updated_at from sessions order by updated_at desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) Session(id string) (Session, bool, error) {
	rows, err := s.db.Query(`select id, title, provider, model, project_root, input_tokens, output_tokens, created_at, updated_at from sessions where id = ?`, id)
	if err != nil {
		return Session{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Session{}, false, rows.Err()
	}
	sess, err := scanSession(rows)
	return sess, err == nil, err
}

func (s *Store) DeleteSession(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`delete from messages where session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`delete from workspace_saves where session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`delete from sessions where id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) unlockWith(password string) {
	sum := sha3.Sum256([]byte("weazlcode/local-only/" + password))
	s.key = sum[:]
	s.unlocked = true
}

func (s *Store) encrypt(plain string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := append(nonce, gcm.Seal(nil, nonce, []byte(plain), nil)...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (s *Store) decrypt(blob string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("encrypted payload is too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt payload: %w", err)
	}
	return string(plain), nil
}

func scanSession(rows interface {
	Scan(dest ...any) error
}) (Session, error) {
	var sess Session
	return sess, rows.Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &sess.ProjectRoot, &sess.InputTokens, &sess.OutputTokens, &sess.CreatedAt, &sess.UpdatedAt)
}

func (s *Store) ensureColumn(table, name, spec string) error {
	rows, err := s.db.Query(`pragma table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if columnName == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`alter table ` + table + ` add column ` + name + ` ` + spec)
	return err
}

func mkdirFor(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}
