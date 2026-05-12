package storage

import (
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) SaveWorkspace(name, sessionID, snapshot string, throughMessageID int64) (int64, error) {
	if !s.unlocked {
		return 0, errors.New("database is locked")
	}
	blob, err := s.encrypt(snapshot)
	if err != nil {
		return 0, err
	}
	result, err := s.db.Exec(
		`insert into workspace_saves (name, session_id, snapshot, through_message_id) values (?, ?, ?, ?)`,
		name, sessionID, blob, throughMessageID,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) RenameWorkspace(id int64, name string) error {
	if !s.unlocked {
		return errors.New("database is locked")
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return errors.New("workspace name is required")
	}
	result, err := s.db.Exec(`update workspace_saves set name = ? where id = ?`, name, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("workspace save not found")
	}
	return nil
}

func (s *Store) UpdateWorkspace(id int64, sessionID, snapshot string, throughMessageID int64) error {
	if !s.unlocked {
		return errors.New("database is locked")
	}
	blob, err := s.encrypt(snapshot)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(
		`update workspace_saves set session_id = ?, snapshot = ?, through_message_id = ? where id = ?`,
		sessionID, blob, throughMessageID, id,
	)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("workspace save not found")
	}
	return nil
}

func (s *Store) DeleteWorkspace(id int64) error {
	if !s.unlocked {
		return errors.New("database is locked")
	}
	result, err := s.db.Exec(`delete from workspace_saves where id = ?`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("workspace save not found")
	}
	return nil
}

func (s *Store) WorkspaceSaves(limit int) ([]WorkspaceSave, error) {
	rows, err := s.db.Query(`select id, name, session_id, through_message_id, created_at from workspace_saves order by created_at desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var saves []WorkspaceSave
	for rows.Next() {
		var save WorkspaceSave
		if err := rows.Scan(&save.ID, &save.Name, &save.SessionID, &save.ThroughMessageID, &save.CreatedAt); err != nil {
			return nil, err
		}
		saves = append(saves, save)
	}
	return saves, rows.Err()
}

func (s *Store) Remember(key, value, tags string) error {
	if !s.unlocked {
		return errors.New("database is locked")
	}
	blob, err := s.encrypt(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`insert into memories (key, value, tags, updated_at)
		 values (?, ?, ?, current_timestamp)
		 on conflict(key) do update set value = excluded.value, tags = excluded.tags, updated_at = current_timestamp`,
		key, blob, tags,
	)
	return err
}

func (s *Store) ForgetMemory(key string) error {
	if !s.unlocked {
		return errors.New("database is locked")
	}
	_, err := s.db.Exec(`delete from memories where key = ?`, key)
	return err
}

func (s *Store) Memories(limit int) ([]Memory, error) {
	if !s.unlocked {
		return nil, errors.New("database is locked")
	}
	rows, err := s.db.Query(`select key, value, coalesce(tags, ''), created_at, updated_at from memories order by updated_at desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanMemories(rows)
}

func (s *Store) RecallMemories(query string, limit int) ([]Memory, error) {
	if !s.unlocked {
		return nil, errors.New("database is locked")
	}
	rows, err := s.db.Query(`select key, value, coalesce(tags, ''), created_at, updated_at from memories order by updated_at desc limit 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	memories, err := s.scanMemories(rows)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		if len(memories) > limit {
			return memories[:limit], nil
		}
		return memories, nil
	}
	filtered := make([]Memory, 0, min(limit, len(memories)))
	for _, memory := range memories {
		haystack := strings.ToLower(memory.Key + "\n" + memory.Tags + "\n" + memory.Value)
		if strings.Contains(haystack, query) {
			filtered = append(filtered, memory)
			if len(filtered) >= limit {
				break
			}
		}
	}
	return filtered, nil
}

func (s *Store) scanMemories(rows *sql.Rows) ([]Memory, error) {
	var memories []Memory
	for rows.Next() {
		var memory Memory
		var enc string
		if err := rows.Scan(&memory.Key, &enc, &memory.Tags, &memory.CreatedAt, &memory.UpdatedAt); err != nil {
			return nil, err
		}
		value, err := s.decrypt(enc)
		if err != nil {
			return nil, err
		}
		memory.Value = value
		memories = append(memories, memory)
	}
	return memories, rows.Err()
}
