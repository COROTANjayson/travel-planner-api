package trips

import "time"

type Input struct {
	Name               string     `json:"name"`
	Destination        string     `json:"destination"`
	StartDate          string     `json:"start_date"`
	EndDate            string     `json:"end_date"`
	TimeZone           string     `json:"time_zone"`
	DestinationPlaceID *int64     `json:"destination_place_id"`
	StartsAt           *time.Time `json:"starts_at"`
	EndsAt             *time.Time `json:"ends_at"`
}

type Destination struct{ Name, Kind, TimeZone string }

type Trip struct {
	ID int64 `json:"id"`
	Input
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
