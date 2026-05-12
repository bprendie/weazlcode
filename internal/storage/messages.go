package storage

import (
	"database/sql"
	"errors"
)

func (s *Store) AddMessage(sessionID, role, content string) error {
	return s.AddMessageWithTools(sessionID, role, content, "", "")
}

func (s *Store) AddMessageWithTools(sessionID, role, content, toolCalls, toolCallID string) error {
	if !s.unlocked {
		return errors.New("database is locked")
	}
	blob, err := s.encrypt(content)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`insert into messages (session_id, role, content, tool_calls, tool_call_id) values (?, ?, ?, ?, ?)`,
		sessionID, role, blob, toolCalls, toolCallID,
	)
	return err
}

func (s *Store) Messages(sessionID string) ([]Message, error) {
	if !s.unlocked {
		return nil, errors.New("database is locked")
	}
	rows, err := s.db.Query(`select id, session_id, role, content, tool_calls, tool_call_id, created_at from messages where session_id = ? order by id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanMessages(rows)
}

func (s *Store) scanMessages(rows *sql.Rows) ([]Message, error) {
	var messages []Message
	for rows.Next() {
		var msg Message
		var enc string
		var toolCalls, toolCallID sql.NullString
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &enc, &toolCalls, &toolCallID, &msg.CreatedAt); err != nil {
			return nil, err
		}
		content, err := s.decrypt(enc)
		if err != nil {
			return nil, err
		}
		msg.Content = content
		if toolCalls.Valid {
			msg.ToolCalls = toolCalls.String
		}
		if toolCallID.Valid {
			msg.ToolCallID = toolCallID.String
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (s *Store) MessagesAfter(sessionID string, afterID int64) ([]Message, error) {
	if !s.unlocked {
		return nil, errors.New("database is locked")
	}
	rows, err := s.db.Query(`select id, session_id, role, content, tool_calls, tool_call_id, created_at from messages where session_id = ? and id > ? order by id`, sessionID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanMessages(rows)
}

func (s *Store) MessagesThrough(sessionID string, throughID int64) ([]Message, error) {
	if !s.unlocked {
		return nil, errors.New("database is locked")
	}
	rows, err := s.db.Query(`select id, session_id, role, content, tool_calls, tool_call_id, created_at from messages where session_id = ? and id <= ? order by id`, sessionID, throughID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanMessages(rows)
}

func (s *Store) SaveContextCheckpoint(sessionID string, throughMessageID int64, summary string) error {
	if !s.unlocked {
		return errors.New("database is locked")
	}
	blob, err := s.encrypt(summary)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`insert into context_checkpoints (session_id, through_message_id, summary) values (?, ?, ?)`,
		sessionID, throughMessageID, blob,
	)
	return err
}

func (s *Store) LatestContextCheckpoint(sessionID string) (ContextCheckpoint, bool, error) {
	if !s.unlocked {
		return ContextCheckpoint{}, false, errors.New("database is locked")
	}
	row := s.db.QueryRow(`select id, session_id, through_message_id, summary, created_at from context_checkpoints where session_id = ? order by id desc limit 1`, sessionID)
	var cp ContextCheckpoint
	var enc string
	if err := row.Scan(&cp.ID, &cp.SessionID, &cp.ThroughMessageID, &enc, &cp.CreatedAt); errors.Is(err, sql.ErrNoRows) {
		return ContextCheckpoint{}, false, nil
	} else if err != nil {
		return ContextCheckpoint{}, false, err
	}
	summary, err := s.decrypt(enc)
	if err != nil {
		return ContextCheckpoint{}, false, err
	}
	cp.Summary = summary
	return cp, true, nil
}

func (s *Store) ClearSessionContext(sessionID string) error {
	if !s.unlocked {
		return errors.New("database is locked")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from messages where session_id = ?`, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(`delete from context_checkpoints where session_id = ?`, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`update sessions
		 set title = 'New session',
		     input_tokens = 0,
		     output_tokens = 0,
		     updated_at = current_timestamp
		 where id = ?`,
		sessionID,
	); err != nil {
		return err
	}
	return tx.Commit()
}
