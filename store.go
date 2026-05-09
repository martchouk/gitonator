package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type TaskRow struct {
	ID          int64           `json:"id"`
	IssueNumber int             `json:"issue_number"`
	Role        string          `json:"role"`
	Assignee    string          `json:"assignee"`
	Action      string          `json:"action"`
	Status      string          `json:"status"`
	DedupKey    string          `json:"dedup_key"`
	PayloadRaw  string          `json:"payload_json"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   string          `json:"created_at"`
	ClaimedAt   sql.NullString  `json:"claimed_at"`
	FinishedAt  sql.NullString  `json:"finished_at"`
	HeartbeatAt sql.NullString  `json:"heartbeat_at"`
	ClaimedBy   sql.NullString  `json:"claimed_by"`
	ErrorText   sql.NullString  `json:"error_text"`
}

type TransitionAuditRow struct {
	ID               int64           `json:"id"`
	IssueNumber      int             `json:"issue_number"`
	FromStatus       string          `json:"from_status"`
	ToStatus         string          `json:"to_status"`
	FromAssignee     string          `json:"from_assignee"`
	ToAssignee       string          `json:"to_assignee"`
	Actor            string          `json:"actor"`
	TriggerType      string          `json:"trigger_type"`
	TriggerCommentID sql.NullInt64   `json:"trigger_comment_id"`
	Result           string          `json:"result"`
	Reason           string          `json:"reason"`
	ValidationRaw    string          `json:"validation_json"`
	Validation       json.RawMessage `json:"validation"`
	MetadataRaw      string          `json:"metadata_json"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedAt        string          `json:"created_at"`
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS deliveries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			delivery_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			received_at TEXT NOT NULL,
			processed_at TEXT,
			status TEXT NOT NULL,
			error_text TEXT,
			payload_json TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_number INTEGER NOT NULL,
			role TEXT NOT NULL,
			assignee TEXT NOT NULL,
			action TEXT NOT NULL,
			status TEXT NOT NULL,
			dedup_key TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			claimed_at TEXT,
			finished_at TEXT,
			heartbeat_at TEXT,
			claimed_by TEXT,
			last_message TEXT,
			error_text TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_issue_status ON tasks(issue_number, status);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_assignee_status ON tasks(assignee, status, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_dedup_status ON tasks(dedup_key, status, created_at);`,
		`CREATE TABLE IF NOT EXISTS failures (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_number INTEGER,
			stage TEXT NOT NULL,
			error_text TEXT NOT NULL,
			created_at TEXT NOT NULL,
			context_json TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS transition_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_number INTEGER NOT NULL,
			from_status TEXT,
			to_status TEXT,
			from_assignee TEXT,
			to_assignee TEXT,
			actor TEXT,
			trigger_type TEXT NOT NULL,
			trigger_comment_id INTEGER,
			result TEXT NOT NULL,
			reason TEXT,
			validation_json TEXT,
			metadata_json TEXT,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_transition_audit_issue_created
			ON transition_audit(issue_number, created_at, id);`,
		`CREATE INDEX IF NOT EXISTS idx_transition_audit_issue_result
			ON transition_audit(issue_number, result, created_at);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	_, _ = db.Exec(`ALTER TABLE tasks ADD COLUMN dedup_key TEXT NOT NULL DEFAULT ''`)

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) RecordDelivery(id, eventType string, payload []byte) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO deliveries (delivery_id, event_type, received_at, status, payload_json)
		 VALUES (?, ?, ?, 'received', ?)`,
		id, eventType, nowUTC(), string(payload),
	)
	return err
}

func (s *Store) IsDeliveryProcessed(id string) (bool, error) {
	var status string
	err := s.db.QueryRow(`SELECT status FROM deliveries WHERE delivery_id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == "processed", nil
}

func (s *Store) MarkDeliveryProcessed(id string) error {
	_, err := s.db.Exec(`UPDATE deliveries SET status = 'processed', processed_at = ? WHERE delivery_id = ?`, nowUTC(), id)
	return err
}

func (s *Store) MarkDeliveryFailed(id, errText string) error {
	_, err := s.db.Exec(`UPDATE deliveries SET status = 'failed', processed_at = ?, error_text = ? WHERE delivery_id = ?`, nowUTC(), errText, id)
	return err
}

func (s *Store) QueueTask(task AgentTask) (int64, error) {
	b, _ := json.Marshal(task)
	res, err := s.db.Exec(
		`INSERT INTO tasks (issue_number, role, assignee, action, status, dedup_key, payload_json, created_at)
		 VALUES (?, ?, ?, ?, 'queued', ?, ?, ?)`,
		task.IssueNumber, task.Role, task.Assignee, task.Action, task.DedupKey, string(b), nowUTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FindActiveTaskByDedupKey(dedupKey string) (*TaskRow, error) {
	row := s.db.QueryRow(
		`SELECT id, issue_number, role, assignee, action, status, dedup_key, payload_json, created_at, claimed_at, finished_at, heartbeat_at, claimed_by, error_text
		 FROM tasks
		 WHERE dedup_key = ? AND status IN ('queued','claimed','running')
		 ORDER BY id DESC
		 LIMIT 1`,
		dedupKey,
	)

	var t TaskRow
	if err := row.Scan(
		&t.ID,
		&t.IssueNumber,
		&t.Role,
		&t.Assignee,
		&t.Action,
		&t.Status,
		&t.DedupKey,
		&t.PayloadRaw,
		&t.CreatedAt,
		&t.ClaimedAt,
		&t.FinishedAt,
		&t.HeartbeatAt,
		&t.ClaimedBy,
		&t.ErrorText,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	t.Payload = json.RawMessage(t.PayloadRaw)
	return &t, nil
}

func (s *Store) ListQueuedTasksForAssignee(assignee string, limit int) ([]TaskRow, error) {
	rows, err := s.db.Query(
		`SELECT id, issue_number, role, assignee, action, status, dedup_key, payload_json, created_at, claimed_at, finished_at, heartbeat_at, claimed_by, error_text
		 FROM tasks
		 WHERE assignee = ? AND status IN ('queued','claimed','running')
		 ORDER BY created_at ASC
		 LIMIT ?`,
		assignee, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskRow
	for rows.Next() {
		var t TaskRow
		if err := rows.Scan(
			&t.ID,
			&t.IssueNumber,
			&t.Role,
			&t.Assignee,
			&t.Action,
			&t.Status,
			&t.DedupKey,
			&t.PayloadRaw,
			&t.CreatedAt,
			&t.ClaimedAt,
			&t.FinishedAt,
			&t.HeartbeatAt,
			&t.ClaimedBy,
			&t.ErrorText,
		); err != nil {
			return nil, err
		}
		t.Payload = json.RawMessage(t.PayloadRaw)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ClaimTask(taskID int64, agent string) error {
	res, err := s.db.Exec(
		`UPDATE tasks
		 SET status = 'claimed', claimed_at = ?, heartbeat_at = ?, claimed_by = ?
		 WHERE id = ? AND status = 'queued'`,
		nowUTC(), nowUTC(), agent, taskID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task not claimable")
	}
	return nil
}

func (s *Store) HeartbeatTask(taskID int64, agent, message string) error {
	res, err := s.db.Exec(
		`UPDATE tasks
		 SET status = 'running', heartbeat_at = ?, claimed_by = COALESCE(claimed_by, ?), last_message = ?
		 WHERE id = ? AND status IN ('claimed','running')`,
		nowUTC(), agent, message, taskID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task not running/claimed")
	}
	return nil
}

func (s *Store) CompleteTask(taskID int64, agent string, result map[string]any, message string) error {
	raw, _ := json.Marshal(result)
	res, err := s.db.Exec(
		`UPDATE tasks
		 SET status = 'completed', finished_at = ?, heartbeat_at = ?, claimed_by = COALESCE(claimed_by, ?), last_message = ?, error_text = NULL
		 WHERE id = ? AND status IN ('claimed','running')`,
		nowUTC(), nowUTC(), agent, withResultMessage(message, raw), taskID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task not completable")
	}
	return nil
}

func (s *Store) FailTask(taskID int64, agent, message string, result map[string]any) error {
	raw, _ := json.Marshal(result)
	res, err := s.db.Exec(
		`UPDATE tasks
		 SET status = 'failed', finished_at = ?, heartbeat_at = ?, claimed_by = COALESCE(claimed_by, ?), error_text = ?
		 WHERE id = ? AND status IN ('claimed','running','queued')`,
		nowUTC(), nowUTC(), agent, withResultMessage(message, raw), taskID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task not fail-able")
	}
	return nil
}

func (s *Store) RecoverStaleTasks(staleAfterSeconds int) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE tasks
		 SET status = 'queued',
		     claimed_at = NULL,
		     claimed_by = NULL,
		     last_message = 'recovered stale task',
		     error_text = NULL
		 WHERE status IN ('claimed','running')
		   AND heartbeat_at IS NOT NULL
		   AND heartbeat_at < datetime('now', '-' || ? || ' seconds')`,
		staleAfterSeconds,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func withResultMessage(message string, raw []byte) string {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return message
	}
	if message == "" {
		return string(raw)
	}
	return message + " | result=" + string(raw)
}

func (s *Store) RecordFailure(issueNumber int, stage, errText string, ctxJSON interface{}) error {
	b, _ := json.Marshal(ctxJSON)
	_, err := s.db.Exec(
		`INSERT INTO failures (issue_number, stage, error_text, created_at, context_json)
		 VALUES (?, ?, ?, ?, ?)`,
		issueNumber, stage, errText, nowUTC(), string(b),
	)
	return err
}

func (s *Store) RecordTransitionAudit(
	issueNumber int,
	fromStatus string,
	toStatus string,
	fromAssignee string,
	toAssignee string,
	actor string,
	triggerType string,
	triggerCommentID *int64,
	result string,
	reason string,
	validation interface{},
	metadata interface{},
) error {
	var triggerID interface{}
	if triggerCommentID != nil {
		triggerID = *triggerCommentID
	}

	var validationJSON string
	if validation != nil {
		if b, err := json.Marshal(validation); err == nil {
			validationJSON = string(b)
		}
	}

	var metadataJSON string
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	_, err := s.db.Exec(
		`INSERT INTO transition_audit (
			issue_number,
			from_status,
			to_status,
			from_assignee,
			to_assignee,
			actor,
			trigger_type,
			trigger_comment_id,
			result,
			reason,
			validation_json,
			metadata_json,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		issueNumber,
		fromStatus,
		toStatus,
		fromAssignee,
		toAssignee,
		actor,
		triggerType,
		triggerID,
		result,
		reason,
		validationJSON,
		metadataJSON,
		nowUTC(),
	)
	return err
}

func (s *Store) ListTransitionAudit(issueNumber int, limit int) ([]TransitionAuditRow, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.Query(
		`SELECT
			id,
			issue_number,
			from_status,
			to_status,
			from_assignee,
			to_assignee,
			actor,
			trigger_type,
			trigger_comment_id,
			result,
			reason,
			validation_json,
			metadata_json,
			created_at
		 FROM transition_audit
		 WHERE issue_number = ?
		 ORDER BY id DESC
		 LIMIT ?`,
		issueNumber,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TransitionAuditRow
	for rows.Next() {
		var r TransitionAuditRow
		if err := rows.Scan(
			&r.ID,
			&r.IssueNumber,
			&r.FromStatus,
			&r.ToStatus,
			&r.FromAssignee,
			&r.ToAssignee,
			&r.Actor,
			&r.TriggerType,
			&r.TriggerCommentID,
			&r.Result,
			&r.Reason,
			&r.ValidationRaw,
			&r.MetadataRaw,
			&r.CreatedAt,
		); err != nil {
			return nil, err
		}
		r.Validation = json.RawMessage(r.ValidationRaw)
		r.Metadata = json.RawMessage(r.MetadataRaw)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListTasksByIssue(issueNumber int, limit int) ([]TaskRow, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.Query(
		`SELECT
			id,
			issue_number,
			role,
			assignee,
			action,
			status,
			dedup_key,
			payload_json,
			created_at,
			claimed_at,
			finished_at,
			heartbeat_at,
			claimed_by,
			error_text
		 FROM tasks
		 WHERE issue_number = ?
		 ORDER BY id DESC
		 LIMIT ?`,
		issueNumber,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskRow
	for rows.Next() {
		var t TaskRow
		if err := rows.Scan(
			&t.ID,
			&t.IssueNumber,
			&t.Role,
			&t.Assignee,
			&t.Action,
			&t.Status,
			&t.DedupKey,
			&t.PayloadRaw,
			&t.CreatedAt,
			&t.ClaimedAt,
			&t.FinishedAt,
			&t.HeartbeatAt,
			&t.ClaimedBy,
			&t.ErrorText,
		); err != nil {
			return nil, err
		}
		t.Payload = json.RawMessage(t.PayloadRaw)
		out = append(out, t)
	}
	return out, rows.Err()
}
