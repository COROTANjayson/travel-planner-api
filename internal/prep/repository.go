package prep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/apperror"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// takingPart lists who takes part in Prep on a trip: the owner and non-viewer
// members. Counts and assignees are computed from it on every read, so removals,
// role changes and transfers need no Prep writes. trip is always a constant here.
func takingPart(trip string) string {
	return `(SELECT owner_user_id AS user_id FROM trips WHERE id=` + trip + `
		UNION SELECT user_id FROM trip_members WHERE trip_id=` + trip + ` AND role<>'viewer')`
}

var selectTask = `
	SELECT t.id,t.trip_id,t.created_by_user_id,t.title,t.kind,
		CASE WHEN a.id IS NULL THEN NULL ELSE jsonb_build_object('user_id',a.id,'display_name',a.display_name,'email',a.email) END,
		t.completed_at,
		CASE WHEN t.kind='everyone' THEN COALESCE((
			SELECT jsonb_agg(p.user_id ORDER BY p.user_id) FROM ` + takingPart("t.trip_id") + ` p
			WHERE EXISTS (SELECT 1 FROM prep_task_ticks k WHERE k.task_id=t.id AND k.user_id=p.user_id)), '[]'::jsonb)
			ELSE '[]'::jsonb END,
		CASE WHEN t.kind='everyone' THEN COALESCE((
			SELECT jsonb_agg(p.user_id ORDER BY p.user_id) FROM ` + takingPart("t.trip_id") + ` p
			WHERE NOT EXISTS (SELECT 1 FROM prep_task_ticks k WHERE k.task_id=t.id AND k.user_id=p.user_id)), '[]'::jsonb)
			ELSE '[]'::jsonb END,
		CASE WHEN i.id IS NULL THEN NULL ELSE jsonb_build_object('id',i.id,'title',i.title,'starts_at',i.starts_at) END,
		t.task_date::text,
		t.created_at,t.updated_at
	FROM prep_tasks t
	LEFT JOIN itinerary_items i ON i.id=t.activity_id
	LEFT JOIN users a ON a.id=t.assignee_user_id
		AND (t.completed_at IS NOT NULL OR a.id IN ` + takingPart("t.trip_id") + `)`

func scan(row pgx.Row) (Task, error) {
	var task Task
	var assignee, ready, waiting, activity []byte
	err := row.Scan(&task.ID, &task.TripID, &task.CreatedByUserID, &task.Title, &task.Kind, &assignee, &task.CompletedAt, &ready, &waiting, &activity, &task.Date, &task.CreatedAt, &task.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return task, apperror.ErrNotFound
	}
	if err != nil {
		return task, err
	}
	if assignee != nil {
		if err := json.Unmarshal(assignee, &task.Assignee); err != nil {
			return task, err
		}
	}
	if activity != nil {
		if err := json.Unmarshal(activity, &task.Activity); err != nil {
			return task, err
		}
		task.Activity.StartsAt = task.Activity.StartsAt.UTC()
	}
	if err := json.Unmarshal(ready, &task.ReadyUserIDs); err != nil {
		return task, err
	}
	if err := json.Unmarshal(waiting, &task.WaitingUserIDs); err != nil {
		return task, err
	}
	if task.CompletedAt != nil {
		completedAt := task.CompletedAt.UTC()
		task.CompletedAt = &completedAt
	}
	task.CreatedAt, task.UpdatedAt = task.CreatedAt.UTC(), task.UpdatedAt.UTC()
	return task, nil
}

func (r *Repository) get(ctx context.Context, tripID, id int64) (Task, error) {
	return scan(r.pool.QueryRow(ctx, selectTask+` WHERE t.trip_id=$1 AND t.id=$2`, tripID, id))
}

// ponytail: returns every task unpaginated because only editors add them; add limit/offset if a trip ever reaches hundreds.
func (r *Repository) List(ctx context.Context, tripID int64) ([]Task, error) {
	rows, err := r.pool.Query(ctx, selectTask+` WHERE t.trip_id=$1 ORDER BY t.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		task, err := scan(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (r *Repository) Create(ctx context.Context, userID, tripID int64, in CreateInput) (Task, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO prep_tasks (trip_id,created_by_user_id,title,kind,assignee_user_id,activity_id,task_date)
		SELECT $1::bigint,$2::bigint,$3::text,$4::text,$5::bigint,$6::bigint,$7::text::date
		WHERE ($5::bigint IS NULL OR $5::bigint IN `+takingPart("$1::bigint")+`)
			AND ($6::bigint IS NULL OR EXISTS (SELECT 1 FROM itinerary_items WHERE id=$6::bigint AND trip_id=$1::bigint))
			AND ($7::text::date IS NULL OR EXISTS (SELECT 1 FROM trips WHERE id=$1::bigint AND $7::text::date BETWEEN start_date AND end_date))
		RETURNING id`, tripID, userID, in.Title, in.Kind, in.AssigneeUserID, in.ActivityID, in.Date).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, fmt.Errorf("%w: assignee must be on this trip and not a viewer, and activity and date must fall within this trip", apperror.ErrInvalid)
	}
	if err != nil {
		return Task{}, foreignKeyNotFound(err)
	}
	return r.get(ctx, tripID, id)
}

// Update accepts an unchanged date even if the trip has since moved away from it,
// so a stale date doesn't block other edits. A new date must be one of the trip's days.
func (r *Repository) Update(ctx context.Context, tripID, id int64, in UpdateInput) (Task, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE prep_tasks SET title=$3,activity_id=$4,task_date=$5::text::date,updated_at=now()
		WHERE trip_id=$1 AND id=$2
			AND ($4::bigint IS NULL OR EXISTS (SELECT 1 FROM itinerary_items WHERE id=$4::bigint AND trip_id=$1))
			AND ($5::text::date IS NULL OR task_date=$5::text::date
				OR EXISTS (SELECT 1 FROM trips x WHERE x.id=$1 AND $5::text::date BETWEEN x.start_date AND x.end_date))`, tripID, id, in.Title, in.ActivityID, in.Date)
	if err != nil {
		return Task{}, foreignKeyNotFound(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.state(ctx, tripID, id); err != nil {
			return Task{}, err
		}
		return Task{}, fmt.Errorf("%w: activity and date must fall within this trip", apperror.ErrInvalid)
	}
	return r.get(ctx, tripID, id)
}

func (r *Repository) Delete(ctx context.Context, tripID, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM prep_tasks WHERE trip_id=$1 AND id=$2`, tripID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

func (r *Repository) Assign(ctx context.Context, tripID, id int64, assignee *int64) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE prep_tasks t SET assignee_user_id=$3,updated_at=now()
		WHERE t.trip_id=$1 AND t.id=$2 AND t.kind='one_person' AND t.completed_at IS NULL
			AND ($3::bigint IS NULL OR $3::bigint IN `+takingPart("$1")+`)`, tripID, id, assignee)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	state, err := r.state(ctx, tripID, id)
	if err != nil {
		return err
	}
	switch {
	case state.kind != KindOnePerson:
		return notOnePerson()
	case state.completed:
		return finished()
	}
	return ineligibleAssignee()
}

// Claim treats a task whose assignee no longer takes part as open, or it could
// never be claimed again after they leave.
func (r *Repository) Claim(ctx context.Context, userID, tripID, id int64) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE prep_tasks t SET assignee_user_id=$3,updated_at=now()
		WHERE t.trip_id=$1 AND t.id=$2 AND t.kind='one_person' AND t.completed_at IS NULL
			AND (t.assignee_user_id IS NULL OR t.assignee_user_id NOT IN `+takingPart("$1")+`)`, tripID, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	state, err := r.state(ctx, tripID, id)
	if err != nil {
		return err
	}
	if state.kind != KindOnePerson {
		return notOnePerson()
	}
	if state.assignee != nil && *state.assignee == userID {
		return nil
	}
	return fmt.Errorf("%w: this task is already taken", apperror.ErrConflict)
}

func (r *Repository) Drop(ctx context.Context, userID, tripID, id int64) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE prep_tasks SET assignee_user_id=NULL,updated_at=now()
		WHERE trip_id=$1 AND id=$2 AND kind='one_person' AND completed_at IS NULL AND assignee_user_id=$3`, tripID, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	state, err := r.state(ctx, tripID, id)
	if err != nil {
		return err
	}
	if state.kind != KindOnePerson {
		return notOnePerson()
	}
	return fmt.Errorf("%w: only your own unfinished task can be dropped", apperror.ErrConflict)
}

func (r *Repository) Complete(ctx context.Context, userID, tripID, id int64) error {
	state, err := r.state(ctx, tripID, id)
	if err != nil {
		return err
	}
	if state.kind == KindEveryone {
		_, err := r.pool.Exec(ctx, `INSERT INTO prep_task_ticks (task_id,user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, userID)
		return foreignKeyNotFound(err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE prep_tasks SET completed_at=now(),updated_at=now()
		WHERE trip_id=$1 AND id=$2 AND assignee_user_id=$3 AND completed_at IS NULL`, tripID, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	state, err = r.state(ctx, tripID, id)
	if err != nil {
		return err
	}
	if state.completed && state.assignee != nil && *state.assignee == userID {
		return nil
	}
	return notYours()
}

func (r *Repository) Reopen(ctx context.Context, userID, tripID, id int64, anyAssignee bool) error {
	state, err := r.state(ctx, tripID, id)
	if err != nil {
		return err
	}
	if state.kind == KindEveryone {
		_, err := r.pool.Exec(ctx, `DELETE FROM prep_task_ticks WHERE task_id=$1 AND user_id=$2`, id, userID)
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE prep_tasks SET completed_at=NULL,updated_at=now()
		WHERE trip_id=$1 AND id=$2 AND completed_at IS NOT NULL AND (assignee_user_id=$3 OR $4)`, tripID, id, userID, anyAssignee)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	state, err = r.state(ctx, tripID, id)
	if err != nil {
		return err
	}
	if !state.completed {
		return nil
	}
	return notYours()
}

type taskState struct {
	kind      Kind
	assignee  *int64
	completed bool
}

// state reads a task's raw columns to explain why a conditional write changed
// nothing, or to choose the write for its kind. A missing task is ErrNotFound.
func (r *Repository) state(ctx context.Context, tripID, id int64) (taskState, error) {
	var state taskState
	err := r.pool.QueryRow(ctx, `SELECT kind,assignee_user_id,completed_at IS NOT NULL FROM prep_tasks WHERE trip_id=$1 AND id=$2`, tripID, id).Scan(&state.kind, &state.assignee, &state.completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, apperror.ErrNotFound
	}
	return state, err
}

// foreignKeyNotFound maps a task or trip deleted mid-request to its usual 404.
func foreignKeyNotFound(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return apperror.ErrNotFound
	}
	return err
}

func ineligibleAssignee() error {
	return fmt.Errorf("%w: assignee must be on this trip and not a viewer", apperror.ErrInvalid)
}

func notOnePerson() error {
	return fmt.Errorf("%w: only one-person tasks have an assignee", apperror.ErrInvalid)
}

func finished() error {
	return fmt.Errorf("%w: reopen this task before reassigning it", apperror.ErrConflict)
}

func notYours() error {
	return fmt.Errorf("%w: this task is assigned to someone else", apperror.ErrConflict)
}
