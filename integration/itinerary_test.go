package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tzf "github.com/ringsaturn/tzf/v2"
	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/database"
	"travel-planner/travel-planner-api/internal/itinerary"
	"travel-planner/travel-planner-api/internal/memberships"
	"travel-planner/travel-planner-api/internal/places"
	"travel-planner/travel-planner-api/internal/server"
	"travel-planner/travel-planner-api/internal/trips"
)

func TestItineraryPermissionsScheduleAndCreators(t *testing.T) {
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
	var databaseName string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&databaseName); err != nil || !strings.HasSuffix(databaseName, "_test") {
		t.Fatalf("integration database must end in _test: %q, %v", databaseName, err)
	}

	prefix := fmt.Sprintf("itinerary|%d|", time.Now().UnixNano())
	users := map[int64]bool{}
	addUser := func(name string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, "INSERT INTO users (auth_subject) VALUES ($1) RETURNING id", prefix+name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		users[id] = true
		return id
	}
	owner, editor, member, viewer, outsider := addUser("owner"), addUser("editor"), addUser("member"), addUser("viewer"), addUser("outsider")
	var tripIDs []int64
	var placeIDs []int64
	t.Cleanup(func() {
		cleanupPool, err := database.Open(context.Background(), url)
		if err != nil {
			t.Error(err)
			return
		}
		defer cleanupPool.Close()
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, id := range tripIDs {
			if _, err := cleanupPool.Exec(cleanupCtx, "DELETE FROM trips WHERE id=$1", id); err != nil {
				t.Error(err)
			}
		}
		for _, id := range placeIDs {
			if _, err := cleanupPool.Exec(cleanupCtx, "DELETE FROM places WHERE id=$1", id); err != nil {
				t.Error(err)
			}
		}
		if _, err := cleanupPool.Exec(cleanupCtx, "DELETE FROM place_search_cache WHERE query IN ('manila','cebu')"); err != nil {
			t.Error(err)
		}
		if _, err := cleanupPool.Exec(cleanupCtx, "DELETE FROM users WHERE auth_subject LIKE $1", prefix+"%"); err != nil {
			t.Error(err)
		}
	})
	identity := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.ParseInt(r.Header.Get("X-Test-User-ID"), 10, 64)
			if !users[id] {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), auth.User{ID: id})))
		})
	}
	authz := memberships.NewService(memberships.NewRepository(pool))
	var providerCalls atomic.Int64
	var lastProviderCall atomic.Int64
	var providerUnavailable atomic.Bool
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		arrived := time.Now().UnixNano()
		if previous := lastProviderCall.Swap(arrived); previous != 0 && arrived-previous < int64(900*time.Millisecond) {
			t.Error("outbound provider calls exceeded one request per second")
		}
		if !strings.Contains(r.Header.Get("User-Agent"), "TravelPlannerPortfolio") {
			t.Error("provider request lacks app identification")
		}
		if providerUnavailable.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[{"osm_type":"node","osm_id":987654321,"display_name":"City Hall, Manila, Philippines","lat":"14.5946","lon":"120.978","namedetails":{"name":"City Hall"}}]`))
	}))
	defer providerServer.Close()
	finder, err := tzf.NewEmbeddedFinder()
	if err != nil {
		t.Fatal(err)
	}
	placeService := places.NewService(places.NewRepository(pool), places.NewNominatim(providerServer.URL, &http.Client{Timeout: 5 * time.Second}), finder.GetTimezoneName)
	handler := server.Router(trips.NewService(trips.NewRepository(pool), authz), itinerary.NewService(itinerary.NewRepository(pool), authz), authz, identity, placeService)
	request := func(userID int64, method, path string, body any, want int, target any) *httptest.ResponseRecorder {
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
		r := httptest.NewRequest(method, path, bytes.NewReader(payload)).WithContext(ctx)
		r.Header.Set("X-Test-User-ID", strconv.FormatInt(userID, 10))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("user %d: %s %s = %d, want %d: %s", userID, method, path, w.Code, want, w.Body.String())
		}
		if target != nil && want < 300 {
			if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
				t.Fatal(err)
			}
		}
		return w
	}
	destinationID := destinationFixture(t, ctx, pool, url)
	tripStart := time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC) // October 1, 02:00 in Manila.
	tripEnd := time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC)   // October 3, 23:00 in Manila.
	tripInput := trips.Input{Name: "Itinerary test", DestinationPlaceID: &destinationID, StartsAt: &tripStart, EndsAt: &tripEnd}
	var trip, other trips.Trip
	request(owner, "POST", "/api/v1/trips", tripInput, 201, &trip)
	tripIDs = append(tripIDs, trip.ID)
	request(owner, "POST", "/api/v1/trips", tripInput, 201, &other)
	tripIDs = append(tripIDs, other.ID)
	for _, participant := range []struct {
		id   int64
		role string
	}{{editor, "editor"}, {member, "member"}, {viewer, "viewer"}} {
		if _, err := pool.Exec(ctx, "INSERT INTO trip_members (trip_id,user_id,role) VALUES ($1,$2,$3)", trip.ID, participant.id, participant.role); err != nil {
			t.Fatal(err)
		}
	}
	base := fmt.Sprintf("/api/v1/trips/%d/activities", trip.ID)
	otherBase := fmt.Sprintf("/api/v1/trips/%d/activities", other.ID)
	start := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	in := itinerary.Input{Title: "Breakfast", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Asia/Manila", Notes: "Original"}
	var original itinerary.Activity
	request(owner, "POST", base, in, 201, &original)
	path := fmt.Sprintf("%s/%d", base, original.ID)
	if original.Category == nil || *original.Category != "Activity" || original.Checklist == nil || *original.Checklist == nil || len(*original.Checklist) != 0 {
		t.Fatalf("legacy creation did not receive defaults: %+v", original)
	}
	category := "  Shopping  "
	checklist := []itinerary.ChecklistItem{{Text: "  Buy tickets  ", Done: true}, {Text: "Buy tickets"}}
	categorized := in
	categorized.Category, categorized.Checklist = &category, &checklist
	request(viewer, "PUT", path, categorized, 403, nil)
	request(member, "PUT", path, categorized, 403, nil)
	request(outsider, "PUT", path, categorized, 404, nil)
	var saved itinerary.Activity
	request(editor, "PUT", path, categorized, 200, &saved)
	wantChecklist := []itinerary.ChecklistItem{{Text: "Buy tickets", Done: true}, {Text: "Buy tickets"}}
	assertChecklist := func(activity itinerary.Activity) {
		t.Helper()
		if activity.Category == nil || *activity.Category != "Shopping" || activity.Checklist == nil || !reflect.DeepEqual(*activity.Checklist, wantChecklist) {
			t.Fatalf("category/checklist lost: %+v", activity)
		}
	}
	assertChecklist(saved)
	var customCreated itinerary.Activity
	request(owner, "POST", base, categorized, 201, &customCreated)
	assertChecklist(customCreated)
	request(owner, "DELETE", fmt.Sprintf("%s/%d", base, customCreated.ID), nil, 204, nil)
	request(viewer, "GET", path, nil, 200, &saved)
	assertChecklist(saved)
	var listed []itinerary.Activity
	request(viewer, "GET", base, nil, 200, &listed)
	assertChecklist(listed[0])
	// Old full-replacement clients cannot erase additions they do not send.
	request(owner, "PUT", path, in, 200, &saved)
	assertChecklist(saved)
	legacy := map[string]any{"title": in.Title, "starts_at": in.StartsAt, "ends_at": in.EndsAt, "notes": in.Notes, "category": nil, "checklist": nil}
	request(owner, "PUT", path, legacy, 200, &saved)
	assertChecklist(saved)
	for _, invalid := range []map[string]any{
		{"category": " "}, {"category": strings.Repeat("x", 101)},
		{"checklist": []any{map[string]any{"text": " "}}},
		{"checklist": []any{map[string]any{"text": "Item", "done": "true"}}},
		{"checklist": []any{map[string]any{"text": "Item", "extra": true}}},
		{"checklist": "not an array"}, {"checklist": []any{nil}},
	} {
		body := map[string]any{"title": in.Title, "starts_at": in.StartsAt, "ends_at": in.EndsAt}
		for key, value := range invalid {
			body[key] = value
		}
		request(owner, "POST", base, body, 400, nil)
		request(owner, "PUT", path, body, 400, nil)
	}
	request(viewer, "GET", path, nil, 200, &saved)
	assertChecklist(saved)
	legacy["category"], legacy["checklist"] = "Activity", []itinerary.ChecklistItem{}
	request(owner, "PUT", path, legacy, 200, &original)
	if *original.Category != "Activity" || original.Checklist == nil || *original.Checklist == nil || len(*original.Checklist) != 0 {
		t.Fatal("explicit empty checklist did not clear saved steps")
	}
	rangeURL := fmt.Sprintf("%s?start_from=%s&start_before=%s", base, start.Format(time.RFC3339), start.Add(time.Minute).Format(time.RFC3339))
	var ranged []itinerary.Activity
	request(owner, "GET", rangeURL, nil, 200, &ranged)
	if len(ranged) != 1 || ranged[0].ID != original.ID {
		t.Fatalf("date range = %+v", ranged)
	}
	request(owner, "GET", rangeURL+"&limit=1&offset=1", nil, 200, &ranged)
	if len(ranged) != 0 {
		t.Fatal("date range pagination must follow filtering")
	}
	request(owner, "GET", base+"?start_from=bad&start_before=bad", nil, 400, nil)
	request(owner, "GET", fmt.Sprintf("%s?start_from=%s&start_before=%s", base, start.Add(time.Minute).Format(time.RFC3339), start.Format(time.RFC3339)), nil, 400, nil)
	for _, invalid := range []itinerary.Input{
		{Title: "Too early", StartsAt: tripStart.Add(-time.Minute), EndsAt: tripStart.Add(time.Hour)},
		{Title: "Too late", StartsAt: tripEnd.Add(-time.Hour), EndsAt: tripEnd.Add(time.Minute)},
	} {
		request(owner, "POST", base, invalid, 400, nil)
	}
	var overlapping []itinerary.Activity
	query := fmt.Sprintf("%s/overlaps?starts_at=%s&ends_at=%s", base, start.Add(30*time.Minute).Format(time.RFC3339), start.Add(90*time.Minute).Format(time.RFC3339))
	request(viewer, "GET", query, nil, 200, &overlapping)
	if len(overlapping) != 1 || overlapping[0].ID != original.ID {
		t.Fatalf("overlaps = %+v", overlapping)
	}
	request(owner, "GET", query+fmt.Sprintf("&exclude_id=%d", original.ID), nil, 200, &overlapping)
	if len(overlapping) != 0 {
		t.Fatal("edited activity overlapped itself")
	}
	query = fmt.Sprintf("%s/overlaps?starts_at=%s&ends_at=%s", base, start.Add(time.Hour).Format(time.RFC3339), start.Add(2*time.Hour).Format(time.RFC3339))
	request(owner, "GET", query, nil, 200, &overlapping)
	if len(overlapping) != 0 {
		t.Fatal("touching endpoints must not overlap")
	}
	request(owner, "GET", base+"/overlaps?starts_at=bad", nil, 400, nil)
	for _, tc := range []struct {
		name        string
		id          int64
		read, write int
	}{
		{"owner", owner, 200, 201}, {"editor", editor, 200, 201},
		{"member", member, 200, 403}, {"viewer", viewer, 200, 403},
		{"outsider", outsider, 404, 404}, {"anonymous", 0, 401, 401},
	} {
		t.Logf("checking %s permissions", tc.name)
		request(tc.id, "GET", base, nil, tc.read, nil)
		request(tc.id, "GET", path, nil, tc.read, nil)
		request(tc.id, "GET", base+"/conflicts", nil, tc.read, nil)
		var created itinerary.Activity
		request(tc.id, "POST", base, in, tc.write, &created)
		update, deletion := tc.write, tc.write
		deletePath := path
		if tc.write == 201 {
			update, deletion = 200, 204
			deletePath = fmt.Sprintf("%s/%d", base, created.ID)
			if created.CreatedByUserID != tc.id {
				t.Fatal("creator is not authenticated caller")
			}
		}
		var updated itinerary.Activity
		request(tc.id, "PUT", path, in, update, &updated)
		if update == 200 && updated.CreatedByUserID != owner {
			t.Fatal("PUT reassigned creator")
		}
		request(tc.id, "DELETE", deletePath, nil, deletion, nil)
	}
	spoof := map[string]any{"title": in.Title, "starts_at": in.StartsAt, "ends_at": in.EndsAt, "time_zone": in.TimeZone, "created_by_user_id": editor}
	request(owner, "POST", base, spoof, 400, nil)
	request(owner, "PUT", path, spoof, 400, nil)
	for _, query := range []string{"?limit=0", "?limit=101", "?offset=-1", "?limit=bad"} {
		request(owner, "GET", base+"/conflicts"+query, nil, 400, nil)
	}
	request(owner, "GET", "/api/v1/trips/invalid/activities/conflicts", nil, 400, nil)
	wrongPath := fmt.Sprintf("%s/%d", otherBase, original.ID)
	request(owner, "GET", wrongPath, nil, 404, nil)
	request(owner, "PUT", wrongPath, in, 404, nil)
	request(owner, "DELETE", wrongPath, nil, 404, nil)
	request(owner, "GET", base+"/999999999999", nil, 404, nil)
	request(owner, "PUT", base+"/999999999999", in, 404, nil)
	request(owner, "DELETE", base+"/999999999999", nil, 404, nil)
	request(owner, "GET", "/api/v1/trips/999999999999/activities/conflicts", nil, 404, nil)
	request(owner, "GET", path, nil, 200, &original)
	for _, invalid := range []itinerary.Input{
		{Title: "Before", StartsAt: start.Add(-24 * time.Hour), EndsAt: start, TimeZone: "Asia/Manila"},
		{Title: "After", StartsAt: start, EndsAt: start.Add(72 * time.Hour), TimeZone: "Asia/Manila"},
	} {
		request(owner, "POST", base, invalid, 400, nil)
		request(owner, "PUT", path, invalid, 400, nil)
	}
	var unchanged itinerary.Activity
	request(owner, "GET", path, nil, 200, &unchanged)
	if !reflect.DeepEqual(unchanged, original) {
		t.Fatal("failed replacement changed activity")
	}
	var activities []itinerary.Activity
	request(owner, "GET", base, nil, 200, &activities)
	if len(activities) != 1 {
		t.Fatal("failed creation wrote an activity")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM place_search_cache WHERE query IN ('manila','cebu')"); err != nil {
		t.Fatal(err)
	}
	request(0, "GET", "/api/v1/places/search?q=Manila", nil, 401, nil)
	request(owner, "GET", "/api/v1/places/search?q=M", nil, 400, nil)
	var found []places.Candidate
	request(owner, "GET", "/api/v1/places/search?q=Manila", nil, 200, &found)
	request(owner, "GET", "/api/v1/places/search?q=manila", nil, 200, &found)
	if len(found) != 1 || found[0].ProviderPlaceID != "N987654321" || found[0].TimeZone != "Asia/Manila" || providerCalls.Load() != 1 {
		t.Fatalf("normalized cached search failed: %+v, calls=%d", found, providerCalls.Load())
	}
	if _, err := pool.Exec(ctx, "UPDATE place_search_cache SET expires_at=now()-interval '1 second' WHERE query='manila'"); err != nil {
		t.Fatal(err)
	}
	request(owner, "GET", "/api/v1/places/search?q=Manila", nil, 200, &found)
	if providerCalls.Load() != 2 {
		t.Fatal("expired search cache did not refresh")
	}
	providerUnavailable.Store(true)
	request(owner, "GET", "/api/v1/places/search?q=Cebu", nil, 503, nil)
	providerUnavailable.Store(false)
	request(owner, "POST", "/api/v1/places/resolve", map[string]string{"provider_place_id": "https://example.com"}, 400, nil)
	var resolved places.Place
	request(owner, "POST", "/api/v1/places/resolve", map[string]string{"provider_place_id": "N987654321"}, 200, &resolved)
	placeID := resolved.ID
	request(viewer, "GET", fmt.Sprintf("/api/v1/places/%d", placeID), nil, 200, &resolved)
	if resolved.TimeZone != "Asia/Manila" {
		t.Fatal("place lookup did not persist time zone")
	}
	request(owner, "POST", "/api/v1/places/resolve", map[string]string{"provider_place_id": "N987654321"}, 200, &resolved)
	if resolved.ID != placeID {
		t.Fatal("resolve duplicated place")
	}
	placeIDs = append(placeIDs, placeID)
	located := in
	located.PlaceID = &placeID
	request(member, "PUT", path, located, 403, nil)
	request(outsider, "PUT", path, located, 404, nil)
	var withPlace itinerary.Activity
	request(owner, "PUT", path, located, 200, &withPlace)
	if withPlace.Place == nil || withPlace.Place.ID != placeID || withPlace.Place.TimeZone != "Asia/Manila" {
		t.Fatalf("place missing from activity: %+v", withPlace)
	}
	request(viewer, "GET", path, nil, 200, &withPlace)
	if withPlace.Place == nil || withPlace.Place.Name != "City Hall" {
		t.Fatal("database-only place read failed")
	}
	missingPlace := int64(9223372036854775807)
	located.PlaceID = &missingPlace
	request(owner, "PUT", path, located, 400, nil)
	if _, err := pool.Exec(ctx, "DELETE FROM places WHERE id=$1", placeID); err != nil {
		t.Fatal(err)
	}
	placeIDs = nil
	request(owner, "GET", path, nil, 200, &withPlace)
	if withPlace.PlaceID != nil || withPlace.Place != nil || withPlace.Title != in.Title || withPlace.Notes != in.Notes {
		t.Fatalf("place deletion changed activity snapshot: %+v", withPlace)
	}
	// A trip edit cannot strand an existing activity outside its new window.
	lateStart := time.Date(2026, 10, 1, 16, 0, 0, 0, time.UTC)
	tripInput.StartsAt = &lateStart
	request(owner, "PUT", fmt.Sprintf("/api/v1/trips/%d", trip.ID), tripInput, 409, nil)
	request(owner, "GET", path, nil, 200, nil)
	tripInput.StartsAt = &tripStart
	var newDestinationID int64
	err = pool.QueryRow(ctx, `INSERT INTO places (provider_place_id,kind,name,address,latitude,longitude,time_zone,refreshed_at)
		VALUES ($1,'city','New York','New York, USA',40.7128,-74.006,'America/New_York',now()) RETURNING id`,
		"N"+strconv.FormatInt(time.Now().UnixNano(), 10)).Scan(&newDestinationID)
	if err != nil {
		t.Fatal(err)
	}
	placeIDs = append(placeIDs, newDestinationID)
	tripInput.DestinationPlaceID = &newDestinationID
	request(owner, "PUT", fmt.Sprintf("/api/v1/trips/%d", trip.ID), tripInput, 200, nil)
	var movedZone itinerary.Activity
	request(owner, "GET", path, nil, 200, &movedZone)
	if movedZone.TimeZone != "America/New_York" || !movedZone.StartsAt.Equal(original.StartsAt) {
		t.Fatal("trip zone edit shifted activity instant")
	}
	tripInput.DestinationPlaceID = &destinationID
	request(owner, "PUT", fmt.Sprintf("/api/v1/trips/%d", trip.ID), tripInput, 200, nil)
	// Full replacement clears omitted notes and derives the activity zone from the trip.
	travel := map[string]any{"title": "Travel", "starts_at": "2026-10-03T14:00:00+08:00", "ends_at": "2026-10-03T14:30:00+08:00", "time_zone": "America/Los_Angeles", "notes": "Travel notes", "category": nil, "checklist": nil}
	var authored itinerary.Activity
	request(editor, "POST", base, travel, 201, &authored)
	if authored.Category == nil || *authored.Category != "Activity" || authored.Checklist == nil || *authored.Checklist == nil {
		t.Fatal("explicit null creation did not receive defaults")
	}
	authoredPath := fmt.Sprintf("%s/%d", base, authored.ID)
	delete(travel, "notes")
	request(owner, "PUT", authoredPath, travel, 200, &authored)
	if authored.CreatedByUserID != editor || authored.Notes != "" || authored.StartsAt.Location() != time.UTC || authored.TimeZone != "Asia/Manila" {
		t.Fatal("replacement lost authorship, notes default, or UTC normalization")
	}
	request(owner, "POST", fmt.Sprintf("/api/v1/trips/%d/transfer-ownership", trip.ID), memberships.TransferInput{UserID: editor}, 204, nil)
	request(editor, "DELETE", fmt.Sprintf("/api/v1/trips/%d/members/%d", trip.ID, owner), nil, 204, nil)
	request(owner, "GET", path, nil, 404, nil)
	request(editor, "GET", path, nil, 200, &unchanged)
	if unchanged.CreatedByUserID != owner {
		t.Fatal("membership removal or ownership transfer erased historical creator")
	}
	request(editor, "GET", authoredPath, nil, 200, &authored)
	if authored.CreatedByUserID != editor {
		t.Fatal("ownership transfer changed creator")
	}

	// A separate schedule exercises touching endpoints, nested overlaps and ordering ties.
	var conflicts []itinerary.Conflict
	w := request(owner, "GET", otherBase+"/conflicts", nil, 200, &conflicts)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("empty conflicts must be []")
	}
	var scheduled []itinerary.Activity
	for _, window := range [][2]int{{0, 180}, {60, 120}, {60, 90}, {180, 240}, {15, 45}} {
		activity := in
		activity.StartsAt, activity.EndsAt = start.Add(time.Duration(window[0])*time.Minute), start.Add(time.Duration(window[1])*time.Minute)
		var result itinerary.Activity
		request(owner, "POST", otherBase, activity, 201, &result)
		scheduled = append(scheduled, result)
		if len(scheduled) == 2 {
			request(owner, "GET", otherBase+"/conflicts", nil, 200, &conflicts)
			if len(conflicts) != 1 || conflicts[0].ActivityIDs != [2]int64{scheduled[0].ID, scheduled[1].ID} {
				t.Fatal("single overlap was not reported")
			}
		}
	}
	w = request(owner, "GET", otherBase+"/conflicts", nil, 200, &conflicts)
	want := []itinerary.Conflict{
		{ActivityIDs: [2]int64{scheduled[0].ID, scheduled[4].ID}, OverlapStartsAt: start.Add(15 * time.Minute), OverlapEndsAt: start.Add(45 * time.Minute)},
		{ActivityIDs: [2]int64{scheduled[0].ID, scheduled[1].ID}, OverlapStartsAt: start.Add(time.Hour), OverlapEndsAt: start.Add(2 * time.Hour)},
		{ActivityIDs: [2]int64{scheduled[0].ID, scheduled[2].ID}, OverlapStartsAt: start.Add(time.Hour), OverlapEndsAt: start.Add(90 * time.Minute)},
		{ActivityIDs: [2]int64{scheduled[1].ID, scheduled[2].ID}, OverlapStartsAt: start.Add(time.Hour), OverlapEndsAt: start.Add(90 * time.Minute)},
	}
	if !reflect.DeepEqual(conflicts, want) {
		t.Fatalf("conflicts = %+v, want %+v", conflicts, want)
	}
	if strings.Contains(w.Body.String(), "+") {
		t.Fatal("conflict timestamps must be UTC")
	}
	request(owner, "GET", otherBase+"/conflicts?limit=1&offset=1", nil, 200, &conflicts)
	if !reflect.DeepEqual(conflicts, want[1:2]) {
		t.Fatal("conflict pagination was applied before ordering")
	}
	request(owner, "GET", otherBase+"/conflicts?offset=4", nil, 200, &conflicts)
	if conflicts == nil || len(conflicts) != 0 {
		t.Fatal("exhausted conflict page must be []")
	}
	request(owner, "DELETE", fmt.Sprintf("%s/%d", otherBase, scheduled[1].ID), nil, 204, nil)
	request(owner, "DELETE", fmt.Sprintf("%s/%d", otherBase, scheduled[2].ID), nil, 204, nil)
	request(owner, "DELETE", fmt.Sprintf("%s/%d", otherBase, scheduled[4].ID), nil, 204, nil)
	request(owner, "GET", otherBase+"/conflicts", nil, 200, &conflicts)
	if len(conflicts) != 0 {
		t.Fatal("touching endpoints conflict")
	}
	request(owner, "DELETE", fmt.Sprintf("/api/v1/trips/%d", other.ID), nil, 204, nil)
	request(owner, "GET", otherBase+"/conflicts", nil, 404, nil)
}
