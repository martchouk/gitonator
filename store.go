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

// TaskRow is used for the timeline and task inspection tools.
// Nullable legacy columns are retained for backward compatibility with old task data.
type TaskRow struct {
	ID            int64          `json:"id"`
	IssueNumber   int            `json:"issue_number"`
	Repo          string         `json:"repo"`
	Role          string         `json:"role"`
	Assignee      string         `json:"assignee"`
	LastCommentID int64          `json:"last_comment_id"`
	CurrentStatus string         `json:"current_status"`
	Status        string         `json:"status"`
	DedupKey      string         `json:"dedup_key"`
	BridgeID      sql.NullString `json:"bridge_id"`
	CreatedAt     string         `json:"created_at"`
	// Legacy columns — populated from old data only.
	ClaimedAt   sql.NullString  `json:"claimed_at,omitempty"`
	FinishedAt  sql.NullString  `json:"finished_at,omitempty"`
	HeartbeatAt sql.NullString  `json:"heartbeat_at,omitempty"`
	ClaimedBy   sql.NullString  `json:"claimed_by,omitempty"`
	ErrorText   sql.NullString  `json:"error_text,omitempty"`
	PayloadRaw  string          `json:"-"`
	Payload     json.RawMessage `json:"payload,omitempty"`
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
			repo          TEXT NOT NULL DEFAULT '',
			role          TEXT NOT NULL,
			assignee      TEXT NOT NULL DEFAULT '',
			last_comment_id INTEGER NOT NULL DEFAULT 0,
			current_status  TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL,
			dedup_key     TEXT NOT NULL DEFAULT '',
			bridge_id     TEXT,
			payload_json  TEXT NOT NULL DEFAULT '',
			created_at    TEXT NOT NULL,
			claimed_at    TEXT,
			finished_at   TEXT,
			heartbeat_at  TEXT,
			claimed_by    TEXT,
			last_message  TEXT,
			error_text    TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_issue_status   ON tasks(issue_number, status);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_dedup_status   ON tasks(dedup_key, status);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_role_status    ON tasks(role, status, created_at);`,
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
		`CREATE TABLE IF NOT EXISTS issue_metadata (
			issue_id   INTEGER NOT NULL,
			key        TEXT    NOT NULL,
			value      TEXT    NOT NULL,
			updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
			PRIMARY KEY (issue_id, key)
		);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	// Idempotent migrations for databases created before this schema.
	migrations := []string{
		`ALTER TABLE tasks ADD COLUMN dedup_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN repo TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN last_comment_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE tasks ADD COLUMN current_status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN bridge_id TEXT`,
	}
	for _, m := range migrations {
		_, _ = db.Exec(m) // ignore "duplicate column" errors
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// QueueTask inserts a new task in 'queued' status and returns its database ID.
func (s *Store) QueueTask(pkg WorkPackage) (int64, error) {
	dedupKey := fmt.Sprintf("issue:%d", pkg.IssueID)
	raw, _ := json.Marshal(pkg)
	res, err := s.db.Exec(
		`INSERT INTO tasks
			(issue_number, repo, role, assignee, last_comment_id, current_status,
			 status, dedup_key, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?)`,
		pkg.IssueID, pkg.Repo, pkg.Role, pkg.Assignee, pkg.LastCommentID, pkg.CurrentStatus,
		dedupKey, string(raw), nowUTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindActiveTaskByIssue returns the oldest queued or dispatched task for the issue, or nil.
func (s *Store) FindActiveTaskByIssue(issueNumber int) (*TaskRow, error) {
	dedupKey := fmt.Sprintf("issue:%d", issueNumber)
	row := s.db.QueryRow(
		`SELECT id, issue_number, COALESCE(repo,''), role, COALESCE(assignee,''),
		        COALESCE(last_comment_id,0), COALESCE(current_status,''), status,
		        dedup_key, bridge_id, created_at
		 FROM tasks
		 WHERE dedup_key = ? AND status IN ('queued','dispatched')
		 ORDER BY id ASC
		 LIMIT 1`,
		dedupKey,
	)
	var t TaskRow
	if err := row.Scan(
		&t.ID, &t.IssueNumber, &t.Repo, &t.Role, &t.Assignee,
		&t.LastCommentID, &t.CurrentStatus, &t.Status,
		&t.DedupKey, &t.BridgeID, &t.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// GetNextWorkPackage atomically selects the oldest queued task matching the given roles,
// marks it dispatched with the given bridgeID, and returns it. Returns nil if none available.
func (s *Store) GetNextWorkPackage(bridgeID string, roles []string) (*WorkPackage, error) {
	if len(roles) == 0 {
		return nil, nil
	}

	// Build IN clause placeholders.
	placeholders := make([]interface{}, len(roles))
	inClause := "("
	for i, r := range roles {
		placeholders[i] = r
		if i > 0 {
			inClause += ","
		}
		inClause += "?"
	}
	inClause += ")"

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	query := `SELECT id, COALESCE(repo,''), issue_number, role, COALESCE(assignee,''),
	                 COALESCE(last_comment_id,0), COALESCE(current_status,''),
	                 COALESCE(payload_json,'{}')
	          FROM tasks
	          WHERE status = 'queued' AND role IN ` + inClause + `
	          ORDER BY created_at ASC
	          LIMIT 1`

	row := tx.QueryRow(query, placeholders...)

	var pkg WorkPackage
	var payloadJSON string
	if err := row.Scan(
		&pkg.ID, &pkg.Repo, &pkg.IssueID, &pkg.Role,
		&pkg.Assignee, &pkg.LastCommentID, &pkg.CurrentStatus, &payloadJSON,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// Enrich the package with fields stored only in the JSON payload (e.g., WorkflowKey,
	// ValidTransitions, NextAssigneeRoles). If payload_json is absent or malformed,
	// the extra fields stay zero.
	var enriched WorkPackage
	if json.Unmarshal([]byte(payloadJSON), &enriched) == nil {
		pkg.WorkflowKey = enriched.WorkflowKey
		pkg.ValidTransitions = enriched.ValidTransitions
		pkg.NextAssigneeRoles = enriched.NextAssigneeRoles
		pkg.PastWorkers = enriched.PastWorkers
	}
	pastWorkers, err := s.listPastWorkersTx(tx, pkg.IssueID)
	if err != nil {
		return nil, err
	}
	pkg.PastWorkers = mergeUniqueStrings(pkg.PastWorkers, pastWorkers)

	_, err = tx.Exec(
		`UPDATE tasks SET status='dispatched', bridge_id=?, claimed_at=?
		 WHERE id=?`,
		bridgeID, nowUTC(), pkg.ID,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func (s *Store) ListPastWorkers(issueNumber int) ([]string, error) {
	return s.listPastWorkers(s.db, issueNumber)
}

type queryer interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

func (s *Store) listPastWorkersTx(tx *sql.Tx, issueNumber int) ([]string, error) {
	return s.listPastWorkers(tx, issueNumber)
}

func (s *Store) listPastWorkers(q queryer, issueNumber int) ([]string, error) {
	// Prefer claimed_by (GitHub-authenticated commenter) over assignee (issue-level assignment).
	rows, err := q.Query(
		`SELECT COALESCE(NULLIF(claimed_by,''), NULLIF(assignee,''))
		 FROM tasks
		 WHERE issue_number = ?
		   AND status = 'completed'
		   AND COALESCE(NULLIF(claimed_by,''), NULLIF(assignee,'')) IS NOT NULL
		 ORDER BY id ASC`,
		issueNumber,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]bool{}
	var workers []string
	for rows.Next() {
		var worker string
		if err := rows.Scan(&worker); err != nil {
			return nil, err
		}
		if !seen[worker] {
			seen[worker] = true
			workers = append(workers, worker)
		}
	}
	return workers, rows.Err()
}

// SetTaskClaimedBy records the GitHub-authenticated login of the agent who posted a
// completion comment as the confirmed worker on the most recent active or completed
// task for the issue. Prioritises dispatched tasks (agent still running) over recently
// completed ones, and never overwrites an already-set claimed_by.
func (s *Store) SetTaskClaimedBy(issueNumber int, login string) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET claimed_by = ?
		 WHERE id = (
		     SELECT id FROM tasks
		     WHERE issue_number = ?
		       AND COALESCE(claimed_by, '') = ''
		       AND status IN ('dispatched', 'completed', 'queued')
		     ORDER BY CASE status
		                WHEN 'dispatched' THEN 0
		                WHEN 'completed'  THEN 1
		                ELSE 2
		              END ASC,
		              id DESC
		     LIMIT 1
		 )`,
		login, issueNumber,
	)
	return err
}

func mergeUniqueStrings(values ...[]string) []string {
	seen := map[string]bool{}
	var merged []string
	for _, group := range values {
		for _, value := range group {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			merged = append(merged, value)
		}
	}
	return merged
}

// SupersedeQueuedTask cancels any queued (not yet dispatched) task for the given issue.
// Called when the issue transitions to a new workflow state or its assignee changes,
// so the stale queued task does not block the incoming one.
func (s *Store) SupersedeQueuedTask(issueNumber int) error {
	dedupKey := fmt.Sprintf("issue:%d", issueNumber)
	_, err := s.db.Exec(
		`UPDATE tasks SET status='superseded', finished_at=?
		 WHERE dedup_key=? AND status='queued'`,
		nowUTC(), dedupKey,
	)
	return err
}

// HasAnyTask reports whether any task (in any status) has ever been queued for
// the given issue. Used by the bootstrap guard to distinguish a genuinely new issue
// from a transient no-status webhook event mid-workflow.
func (s *Store) HasAnyTask(issueNumber int) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM tasks WHERE issue_number = ?`, issueNumber,
	).Scan(&n)
	return n > 0, err
}

// CompleteDispatchedTask marks any dispatched task for the given issue as completed.
// This is called by processIssue before queuing a new task; it is a no-op if none exist.
func (s *Store) CompleteDispatchedTask(issueNumber int) error {
	dedupKey := fmt.Sprintf("issue:%d", issueNumber)
	_, err := s.db.Exec(
		`UPDATE tasks SET status='completed', finished_at=?
		 WHERE dedup_key=? AND status='dispatched'`,
		nowUTC(), dedupKey,
	)
	return err
}

// RequeueDispatchedTask moves a bridge-claimed dispatched task back to queued
// after the bridge reports that its agent could not complete the work.
func (s *Store) RequeueDispatchedTask(taskID int64, bridgeID, errorText string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE tasks
		 SET status='queued',
		     bridge_id=NULL,
		     claimed_at=NULL,
		     last_message='agent failed; requeued',
		     error_text=?
		 WHERE id=?
		   AND status='dispatched'
		   AND bridge_id=?`,
		errorText, taskID, bridgeID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RecoverStaleTasks re-queues dispatched tasks that have not been followed by a
// webhook within staleAfterSeconds. The bridge_id is logged for diagnostics.
func (s *Store) RecoverStaleTasks(staleAfterSeconds int) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE tasks
		 SET status='queued', bridge_id=NULL, last_message='recovered stale dispatched task'
		 WHERE status='dispatched'
		   AND claimed_at IS NOT NULL
		   AND claimed_at < datetime('now', '-' || ? || ' seconds')`,
		staleAfterSeconds,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RecoverStaleTasksWithLog returns the bridge_ids of recovered tasks for logging.
func (s *Store) RecoverStaleTasksWithLog(staleAfterSeconds int) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	rows, err := tx.Query(
		`SELECT COALESCE(bridge_id, '<unknown>') FROM tasks
		 WHERE status='dispatched'
		   AND claimed_at IS NOT NULL
		   AND claimed_at < datetime('now', '-' || ? || ' seconds')`,
		staleAfterSeconds,
	)
	if err != nil {
		return nil, err
	}
	var bridges []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err == nil {
			bridges = append(bridges, b)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(bridges) > 0 {
		if _, err = tx.Exec(
			`UPDATE tasks
			 SET status='queued', bridge_id=NULL, last_message='recovered stale dispatched task'
			 WHERE status='dispatched'
			   AND claimed_at IS NOT NULL
			   AND claimed_at < datetime('now', '-' || ? || ' seconds')`,
			staleAfterSeconds,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return bridges, nil
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
	_, err := s.db.Exec(`UPDATE deliveries SET status='processed', processed_at=? WHERE delivery_id=?`, nowUTC(), id)
	return err
}

func (s *Store) MarkDeliveryFailed(id, errText string) error {
	_, err := s.db.Exec(`UPDATE deliveries SET status='failed', processed_at=?, error_text=? WHERE delivery_id=?`, nowUTC(), errText, id)
	return err
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
	issueNumber int, fromStatus, toStatus, fromAssignee, toAssignee, actor,
	triggerType string, triggerCommentID *int64, result, reason string,
	validation, metadata interface{},
) error {
	var triggerID interface{}
	if triggerCommentID != nil {
		triggerID = *triggerCommentID
	}
	validationJSON := marshalOrEmpty(validation)
	metadataJSON := marshalOrEmpty(metadata)

	_, err := s.db.Exec(
		`INSERT INTO transition_audit (
			issue_number, from_status, to_status, from_assignee, to_assignee,
			actor, trigger_type, trigger_comment_id, result, reason,
			validation_json, metadata_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		issueNumber, fromStatus, toStatus, fromAssignee, toAssignee,
		actor, triggerType, triggerID, result, reason,
		validationJSON, metadataJSON, nowUTC(),
	)
	return err
}

func (s *Store) ListTransitionAudit(issueNumber int, limit int) ([]TransitionAuditRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, issue_number, from_status, to_status, from_assignee, to_assignee,
		        actor, trigger_type, trigger_comment_id, result, reason,
		        validation_json, metadata_json, created_at
		 FROM transition_audit
		 WHERE issue_number = ?
		 ORDER BY id DESC
		 LIMIT ?`,
		issueNumber, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TransitionAuditRow
	for rows.Next() {
		var r TransitionAuditRow
		if err := rows.Scan(
			&r.ID, &r.IssueNumber, &r.FromStatus, &r.ToStatus,
			&r.FromAssignee, &r.ToAssignee, &r.Actor,
			&r.TriggerType, &r.TriggerCommentID, &r.Result, &r.Reason,
			&r.ValidationRaw, &r.MetadataRaw, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if r.ValidationRaw == "" {
			r.Validation = json.RawMessage("null")
		} else {
			r.Validation = json.RawMessage(r.ValidationRaw)
		}
		if r.MetadataRaw == "" {
			r.Metadata = json.RawMessage("null")
		} else {
			r.Metadata = json.RawMessage(r.MetadataRaw)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListTasksByIssue(issueNumber int, limit int) ([]TaskRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, issue_number, COALESCE(repo,''), role, COALESCE(assignee,''),
		        COALESCE(last_comment_id,0), COALESCE(current_status,''), status,
		        COALESCE(dedup_key,''), bridge_id, created_at,
		        claimed_at, finished_at, heartbeat_at, claimed_by, error_text,
		        COALESCE(payload_json,'')
		 FROM tasks
		 WHERE issue_number = ?
		 ORDER BY id DESC
		 LIMIT ?`,
		issueNumber, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTaskRows(rows)
}

// SetIssueMetadata inserts or replaces a single metadata key for the given issue.
func (s *Store) SetIssueMetadata(issueID int, key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO issue_metadata (issue_id, key, value, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(issue_id, key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		issueID, key, value, nowUTC(),
	)
	return err
}

// GetIssueMetadata retrieves a single metadata value. Returns ("", false, nil) when absent.
func (s *Store) GetIssueMetadata(issueID int, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(
		`SELECT value FROM issue_metadata WHERE issue_id = ? AND key = ?`,
		issueID, key,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// GetRepoForIssue returns the "owner/repo" string stored for an issue via the _repo metadata key.
// Returns ("", false, nil) when the issue has not yet been seen by a webhook.
func (s *Store) GetRepoForIssue(issueNumber int) (string, bool, error) {
	return s.GetIssueMetadata(issueNumber, "_repo")
}

// GetIssueMetadataMap returns all metadata entries for an issue as a key→value map.
func (s *Store) GetIssueMetadataMap(issueID int) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT key, value FROM issue_metadata WHERE issue_id = ?`, issueID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// ClearIssueMetadata deletes the listed metadata keys for the given issue.
func (s *Store) ClearIssueMetadata(issueID int, keys []string) error {
	for _, k := range keys {
		if _, err := s.db.Exec(
			`DELETE FROM issue_metadata WHERE issue_id = ? AND key = ?`, issueID, k,
		); err != nil {
			return err
		}
	}
	return nil
}

// SetIssueWorkflowKey persists the workflow key for an issue so subsequent
// webhook deliveries without an explicit ?workflow= param can reuse it.
func (s *Store) SetIssueWorkflowKey(issueNumber int, key string) error {
	return s.SetIssueMetadata(issueNumber, "_workflow_key", key)
}

// GetIssueWorkflowKey retrieves the persisted workflow key for an issue.
// Returns ("", false, nil) when no key has been stored.
func (s *Store) GetIssueWorkflowKey(issueNumber int) (string, bool, error) {
	return s.GetIssueMetadata(issueNumber, "_workflow_key")
}

// ListActiveTasksAllIssues returns the most recent queued or dispatched task per issue.
func (s *Store) ListActiveTasksAllIssues(limit int) ([]TaskRow, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(
		`SELECT id, issue_number, COALESCE(repo,''), role, COALESCE(assignee,''),
		        COALESCE(last_comment_id,0), COALESCE(current_status,''), status,
		        COALESCE(dedup_key,''), bridge_id, created_at,
		        claimed_at, finished_at, heartbeat_at, claimed_by, error_text,
		        COALESCE(payload_json,'')
		 FROM tasks
		 WHERE status IN ('queued','dispatched')
		 ORDER BY id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRows(rows)
}

// ListRecentTasks returns the most recently created tasks across all issues.
func (s *Store) ListRecentTasks(limit int) ([]TaskRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, issue_number, COALESCE(repo,''), role, COALESCE(assignee,''),
		        COALESCE(last_comment_id,0), COALESCE(current_status,''), status,
		        COALESCE(dedup_key,''), bridge_id, created_at,
		        claimed_at, finished_at, heartbeat_at, claimed_by, error_text,
		        COALESCE(payload_json,'')
		 FROM tasks
		 ORDER BY id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRows(rows)
}

// ListRecentAudit returns the most recent transition audit entries across all issues.
func (s *Store) ListRecentAudit(limit int) ([]TransitionAuditRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, issue_number, from_status, to_status, from_assignee, to_assignee,
		        actor, trigger_type, trigger_comment_id, result, reason,
		        validation_json, metadata_json, created_at
		 FROM transition_audit
		 ORDER BY id DESC
		 LIMIT ?`,
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
			&r.ID, &r.IssueNumber, &r.FromStatus, &r.ToStatus,
			&r.FromAssignee, &r.ToAssignee, &r.Actor,
			&r.TriggerType, &r.TriggerCommentID, &r.Result, &r.Reason,
			&r.ValidationRaw, &r.MetadataRaw, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if r.ValidationRaw == "" {
			r.Validation = json.RawMessage("null")
		} else {
			r.Validation = json.RawMessage(r.ValidationRaw)
		}
		if r.MetadataRaw == "" {
			r.Metadata = json.RawMessage("null")
		} else {
			r.Metadata = json.RawMessage(r.MetadataRaw)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanTaskRows(rows *sql.Rows) ([]TaskRow, error) {
	var out []TaskRow
	for rows.Next() {
		var t TaskRow
		if err := rows.Scan(
			&t.ID, &t.IssueNumber, &t.Repo, &t.Role, &t.Assignee,
			&t.LastCommentID, &t.CurrentStatus, &t.Status,
			&t.DedupKey, &t.BridgeID, &t.CreatedAt,
			&t.ClaimedAt, &t.FinishedAt, &t.HeartbeatAt, &t.ClaimedBy, &t.ErrorText,
			&t.PayloadRaw,
		); err != nil {
			return nil, err
		}
		if t.PayloadRaw != "" {
			t.Payload = json.RawMessage(t.PayloadRaw)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CompletedIssueSummary is a row returned by ListCompletedIssues.
type CompletedIssueSummary struct {
	IssueNumber int    `json:"issueNumber"`
	Title       string `json:"title"`
	Repo        string `json:"repo"`
	FinalStatus string `json:"finalStatus"`
	WorkflowKey string `json:"workflowKey"`
	CompletedAt string `json:"completedAt"`
	StepCount   int    `json:"stepCount"`
}

// ListCompletedIssues returns issues whose latest successful transition reached a
// terminal completion status and that have no remaining active queued/dispatched tasks.
func (s *Store) ListCompletedIssues(limit int) ([]CompletedIssueSummary, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT
			ta.issue_number,
			COALESCE(ta.to_status, '')       AS final_status,
			COALESCE(ta.created_at, '')      AS completed_at,
			COALESCE(t.repo, im_repo.value, '') AS repo,
			COALESCE(im.value, '')           AS workflow_key,
			COALESCE(sc.cnt, 0)              AS step_count,
			COALESCE(im_title.value, '')     AS title
		FROM transition_audit ta
		JOIN (
			SELECT issue_number, MAX(id) AS max_id
			FROM transition_audit
			WHERE result = 'success'
			GROUP BY issue_number
		) last_ta ON ta.issue_number = last_ta.issue_number AND ta.id = last_ta.max_id
		LEFT JOIN (
			SELECT issue_number, MAX(id) AS max_id
			FROM tasks
			GROUP BY issue_number
		) last_task ON ta.issue_number = last_task.issue_number
		LEFT JOIN tasks t
			ON t.issue_number = last_task.issue_number AND t.id = last_task.max_id
		LEFT JOIN issue_metadata im
			ON im.issue_id = ta.issue_number AND im.key = '_workflow_key'
		LEFT JOIN issue_metadata im_title
			ON im_title.issue_id = ta.issue_number AND im_title.key = '_title'
		LEFT JOIN issue_metadata im_repo
			ON im_repo.issue_id = ta.issue_number AND im_repo.key = '_repo'
		LEFT JOIN (
			SELECT issue_number, COUNT(*) AS cnt
			FROM transition_audit
			WHERE result = 'success'
			GROUP BY issue_number
		) sc ON sc.issue_number = ta.issue_number
		WHERE ta.to_status IN ('status:done', 'status:rejected')
		  AND ta.issue_number NOT IN (
			SELECT DISTINCT issue_number FROM tasks WHERE status IN ('queued', 'dispatched')
		  )
		ORDER BY ta.created_at DESC, ta.id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CompletedIssueSummary
	for rows.Next() {
		var cs CompletedIssueSummary
		if err := rows.Scan(
			&cs.IssueNumber, &cs.FinalStatus, &cs.CompletedAt,
			&cs.Repo, &cs.WorkflowKey, &cs.StepCount, &cs.Title,
		); err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	if out == nil {
		out = []CompletedIssueSummary{}
	}
	return out, rows.Err()
}

func marshalOrEmpty(v interface{}) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
