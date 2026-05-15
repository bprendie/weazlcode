package storage

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/bprendie/weazlcode/internal/coding"
)

func (s *Store) SavePlan(plan coding.Plan) error {
	if err := coding.ValidatePlan(plan); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`insert into plans (id, session_id, project_root, title, summary, status, updated_at)
		 values (?, ?, ?, ?, ?, ?, current_timestamp)
		 on conflict(id) do update set
		   session_id = excluded.session_id,
		   project_root = excluded.project_root,
		   title = excluded.title,
		   summary = excluded.summary,
		   status = excluded.status,
		   updated_at = current_timestamp`,
		plan.ID, plan.SessionID, plan.ProjectRoot, plan.Title, plan.Summary, plan.Status,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`delete from tasks where plan_id = ?`, plan.ID); err != nil {
		return err
	}
	if err := qualifyCollidingTaskIDs(tx, &plan); err != nil {
		return err
	}
	for _, task := range plan.Tasks {
		if task.PlanID == "" {
			task.PlanID = plan.ID
		}
		if err := insertTask(tx, task); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func qualifyCollidingTaskIDs(tx *sql.Tx, plan *coding.Plan) error {
	renames := map[string]string{}
	for i := range plan.Tasks {
		taskID := strings.TrimSpace(plan.Tasks[i].ID)
		if taskID == "" {
			continue
		}
		var existingPlanID string
		err := tx.QueryRow(`select plan_id from tasks where id = ? limit 1`, taskID).Scan(&existingPlanID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if existingPlanID == plan.ID {
			continue
		}
		nextID, err := uniqueTaskID(tx, plan.ID, taskID)
		if err != nil {
			return err
		}
		renames[taskID] = nextID
		plan.Tasks[i].ID = nextID
	}
	if len(renames) == 0 {
		return nil
	}
	for i := range plan.Tasks {
		for j, dep := range plan.Tasks[i].DependsOn {
			if renamed, ok := renames[strings.TrimSpace(dep)]; ok {
				plan.Tasks[i].DependsOn[j] = renamed
			}
		}
	}
	return nil
}

func uniqueTaskID(tx *sql.Tx, planID, taskID string) (string, error) {
	suffix := taskIDSuffix(planID)
	candidate := taskID + "-" + suffix
	for n := 2; ; n++ {
		var exists int
		if err := tx.QueryRow(`select 1 from tasks where id = ? limit 1`, candidate).Scan(&exists); err == sql.ErrNoRows {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
		candidate = taskID + "-" + suffix + "-" + strconv.Itoa(n)
	}
}

func taskIDSuffix(planID string) string {
	clean := strings.NewReplacer("-", "", "_", "").Replace(strings.TrimSpace(planID))
	if len(clean) > 8 {
		return clean[:8]
	}
	if clean != "" {
		return clean
	}
	return "plan"
}

func (s *Store) LatestPlan(sessionID string) (coding.Plan, bool, error) {
	row := s.db.QueryRow(
		`select id, session_id, project_root, title, summary, status, created_at, updated_at
		 from plans where session_id = ? order by updated_at desc limit 1`,
		sessionID,
	)
	plan, err := scanPlan(row)
	if err == sql.ErrNoRows {
		return coding.Plan{}, false, nil
	}
	if err != nil {
		return coding.Plan{}, false, err
	}
	tasks, err := s.Tasks(plan.ID)
	if err != nil {
		return coding.Plan{}, false, err
	}
	plan.Tasks = tasks
	return plan, true, nil
}

func (s *Store) Plans(sessionID string, limit int) ([]coding.Plan, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`select id, session_id, project_root, title, summary, status, created_at, updated_at
		 from plans where session_id = ? order by updated_at desc limit ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plans []coding.Plan
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (s *Store) Tasks(planID string) ([]coding.Task, error) {
	rows, err := s.db.Query(
		`select id, plan_id, title, goal, status, allowed_paths, forbidden_paths, context_files, skills, depends_on, verification, acceptance_checks, created_at, updated_at
		 from tasks where plan_id = ? order by created_at, id`,
		planID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []coding.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) UpdatePlanStatus(planID, status string) error {
	if !coding.ValidPlanStatus(status) {
		return codingStatusError("plan", status)
	}
	_, err := s.db.Exec(`update plans set status = ?, updated_at = current_timestamp where id = ?`, status, planID)
	return err
}

func (s *Store) UpdateTaskStatus(taskID, status string) error {
	if !coding.ValidTaskStatus(status) {
		return codingStatusError("task", status)
	}
	_, err := s.db.Exec(`update tasks set status = ?, updated_at = current_timestamp where id = ?`, status, taskID)
	return err
}

func (s *Store) AddTaskEvent(event coding.TaskEvent) (int64, error) {
	payload := string(event.Payload)
	res, err := s.db.Exec(
		`insert into task_events (task_id, type, message, payload) values (?, ?, ?, ?)`,
		event.TaskID, event.Type, event.Message, payload,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func codingStatusError(kind, status string) error {
	return &statusError{kind: kind, status: status}
}

type statusError struct {
	kind   string
	status string
}

func (e *statusError) Error() string {
	return "invalid " + e.kind + " status " + e.status
}

func (s *Store) TaskEvents(taskID string) ([]coding.TaskEvent, error) {
	rows, err := s.db.Query(`select id, task_id, type, message, payload, created_at from task_events where task_id = ? order by id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []coding.TaskEvent
	for rows.Next() {
		var event coding.TaskEvent
		var payload string
		if err := rows.Scan(&event.ID, &event.TaskID, &event.Type, &event.Message, &payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		if payload != "" {
			event.Payload = json.RawMessage(payload)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func insertTask(tx *sql.Tx, task coding.Task) error {
	if err := coding.ValidateTask(task); err != nil {
		return err
	}
	allowed, err := marshalJSON(task.AllowedPaths)
	if err != nil {
		return err
	}
	forbidden, err := marshalJSON(task.ForbiddenPaths)
	if err != nil {
		return err
	}
	contextFiles, err := marshalJSON(task.ContextFiles)
	if err != nil {
		return err
	}
	skills, err := marshalJSON(task.Skills)
	if err != nil {
		return err
	}
	dependsOn, err := marshalJSON(task.DependsOn)
	if err != nil {
		return err
	}
	verification, err := marshalJSON(task.Verification)
	if err != nil {
		return err
	}
	checks, err := marshalJSON(task.AcceptanceChecks)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`insert into tasks (id, plan_id, title, goal, status, allowed_paths, forbidden_paths, context_files, skills, depends_on, verification, acceptance_checks, updated_at)
		 values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, current_timestamp)`,
		task.ID, task.PlanID, task.Title, task.Goal, task.Status, allowed, forbidden, contextFiles, skills, dependsOn, verification, checks,
	)
	return err
}

func scanPlan(row interface {
	Scan(dest ...any) error
}) (coding.Plan, error) {
	var plan coding.Plan
	err := row.Scan(&plan.ID, &plan.SessionID, &plan.ProjectRoot, &plan.Title, &plan.Summary, &plan.Status, &plan.CreatedAt, &plan.UpdatedAt)
	return plan, err
}

func scanTask(row interface {
	Scan(dest ...any) error
}) (coding.Task, error) {
	var task coding.Task
	var allowed, forbidden, contextFiles, skills, dependsOn, verification, checks string
	err := row.Scan(&task.ID, &task.PlanID, &task.Title, &task.Goal, &task.Status, &allowed, &forbidden, &contextFiles, &skills, &dependsOn, &verification, &checks, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return task, err
	}
	if err := unmarshalJSON(allowed, &task.AllowedPaths); err != nil {
		return task, err
	}
	if err := unmarshalJSON(forbidden, &task.ForbiddenPaths); err != nil {
		return task, err
	}
	if err := unmarshalJSON(contextFiles, &task.ContextFiles); err != nil {
		return task, err
	}
	if err := unmarshalJSON(skills, &task.Skills); err != nil {
		return task, err
	}
	if err := unmarshalJSON(dependsOn, &task.DependsOn); err != nil {
		return task, err
	}
	if err := unmarshalJSON(verification, &task.Verification); err != nil {
		return task, err
	}
	if err := unmarshalJSON(checks, &task.AcceptanceChecks); err != nil {
		return task, err
	}
	return task, nil
}

func marshalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalJSON(raw string, dst any) error {
	if raw == "" {
		raw = "[]"
	}
	return json.Unmarshal([]byte(raw), dst)
}
