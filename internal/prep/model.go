package prep

import "time"

type Kind string

const (
	KindOnePerson Kind = "one_person"
	KindEveryone  Kind = "everyone"
)

type Person struct {
	UserID      int64   `json:"user_id"`
	DisplayName string  `json:"display_name"`
	Email       *string `json:"email"`
}

type Activity struct {
	ID       int64     `json:"id"`
	Title    string    `json:"title"`
	StartsAt time.Time `json:"starts_at"`
}

// Task reports state computed at read time. Assignee is null when an unfinished
// task's assignee no longer takes part, and the ready and waiting lists cover
// the people who currently take part in an everyone task.
type Task struct {
	ID              int64      `json:"id"`
	TripID          int64      `json:"trip_id"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	Title           string     `json:"title"`
	Kind            Kind       `json:"kind"`
	Activity        *Activity  `json:"activity"`
	Date            *string    `json:"date"`
	Assignee        *Person    `json:"assignee"`
	CompletedAt     *time.Time `json:"completed_at"`
	ReadyUserIDs    []int64    `json:"ready_user_ids"`
	WaitingUserIDs  []int64    `json:"waiting_user_ids"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CreateInput struct {
	Title          string  `json:"title"`
	Kind           Kind    `json:"kind"`
	AssigneeUserID *int64  `json:"assignee_user_id"`
	ActivityID     *int64  `json:"activity_id"`
	Date           *string `json:"date"`
}

type UpdateInput struct {
	Title      string  `json:"title"`
	ActivityID *int64  `json:"activity_id"`
	Date       *string `json:"date"`
}

type AssigneeInput struct {
	UserID *int64 `json:"user_id"`
}
