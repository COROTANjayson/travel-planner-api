package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/database"
	"travel-planner/travel-planner-api/internal/itinerary"
	"travel-planner/travel-planner-api/internal/memberships"
	"travel-planner/travel-planner-api/internal/prep"
	"travel-planner/travel-planner-api/internal/server"
	"travel-planner/travel-planner-api/internal/trips"
)

func TestPrepTasks(t *testing.T) {
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

	prefix := fmt.Sprintf("prep|%d", time.Now().UnixNano())
	users := make(map[int64]auth.User)
	addUser := func(name string) int64 {
		t.Helper()
		var user auth.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject,email,email_verified,display_name)
			VALUES ($1,$2,true,$3) RETURNING id,email,email_verified,display_name,created_at,updated_at`,
			prefix+"|"+name, name+"@example.com", name).Scan(&user.ID, &user.Email, &user.EmailVerified, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		users[user.ID] = user
		return user.ID
	}
	// IDs ascend in this order, which is the order ready and waiting lists use.
	ownerID, editorID, memberID, otherID, viewerID := addUser("owner"), addUser("editor"), addUser("member"), addUser("other"), addUser("viewer")
	joinerID, outsiderID := addUser("joiner"), addUser("outsider")
	t.Cleanup(func() {
		cleanupPool, err := database.Open(context.Background(), url)
		if err != nil {
			t.Error(err)
			return
		}
		defer cleanupPool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := cleanupPool.Exec(cleanupCtx, "DELETE FROM trips WHERE owner_user_id IN (SELECT id FROM users WHERE auth_subject LIKE $1)", prefix+"|%"); err != nil {
			t.Error(err)
			return
		}
		if _, err := cleanupPool.Exec(cleanupCtx, "DELETE FROM users WHERE auth_subject LIKE $1", prefix+"|%"); err != nil {
			t.Error(err)
		}
	})

	identity := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.Header.Get("X-Test-User-ID"), 10, 64)
			user, ok := users[id]
			if err != nil || !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
		})
	}
	authz := memberships.NewService(memberships.NewRepository(pool))
	handler := server.Router(
		trips.NewService(trips.NewRepository(pool), authz),
		itinerary.NewService(itinerary.NewRepository(pool), authz),
		authz,
		nil,
		prep.NewService(prep.NewRepository(pool), authz),
		identity,
	)
	send := func(userID int64, method, path string, body any) *httptest.ResponseRecorder {
		var payload []byte
		if body != nil {
			var err error
			if payload, err = json.Marshal(body); err != nil {
				panic(err)
			}
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User-ID", strconv.FormatInt(userID, 10))
		handler.ServeHTTP(w, req)
		return w
	}
	request := func(userID int64, method, path string, body any, want int, target any) {
		t.Helper()
		w := send(userID, method, path, body)
		if w.Code != want {
			t.Fatalf("user %d %s %s: got %d, want %d: %s", userID, method, path, w.Code, want, w.Body.String())
		}
		if target != nil {
			if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
				t.Fatal(err)
			}
		}
	}

	placeID := destinationFixture(t, ctx, pool, url)
	start := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 3, 15, 59, 59, 0, time.UTC)
	tripInput := trips.Input{Name: "Prep trip", DestinationPlaceID: &placeID, StartsAt: &start, EndsAt: &end}
	var trip, otherTrip trips.Trip
	request(ownerID, http.MethodPost, "/api/v1/trips", tripInput, http.StatusCreated, &trip)
	request(ownerID, http.MethodPost, "/api/v1/trips", tripInput, http.StatusCreated, &otherTrip)
	base := fmt.Sprintf("/api/v1/trips/%d", trip.ID)
	tasks := base + "/prep-tasks"
	addMember := func(userID int64, role string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO trip_members (trip_id,user_id,role) VALUES ($1,$2,$3)`, trip.ID, userID, role); err != nil {
			t.Fatal(err)
		}
	}
	addMember(editorID, "editor")
	addMember(memberID, "member")
	addMember(otherID, "member")
	addMember(viewerID, "viewer")
	leave := func(userID int64) {
		t.Helper()
		request(userID, http.MethodDelete, fmt.Sprintf("%s/members/%d", base, userID), nil, http.StatusNoContent, nil)
	}
	setRole := func(userID int64, role memberships.Role) {
		t.Helper()
		request(ownerID, http.MethodPut, fmt.Sprintf("%s/members/%d", base, userID), memberships.RoleInput{Role: role}, http.StatusOK, nil)
	}
	create := func(title string, kind prep.Kind, assignee *int64) prep.Task {
		t.Helper()
		var task prep.Task
		request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: title, Kind: kind, AssigneeUserID: assignee}, http.StatusCreated, &task)
		return task
	}
	read := func(id int64) prep.Task {
		t.Helper()
		var list []prep.Task
		request(viewerID, http.MethodGet, tasks, nil, http.StatusOK, &list)
		for _, task := range list {
			if task.ID == id {
				return task
			}
		}
		t.Fatalf("task %d is not listed", id)
		return prep.Task{}
	}
	expectCount := func(id int64, ready, waiting []int64) {
		t.Helper()
		task := read(id)
		if !slices.Equal(task.ReadyUserIDs, ready) || !slices.Equal(task.WaitingUserIDs, waiting) {
			t.Fatalf("task %d: ready %v waiting %v, want ready %v waiting %v", id, task.ReadyUserIDs, task.WaitingUserIDs, ready, waiting)
		}
	}
	expectAssignee := func(id int64, want *int64) {
		t.Helper()
		task := read(id)
		switch {
		case want == nil && task.Assignee != nil:
			t.Fatalf("task %d: assignee %d, want none", id, task.Assignee.UserID)
		case want != nil && (task.Assignee == nil || task.Assignee.UserID != *want):
			t.Fatalf("task %d: assignee %+v, want %d", id, task.Assignee, *want)
		}
	}
	ptr := func(id int64) *int64 { return &id }
	path := func(id int64, suffix string) string { return fmt.Sprintf("%s/%d%s", tasks, id, suffix) }

	// An empty list is an array, and hidden trips stay hidden.
	var empty []prep.Task
	request(viewerID, http.MethodGet, tasks, nil, http.StatusOK, &empty)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty list: %+v", empty)
	}
	request(outsiderID, http.MethodGet, tasks, nil, http.StatusNotFound, nil)
	request(outsiderID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindEveryone}, http.StatusNotFound, nil)

	// Only owners and editors manage tasks, and input is validated.
	request(memberID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindEveryone}, http.StatusForbidden, nil)
	request(viewerID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindEveryone}, http.StatusForbidden, nil)
	request(ownerID, http.MethodPost, tasks, map[string]any{"title": "No", "kind": "everyone", "unknown": true}, http.StatusBadRequest, nil)
	request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "  ", Kind: prep.KindEveryone}, http.StatusBadRequest, nil)
	request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindEveryone, AssigneeUserID: ptr(memberID)}, http.StatusBadRequest, nil)
	request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindOnePerson, AssigneeUserID: ptr(viewerID)}, http.StatusBadRequest, nil)
	request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindOnePerson, AssigneeUserID: ptr(outsiderID)}, http.StatusBadRequest, nil)

	// Everyone task: viewers are not counted, and each person ticks only their own.
	passport := create("Renew passport", prep.KindEveryone, nil)
	if passport.Assignee != nil || passport.CompletedAt != nil || passport.CreatedByUserID != ownerID {
		t.Fatalf("new everyone task: %+v", passport)
	}
	expectCount(passport.ID, []int64{}, []int64{ownerID, editorID, memberID, otherID})
	request(viewerID, http.MethodPut, path(passport.ID, "/completion"), nil, http.StatusForbidden, nil)
	request(outsiderID, http.MethodPut, path(passport.ID, "/completion"), nil, http.StatusNotFound, nil)
	request(memberID, http.MethodPut, path(passport.ID, "/completion"), nil, http.StatusNoContent, nil)
	request(memberID, http.MethodPut, path(passport.ID, "/completion"), nil, http.StatusNoContent, nil)
	expectCount(passport.ID, []int64{memberID}, []int64{ownerID, editorID, otherID})
	request(editorID, http.MethodDelete, path(passport.ID, "/completion"), nil, http.StatusNoContent, nil)
	expectCount(passport.ID, []int64{memberID}, []int64{ownerID, editorID, otherID})
	request(memberID, http.MethodDelete, path(passport.ID, "/completion"), nil, http.StatusNoContent, nil)
	expectCount(passport.ID, []int64{}, []int64{ownerID, editorID, memberID, otherID})
	for _, id := range []int64{ownerID, editorID, memberID, otherID} {
		request(id, http.MethodPut, path(passport.ID, "/completion"), nil, http.StatusNoContent, nil)
	}
	expectCount(passport.ID, []int64{ownerID, editorID, memberID, otherID}, []int64{})

	// Everyone tasks have no assignee and cannot be claimed.
	request(ownerID, http.MethodPut, path(passport.ID, "/assignee"), prep.AssigneeInput{UserID: ptr(memberID)}, http.StatusBadRequest, nil)
	request(memberID, http.MethodPut, path(passport.ID, "/claim"), nil, http.StatusBadRequest, nil)
	request(memberID, http.MethodDelete, path(passport.ID, "/claim"), nil, http.StatusBadRequest, nil)

	// Someone joining after a task was fully done is listed as left.
	addMember(joinerID, "member")
	expectCount(passport.ID, []int64{ownerID, editorID, memberID, otherID}, []int64{joinerID})

	// Leaving drops a tick from the count without deleting it; returning restores it.
	leave(memberID)
	expectCount(passport.ID, []int64{ownerID, editorID, otherID}, []int64{joinerID})
	var kept int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM prep_task_ticks WHERE task_id=$1 AND user_id=$2`, passport.ID, memberID).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("tick not retained after leaving: %d, %v", kept, err)
	}
	addMember(memberID, "member")
	expectCount(passport.ID, []int64{ownerID, editorID, memberID, otherID}, []int64{joinerID})

	// Becoming a viewer works the same way, and that person can no longer tick.
	setRole(memberID, memberships.RoleViewer)
	expectCount(passport.ID, []int64{ownerID, editorID, otherID}, []int64{joinerID})
	request(memberID, http.MethodDelete, path(passport.ID, "/completion"), nil, http.StatusForbidden, nil)
	setRole(memberID, memberships.RoleMember)
	expectCount(passport.ID, []int64{ownerID, editorID, memberID, otherID}, []int64{joinerID})

	// One-person task: claim, conflict, drop.
	van := create("Book the van", prep.KindOnePerson, nil)
	if van.Assignee != nil || len(van.ReadyUserIDs) != 0 || len(van.WaitingUserIDs) != 0 {
		t.Fatalf("new one-person task: %+v", van)
	}
	request(viewerID, http.MethodPut, path(van.ID, "/claim"), nil, http.StatusForbidden, nil)
	request(memberID, http.MethodPut, path(van.ID, "/completion"), nil, http.StatusConflict, nil)
	request(memberID, http.MethodPut, path(van.ID, "/claim"), nil, http.StatusNoContent, nil)
	request(memberID, http.MethodPut, path(van.ID, "/claim"), nil, http.StatusNoContent, nil)
	expectAssignee(van.ID, ptr(memberID))
	if got := read(van.ID); got.Assignee.DisplayName != "member" || got.Assignee.Email == nil {
		t.Fatalf("assignee identity: %+v", got.Assignee)
	}
	request(otherID, http.MethodPut, path(van.ID, "/claim"), nil, http.StatusConflict, nil)
	request(otherID, http.MethodDelete, path(van.ID, "/claim"), nil, http.StatusConflict, nil)
	request(memberID, http.MethodDelete, path(van.ID, "/claim"), nil, http.StatusNoContent, nil)
	expectAssignee(van.ID, nil)
	request(memberID, http.MethodDelete, path(van.ID, "/claim"), nil, http.StatusConflict, nil)

	// Assigning is for owners and editors, and only eligible people qualify.
	request(memberID, http.MethodPut, path(van.ID, "/assignee"), prep.AssigneeInput{UserID: ptr(memberID)}, http.StatusForbidden, nil)
	request(editorID, http.MethodPut, path(van.ID, "/assignee"), prep.AssigneeInput{UserID: ptr(viewerID)}, http.StatusBadRequest, nil)
	request(editorID, http.MethodPut, path(van.ID, "/assignee"), prep.AssigneeInput{UserID: ptr(outsiderID)}, http.StatusBadRequest, nil)
	request(editorID, http.MethodPut, path(van.ID, "/assignee"), prep.AssigneeInput{UserID: ptr(otherID)}, http.StatusNoContent, nil)
	expectAssignee(van.ID, ptr(otherID))

	// You only finish your own task; an editor assigns it to themselves first.
	request(editorID, http.MethodPut, path(van.ID, "/completion"), nil, http.StatusConflict, nil)
	request(memberID, http.MethodPut, path(van.ID, "/completion"), nil, http.StatusConflict, nil)
	request(otherID, http.MethodPut, path(van.ID, "/completion"), nil, http.StatusNoContent, nil)
	request(otherID, http.MethodPut, path(van.ID, "/completion"), nil, http.StatusNoContent, nil)
	if read(van.ID).CompletedAt == nil {
		t.Fatal("assignee's finish was not stored")
	}

	// A finished task cannot be reassigned or dropped until it is reopened.
	request(editorID, http.MethodPut, path(van.ID, "/assignee"), prep.AssigneeInput{UserID: ptr(memberID)}, http.StatusConflict, nil)
	request(ownerID, http.MethodPut, path(van.ID, "/assignee"), prep.AssigneeInput{}, http.StatusConflict, nil)
	request(otherID, http.MethodDelete, path(van.ID, "/claim"), nil, http.StatusConflict, nil)
	request(memberID, http.MethodPut, path(van.ID, "/claim"), nil, http.StatusConflict, nil)
	expectAssignee(van.ID, ptr(otherID))

	// The assignee or an editor can reopen; another member cannot.
	request(memberID, http.MethodDelete, path(van.ID, "/completion"), nil, http.StatusConflict, nil)
	request(otherID, http.MethodDelete, path(van.ID, "/completion"), nil, http.StatusNoContent, nil)
	request(otherID, http.MethodDelete, path(van.ID, "/completion"), nil, http.StatusNoContent, nil)
	if read(van.ID).CompletedAt != nil {
		t.Fatal("reopen did not clear completion")
	}
	request(otherID, http.MethodPut, path(van.ID, "/completion"), nil, http.StatusNoContent, nil)
	request(editorID, http.MethodDelete, path(van.ID, "/completion"), nil, http.StatusNoContent, nil)
	if read(van.ID).CompletedAt != nil {
		t.Fatal("editor reopen did not clear completion")
	}

	// A departed assignee's unfinished task reads as open, and comes back if they return.
	leave(otherID)
	expectAssignee(van.ID, nil)
	addMember(otherID, "member")
	expectAssignee(van.ID, ptr(otherID))
	leave(otherID)
	request(otherID, http.MethodPut, path(van.ID, "/claim"), nil, http.StatusNotFound, nil)
	request(memberID, http.MethodPut, path(van.ID, "/claim"), nil, http.StatusNoContent, nil)
	expectAssignee(van.ID, ptr(memberID))
	addMember(otherID, "member")
	expectAssignee(van.ID, ptr(memberID))

	// An assignee who becomes a viewer frees the task the same way.
	setRole(memberID, memberships.RoleViewer)
	expectAssignee(van.ID, nil)
	setRole(memberID, memberships.RoleMember)
	expectAssignee(van.ID, ptr(memberID))

	// Finished work keeps its name after the person leaves.
	request(memberID, http.MethodPut, path(van.ID, "/completion"), nil, http.StatusNoContent, nil)
	leave(memberID)
	if got := read(van.ID); got.CompletedAt == nil || got.Assignee == nil || got.Assignee.UserID != memberID || got.Assignee.DisplayName != "member" {
		t.Fatalf("finished task lost its assignee: %+v", got)
	}
	request(otherID, http.MethodPut, path(van.ID, "/claim"), nil, http.StatusConflict, nil)
	request(editorID, http.MethodDelete, path(van.ID, "/completion"), nil, http.StatusNoContent, nil)
	expectAssignee(van.ID, nil)
	addMember(memberID, "member")

	// Exactly one of many simultaneous claims wins.
	contested := create("Bring the speaker", prep.KindOnePerson, nil)
	claimants := []int64{ownerID, editorID, memberID, otherID, joinerID}
	codes := make([]int, len(claimants))
	var wait sync.WaitGroup
	for index, id := range claimants {
		wait.Add(1)
		go func() {
			defer wait.Done()
			codes[index] = send(id, http.MethodPut, path(contested.ID, "/claim"), nil).Code
		}()
	}
	wait.Wait()
	wins, conflicts := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusNoContent:
			wins++
		case http.StatusConflict:
			conflicts++
		}
	}
	if wins != 1 || conflicts != len(claimants)-1 {
		t.Fatalf("concurrent claims: %v", codes)
	}
	if read(contested.ID).Assignee == nil {
		t.Fatal("winning claim was not stored")
	}

	// Simultaneous ticks by different people are all kept.
	insurance := create("Get travel insurance", prep.KindEveryone, nil)
	for _, id := range claimants {
		wait.Add(1)
		go func() {
			defer wait.Done()
			send(id, http.MethodPut, path(insurance.ID, "/completion"), nil)
		}()
	}
	wait.Wait()
	expectCount(insurance.ID, []int64{ownerID, editorID, memberID, otherID, joinerID}, []int64{})

	// Renaming keeps state, and a task belongs to one trip only.
	var renamed prep.Task
	request(memberID, http.MethodPut, path(insurance.ID, ""), prep.UpdateInput{Title: "No"}, http.StatusForbidden, nil)
	request(ownerID, http.MethodPut, path(insurance.ID, ""), map[string]string{"title": "Insurance", "kind": "one_person"}, http.StatusBadRequest, nil)
	request(editorID, http.MethodPut, path(insurance.ID, ""), prep.UpdateInput{Title: "Insurance"}, http.StatusOK, &renamed)
	if renamed.Title != "Insurance" || renamed.Kind != prep.KindEveryone || len(renamed.ReadyUserIDs) != 5 {
		t.Fatalf("rename: %+v", renamed)
	}
	foreign := fmt.Sprintf("/api/v1/trips/%d/prep-tasks/%d", otherTrip.ID, insurance.ID)
	request(ownerID, http.MethodPut, foreign, prep.UpdateInput{Title: "No"}, http.StatusNotFound, nil)
	request(ownerID, http.MethodPut, foreign+"/completion", nil, http.StatusNotFound, nil)
	request(ownerID, http.MethodPut, foreign+"/claim", nil, http.StatusNotFound, nil)
	request(ownerID, http.MethodPut, foreign+"/assignee", prep.AssigneeInput{}, http.StatusNotFound, nil)
	request(ownerID, http.MethodDelete, foreign, nil, http.StatusNotFound, nil)

	// A task can point to an activity on its own trip, carry a date, or both.
	activityStart := start.Add(2 * time.Hour)
	activityInput := itinerary.Input{Title: "Airport transfer", StartsAt: activityStart, EndsAt: activityStart.Add(time.Hour)}
	var transfer, foreignActivity itinerary.Activity
	request(ownerID, http.MethodPost, base+"/activities", activityInput, http.StatusCreated, &transfer)
	request(ownerID, http.MethodPost, fmt.Sprintf("/api/v1/trips/%d/activities", otherTrip.ID), activityInput, http.StatusCreated, &foreignActivity)
	tripDay, badDate := "2026-10-02", "2026-02-30"
	if got := read(van.ID); got.Activity != nil || got.Date != nil {
		t.Fatalf("tasks have no activity or date by default: %+v", got)
	}
	var linked prep.Task
	request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "Book the driver", Kind: prep.KindOnePerson, ActivityID: &transfer.ID, Date: &tripDay}, http.StatusCreated, &linked)
	if linked.Activity == nil || linked.Activity.ID != transfer.ID || linked.Activity.Title != "Airport transfer" || !linked.Activity.StartsAt.Equal(activityStart) || linked.Activity.StartsAt.Location() != time.UTC {
		t.Fatalf("created activity link: %+v", linked.Activity)
	}
	if linked.Date == nil || *linked.Date != tripDay {
		t.Fatalf("a trip day is kept: %v", linked.Date)
	}
	request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindEveryone, ActivityID: &foreignActivity.ID}, http.StatusBadRequest, nil)
	request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindEveryone, ActivityID: ptr(1 << 40)}, http.StatusBadRequest, nil)
	request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindEveryone, Date: &badDate}, http.StatusBadRequest, nil)

	// A date must be one of the trip's days, which run Oct 1 to Oct 3 in its time zone.
	for _, day := range []string{"2026-10-01", "2026-10-03"} {
		request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "First or last day", Kind: prep.KindEveryone, Date: &day}, http.StatusCreated, nil)
	}
	for _, day := range []string{"2026-09-30", "2026-10-04", "2026-08-15"} {
		request(ownerID, http.MethodPost, tasks, prep.CreateInput{Title: "No", Kind: prep.KindEveryone, Date: &day}, http.StatusBadRequest, nil)
		request(ownerID, http.MethodPut, path(linked.ID, ""), prep.UpdateInput{Title: "No", Date: &day}, http.StatusBadRequest, nil)
	}

	// Editing changes the details and leaves the assignee and finish alone.
	request(editorID, http.MethodPut, path(linked.ID, "/assignee"), prep.AssigneeInput{UserID: ptr(otherID)}, http.StatusNoContent, nil)
	request(otherID, http.MethodPut, path(linked.ID, "/completion"), nil, http.StatusNoContent, nil)
	request(memberID, http.MethodPut, path(linked.ID, ""), prep.UpdateInput{Title: "No"}, http.StatusForbidden, nil)
	request(ownerID, http.MethodPut, path(linked.ID, ""), prep.UpdateInput{Title: "No", ActivityID: &foreignActivity.ID}, http.StatusBadRequest, nil)
	request(ownerID, http.MethodPut, path(1<<40, ""), prep.UpdateInput{Title: "No", ActivityID: &transfer.ID}, http.StatusNotFound, nil)
	var edited prep.Task
	request(editorID, http.MethodPut, path(linked.ID, ""), prep.UpdateInput{Title: "Book the driver", Date: &tripDay}, http.StatusOK, &edited)
	if edited.Activity != nil || edited.Date == nil || *edited.Date != tripDay || edited.Assignee == nil || edited.Assignee.UserID != otherID || edited.CompletedAt == nil {
		t.Fatalf("clearing the activity: %+v", edited)
	}
	edited = prep.Task{}
	request(editorID, http.MethodPut, path(linked.ID, ""), prep.UpdateInput{Title: "Book the driver", ActivityID: &transfer.ID}, http.StatusOK, &edited)
	if edited.Activity == nil || edited.Date != nil {
		t.Fatalf("clearing the date: %+v", edited)
	}
	request(editorID, http.MethodPut, path(linked.ID, ""), prep.UpdateInput{Title: "Book the driver", ActivityID: &transfer.ID, Date: &tripDay}, http.StatusOK, nil)

	// A renamed activity shows its new title, and deleting it only clears the link.
	activityInput.Title = "Airport pickup"
	request(ownerID, http.MethodPut, fmt.Sprintf("%s/activities/%d", base, transfer.ID), activityInput, http.StatusOK, nil)
	if got := read(linked.ID); got.Activity == nil || got.Activity.Title != "Airport pickup" {
		t.Fatalf("activity title: %+v", got.Activity)
	}
	request(ownerID, http.MethodDelete, fmt.Sprintf("%s/activities/%d", base, transfer.ID), nil, http.StatusNoContent, nil)
	if got := read(linked.ID); got.Activity != nil || got.Date == nil || *got.Date != tripDay || got.Title != "Book the driver" {
		t.Fatalf("task after its activity was deleted: %+v", got)
	}

	// If the trip moves, a saved date doesn't block other edits, but a new date must fit.
	if _, err := pool.Exec(ctx, `UPDATE trips SET start_date='2026-11-01',end_date='2026-11-03' WHERE id=$1`, trip.ID); err != nil {
		t.Fatal(err)
	}
	edited = prep.Task{}
	request(editorID, http.MethodPut, path(linked.ID, ""), prep.UpdateInput{Title: "Book the car", Date: &tripDay}, http.StatusOK, &edited)
	if edited.Title != "Book the car" || edited.Date == nil || *edited.Date != tripDay {
		t.Fatalf("editing a task with a stale date: %+v", edited)
	}
	staleDay := "2026-10-03"
	request(editorID, http.MethodPut, path(linked.ID, ""), prep.UpdateInput{Title: "Book the car", Date: &staleDay}, http.StatusBadRequest, nil)
	if _, err := pool.Exec(ctx, `UPDATE trips SET start_date='2026-10-01',end_date='2026-10-03' WHERE id=$1`, trip.ID); err != nil {
		t.Fatal(err)
	}

	// Acting on a deleted task is a 404, and deleting removes its ticks.
	request(memberID, http.MethodDelete, path(insurance.ID, ""), nil, http.StatusForbidden, nil)
	request(editorID, http.MethodDelete, path(insurance.ID, ""), nil, http.StatusNoContent, nil)
	for _, actor := range []string{"/completion", "/claim"} {
		request(memberID, http.MethodPut, path(insurance.ID, actor), nil, http.StatusNotFound, nil)
	}
	request(memberID, http.MethodDelete, path(insurance.ID, "/completion"), nil, http.StatusNotFound, nil)
	request(ownerID, http.MethodDelete, path(insurance.ID, ""), nil, http.StatusNotFound, nil)
	var ticks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM prep_task_ticks WHERE task_id=$1`, insurance.ID).Scan(&ticks); err != nil || ticks != 0 {
		t.Fatalf("ticks after delete: %d, %v", ticks, err)
	}

	// Transfer writes nothing, and counts change only when a viewer becomes owner.
	before := read(passport.ID)
	request(ownerID, http.MethodPost, base+"/transfer-ownership", memberships.TransferInput{UserID: editorID}, http.StatusNoContent, nil)
	expectCount(passport.ID, before.ReadyUserIDs, before.WaitingUserIDs)
	request(editorID, http.MethodPost, base+"/transfer-ownership", memberships.TransferInput{UserID: viewerID}, http.StatusNoContent, nil)
	expectCount(passport.ID, before.ReadyUserIDs, []int64{viewerID, joinerID})

	// The database rejects states the service must never write.
	if _, err := pool.Exec(ctx, `UPDATE prep_tasks SET assignee_user_id=$2 WHERE id=$1`, passport.ID, memberID); err == nil {
		t.Fatal("everyone task accepted an assignee")
	}
	if _, err := pool.Exec(ctx, `UPDATE prep_tasks SET completed_at=now(),assignee_user_id=NULL WHERE id=$1`, contested.ID); err == nil {
		t.Fatal("completion without an assignee was accepted")
	}

	// Deleting the trip removes its tasks and ticks.
	request(viewerID, http.MethodDelete, base, nil, http.StatusNoContent, nil)
	var left int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM prep_tasks WHERE trip_id=$1)+(SELECT count(*) FROM prep_task_ticks WHERE task_id=$2)`, trip.ID, passport.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows after trip delete: %d, %v", left, err)
	}
}
