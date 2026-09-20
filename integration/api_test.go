package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/database"
	"travel-planner/travel-planner-api/internal/itinerary"
	"travel-planner/travel-planner-api/internal/server"
	"travel-planner/travel-planner-api/internal/trips"
)

// Uses only TEST_DATABASE_URL and removes only the trips created by this test.
func TestAPILifecycle(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated database ending in _test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var name string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, "_test") {
		t.Fatal("integration database name must end in _test")
	}

	subject := fmt.Sprintf("integration|%d", time.Now().UnixNano())
	var ownerID, otherID int64
	if err := pool.QueryRow(ctx, "INSERT INTO users (auth_subject) VALUES ($1) RETURNING id", subject+"|owner").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO users (auth_subject) VALUES ($1) RETURNING id", subject+"|other").Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupPool, err := database.Open(context.Background(), url)
		if err != nil {
			t.Error(err)
			return
		}
		defer cleanupPool.Close()
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := cleanupPool.Exec(cctx, "DELETE FROM users WHERE id=$1 OR id=$2", ownerID, otherID); err != nil {
			t.Error(err)
		}
	})

	testAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.Header.Get("X-Test-User-ID"), 10, 64)
			if err != nil || (id != ownerID && id != otherID) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), auth.User{ID: id})))
		})
	}
	handler := server.Router(trips.NewService(trips.NewRepository(pool)), itinerary.NewService(itinerary.NewRepository(pool)), testAuth)
	requestAs := func(userID int64, method, path string, body any, status int, target any) {
		t.Helper()
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User-ID", strconv.FormatInt(userID, 10))
		handler.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		if target != nil {
			if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
				t.Fatal(err)
			}
		}
	}
	request := func(method, path string, body any, status int, target any) {
		requestAs(ownerID, method, path, body, status, target)
	}
	cleanup := func(id int64) {
		t.Helper()
		t.Cleanup(func() {
			cleanupPool, err := database.Open(context.Background(), url)
			if err != nil {
				t.Error(err)
				return
			}
			defer cleanupPool.Close()
			cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := cleanupPool.Exec(cctx, "DELETE FROM trips WHERE id=$1", id); err != nil {
				t.Error(err)
			}
		})
	}

	input := trips.Input{Name: "Cebu integration trip", Destination: "Cebu", StartDate: "2026-10-01", EndDate: "2026-10-03", TimeZone: "Asia/Manila"}
	var trip trips.Trip
	request("POST", "/api/v1/trips", input, 201, &trip)
	cleanup(trip.ID)
	var storedOwnerID int64
	if err := pool.QueryRow(ctx, "SELECT owner_user_id FROM trips WHERE id=$1", trip.ID).Scan(&storedOwnerID); err != nil || storedOwnerID != ownerID {
		t.Fatalf("trip owner = %d, want %d: %v", storedOwnerID, ownerID, err)
	}
	request("POST", "/api/v1/trips", map[string]any{"owner_user_id": otherID}, 400, nil)
	base := fmt.Sprintf("/api/v1/trips/%d", trip.ID)
	request("GET", base, nil, 200, &trip)
	if trip.StartDate != input.StartDate || trip.TimeZone != input.TimeZone {
		t.Fatal("trip fields did not round-trip")
	}
	var tripList []trips.Trip
	request("GET", "/api/v1/trips?limit=1", nil, 200, &tripList)
	if len(tripList) != 1 {
		t.Fatal("trip pagination failed")
	}
	input.Name = "Updated Cebu trip"
	request("PUT", base, input, 200, &trip)
	if trip.Name != input.Name {
		t.Fatal("trip update not persisted")
	}

	var empty []itinerary.Activity
	request("GET", base+"/activities", nil, 200, &empty)
	if empty == nil || len(empty) != 0 {
		t.Fatal("empty list must be []")
	}
	start, _ := time.Parse(time.RFC3339, "2026-10-01T09:00:00+08:00")
	activityInput := itinerary.Input{Title: "Breakfast", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Asia/Manila", Notes: "Near hotel"}
	var activity itinerary.Activity
	request("POST", base+"/activities", activityInput, 201, &activity)
	if activity.StartsAt.Location() != time.UTC || activity.StartsAt.Hour() != 1 {
		t.Fatal("timestamps not returned as UTC")
	}
	activityPath := fmt.Sprintf("%s/activities/%d", base, activity.ID)
	request("GET", activityPath, nil, 200, &activity)
	activityInput.Title = "Late breakfast"
	request("PUT", activityPath, activityInput, 200, &activity)
	if activity.Title != activityInput.Title {
		t.Fatal("activity update not persisted")
	}

	var other trips.Trip
	requestAs(otherID, "POST", "/api/v1/trips", input, 201, &other)
	cleanup(other.ID)
	requestAs(otherID, "GET", base, nil, 404, nil)
	requestAs(otherID, "PUT", base, input, 404, nil)
	requestAs(otherID, "DELETE", base, nil, 404, nil)
	request("GET", base, nil, 200, nil)
	var ownerTrips, otherTrips []trips.Trip
	request("GET", "/api/v1/trips?limit=1", nil, 200, &ownerTrips)
	requestAs(otherID, "GET", "/api/v1/trips?limit=1", nil, 200, &otherTrips)
	if len(ownerTrips) != 1 || ownerTrips[0].ID != trip.ID || len(otherTrips) != 1 || otherTrips[0].ID != other.ID {
		t.Fatal("trip list was not filtered by owner before pagination")
	}
	wrongPath := fmt.Sprintf("/api/v1/trips/%d/activities/%d", other.ID, activity.ID)
	request("GET", wrongPath, nil, 404, nil)
	request("PUT", wrongPath, activityInput, 404, nil)
	request("DELETE", wrongPath, nil, 404, nil)
	request("GET", activityPath, nil, 200, nil)

	activityInput.StartsAt = start.Add(-time.Hour)
	activityInput.EndsAt = start
	request("POST", base+"/activities", activityInput, 201, nil)
	var activities []itinerary.Activity
	request("GET", base+"/activities?limit=1", nil, 200, &activities)
	if len(activities) != 1 || !activities[0].StartsAt.Equal(activityInput.StartsAt) {
		t.Fatal("schedule order or pagination incorrect")
	}
	request("DELETE", activityPath, nil, 204, nil)
	request("GET", activityPath, nil, 404, nil)
	request("DELETE", base, nil, 204, nil)
	request("GET", base, nil, 404, nil)
	request("PUT", base, input, 404, nil)
	request("GET", base+"/activities", nil, 404, nil)
	request("POST", base+"/activities", activityInput, 404, nil)
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM itinerary_items WHERE trip_id=$1", trip.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("trip deletion did not cascade")
	}
}
