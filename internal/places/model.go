package places

import "time"

type Candidate struct {
	ProviderPlaceID string  `json:"provider_place_id"`
	Name            string  `json:"name"`
	Address         string  `json:"address"`
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	TimeZone        string  `json:"time_zone"`
}

type Place struct {
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	Candidate
	RefreshedAt time.Time `json:"refreshed_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
