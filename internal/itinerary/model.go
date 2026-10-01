package itinerary

import "time"

type Input struct {
	Title    string    `json:"title"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	TimeZone string    `json:"time_zone"`
	Notes    string    `json:"notes"`
}

type Activity struct {
	ID              int64 `json:"id"`
	TripID          int64 `json:"trip_id"`
	CreatedByUserID int64 `json:"created_by_user_id"`
	Input
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Conflict struct {
	ActivityIDs     [2]int64  `json:"activity_ids"`
	OverlapStartsAt time.Time `json:"overlap_starts_at"`
	OverlapEndsAt   time.Time `json:"overlap_ends_at"`
}
