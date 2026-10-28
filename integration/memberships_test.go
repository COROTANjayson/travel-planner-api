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
	"sync"
	"testing"
	"time"

	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/database"
	"travel-planner/travel-planner-api/internal/itinerary"
	"travel-planner/travel-planner-api/internal/memberships"
	"travel-planner/travel-planner-api/internal/server"
	"travel-planner/travel-planner-api/internal/trips"
)

func TestMembershipLifecycle(t *testing.T) {
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

	prefix := fmt.Sprintf("membership|%d", time.Now().UnixNano())
	users := make(map[int64]auth.User)
	addUser := func(suffix, email string, verified bool) int64 {
		t.Helper()
		var user auth.User
		if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject,email,email_verified,display_name)
			VALUES ($1,$2,$3,$4) RETURNING id,email,email_verified,display_name,created_at,updated_at`,
			prefix+"|"+suffix, email, verified, suffix).Scan(&user.ID, &user.Email, &user.EmailVerified, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		users[user.ID] = user
		return user.ID
	}
	ownerID := addUser("owner", "owner@example.com", true)
	memberID := addUser("member", "member@example.com", true)
	wrongID := addUser("wrong", "wrong@example.com", true)
	unverifiedID := addUser("unverified", "unverified@example.com", false)
	raceOneID := addUser("race-one", "race@example.com", true)
	raceTwoID := addUser("race-two", "race@example.com", true)

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

	testAuth := func(next http.Handler) http.Handler {
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
	membershipService := memberships.NewService(memberships.NewRepository(pool))
	handler := server.Router(
		trips.NewService(trips.NewRepository(pool), membershipService),
		itinerary.NewService(itinerary.NewRepository(pool), membershipService),
		membershipService,
		testAuth,
	)
	request := func(userID int64, method, path string, body any, want int, target any) *httptest.ResponseRecorder {
		t.Helper()
		var payload []byte
		if body != nil {
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
		if w.Code != want {
			t.Fatalf("%d %s %s: got %d, want %d: %s", userID, method, path, w.Code, want, w.Body.String())
		}
		if target != nil {
			if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
				t.Fatal(err)
			}
		}
		return w
	}

	placeID := destinationFixture(t, ctx, pool, url)
	tripStart := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	tripEnd := time.Date(2026, 10, 3, 15, 59, 59, 0, time.UTC)
	tripInput := trips.Input{Name: "Shared trip", DestinationPlaceID: &placeID, StartsAt: &tripStart, EndsAt: &tripEnd}
	var trip trips.Trip
	request(ownerID, http.MethodPost, "/api/v1/trips", tripInput, http.StatusCreated, &trip)
	base := fmt.Sprintf("/api/v1/trips/%d", trip.ID)
	request(wrongID, http.MethodGet, base+"/members", nil, http.StatusNotFound, nil)
	request(wrongID, http.MethodPost, base+"/invitations", memberships.InvitationInput{Email: "member@example.com", Role: memberships.RoleEditor}, http.StatusNotFound, nil)

	var replaced, active memberships.Invitation
	request(ownerID, http.MethodPost, base+"/invitations", memberships.InvitationInput{Email: " MEMBER@example.com ", Role: memberships.RoleEditor}, http.StatusCreated, &replaced)
	request(ownerID, http.MethodPost, base+"/invitations", memberships.InvitationInput{Email: "member@example.com", Role: memberships.RoleEditor}, http.StatusCreated, &active)
	request(memberID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: replaced.Token}, http.StatusBadRequest, nil)
	request(wrongID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: active.Token}, http.StatusBadRequest, nil)
	var participant memberships.Participant
	request(memberID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: active.Token}, http.StatusOK, &participant)
	request(memberID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: active.Token}, http.StatusOK, &participant)
	if participant.Role != memberships.RoleEditor {
		t.Fatalf("accepted role = %q", participant.Role)
	}
	var invitations []memberships.Invitation
	w := request(ownerID, http.MethodGet, base+"/invitations", nil, http.StatusOK, &invitations)
	if strings.Contains(w.Body.String(), active.Token) || len(invitations) != 2 {
		t.Fatal("invitation list leaked a token or omitted history")
	}
	request(memberID, http.MethodGet, base, nil, http.StatusOK, nil)
	tripInput.Name = "Edited by collaborator"
	request(memberID, http.MethodPut, base, tripInput, http.StatusOK, nil)
	request(memberID, http.MethodGet, base+"/invitations", nil, http.StatusForbidden, nil)

	start := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	activity := itinerary.Input{Title: "Breakfast", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Asia/Manila"}
	request(memberID, http.MethodPost, base+"/activities", activity, http.StatusCreated, nil)
	request(ownerID, http.MethodPut, base+"/members/"+strconv.FormatInt(memberID, 10), memberships.RoleInput{Role: memberships.RoleMember}, http.StatusOK, nil)
	request(memberID, http.MethodGet, base+"/activities", nil, http.StatusOK, nil)
	request(memberID, http.MethodPut, base, tripInput, http.StatusForbidden, nil)
	request(memberID, http.MethodPost, base+"/activities", activity, http.StatusForbidden, nil)
	request(ownerID, http.MethodPut, base+"/members/"+strconv.FormatInt(memberID, 10), memberships.RoleInput{Role: memberships.RoleViewer}, http.StatusOK, nil)
	request(memberID, http.MethodGet, base, nil, http.StatusOK, nil)
	request(memberID, http.MethodDelete, base+"/members/"+strconv.FormatInt(ownerID, 10), nil, http.StatusForbidden, nil)
	request(ownerID, http.MethodDelete, base+"/members/"+strconv.FormatInt(ownerID, 10), nil, http.StatusConflict, nil)

	var participants []memberships.Participant
	request(memberID, http.MethodGet, base+"/members", nil, http.StatusOK, &participants)
	if len(participants) != 2 || participants[0].UserID != ownerID || participants[0].Role != memberships.RoleOwner {
		t.Fatalf("participant list missing owner: %+v", participants)
	}
	request(memberID, http.MethodDelete, base+"/members/"+strconv.FormatInt(memberID, 10), nil, http.StatusNoContent, nil)
	request(memberID, http.MethodGet, base, nil, http.StatusNotFound, nil)

	var transferInvite memberships.Invitation
	request(ownerID, http.MethodPost, base+"/invitations", memberships.InvitationInput{Email: "member@example.com", Role: memberships.RoleEditor}, http.StatusCreated, &transferInvite)
	request(memberID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: transferInvite.Token}, http.StatusOK, nil)
	request(ownerID, http.MethodPost, base+"/transfer-ownership", memberships.TransferInput{UserID: memberID}, http.StatusNoContent, nil)
	request(ownerID, http.MethodPut, base, tripInput, http.StatusOK, nil)
	request(ownerID, http.MethodDelete, base, nil, http.StatusForbidden, nil)
	request(memberID, http.MethodDelete, base+"/members/"+strconv.FormatInt(ownerID, 10), nil, http.StatusNoContent, nil)
	request(ownerID, http.MethodGet, base, nil, http.StatusNotFound, nil)

	var revoked memberships.Invitation
	request(memberID, http.MethodPost, base+"/invitations", memberships.InvitationInput{Email: "unverified@example.com", Role: memberships.RoleViewer}, http.StatusCreated, &revoked)
	request(memberID, http.MethodDelete, fmt.Sprintf("%s/invitations/%d", base, revoked.ID), nil, http.StatusNoContent, nil)
	request(unverifiedID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: revoked.Token}, http.StatusBadRequest, nil)
	var expired memberships.Invitation
	request(memberID, http.MethodPost, base+"/invitations", memberships.InvitationInput{Email: "wrong@example.com", Role: memberships.RoleViewer}, http.StatusCreated, &expired)
	if _, err := pool.Exec(ctx, "UPDATE trip_invitations SET expires_at=now()-interval '1 second' WHERE id=$1", expired.ID); err != nil {
		t.Fatal(err)
	}
	request(wrongID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: expired.Token}, http.StatusBadRequest, nil)

	var raceInvite memberships.Invitation
	request(memberID, http.MethodPost, base+"/invitations", memberships.InvitationInput{Email: "race@example.com", Role: memberships.RoleViewer}, http.StatusCreated, &raceInvite)
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, userID := range []int64{raceOneID, raceTwoID} {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			payload, _ := json.Marshal(memberships.AcceptInput{Token: raceInvite.Token})
			w := httptest.NewRecorder()
			path := "/api/v1/invitations/accept"
			if userID == raceOneID {
				path = fmt.Sprintf("/api/v1/invitations/%d/accept", raceInvite.ID)
			}
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Test-User-ID", strconv.FormatInt(userID, 10))
			handler.ServeHTTP(w, req)
			statuses <- w.Code
		}(userID)
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusBadRequest]+counts[http.StatusNotFound] != 1 {
		t.Fatalf("concurrent acceptance statuses: %v", counts)
	}

	t.Run("recipient inbox and decisions", func(t *testing.T) {
		var inboxTrip trips.Trip
		request(ownerID, http.MethodPost, "/api/v1/trips", tripInput, http.StatusCreated, &inboxTrip)
		inboxBase := fmt.Sprintf("/api/v1/trips/%d", inboxTrip.ID)
		invite := func(email string) memberships.Invitation {
			t.Helper()
			var result memberships.Invitation
			request(ownerID, http.MethodPost, inboxBase+"/invitations", memberships.InvitationInput{Email: email, Role: memberships.RoleMember}, http.StatusCreated, &result)
			return result
		}
		actionPath := func(id int64, action string) string {
			return fmt.Sprintf("/api/v1/invitations/%d/%s", id, action)
		}
		inbox := func(userID int64) []memberships.ReceivedInvitation {
			t.Helper()
			var result []memberships.ReceivedInvitation
			w := request(userID, http.MethodGet, "/api/v1/invitations", nil, http.StatusOK, &result)
			if strings.Contains(w.Body.String(), "token") || strings.Contains(w.Body.String(), "email") {
				t.Fatal("recipient summary exposed private fields")
			}
			return result
		}
		// Invite before signup and discover using the normalized verified address.
		old := invite(" FUTURE@example.com ")
		pending := invite("future@example.com")
		futureID := addUser("future", " Future@EXAMPLE.com ", true)
		missingID := addUser("no-email", "", true)
		missingUser := users[missingID]
		missingUser.Email = nil
		users[missingID] = missingUser
		for _, id := range []int64{unverifiedID, missingID} {
			request(id, http.MethodGet, "/api/v1/invitations", nil, http.StatusForbidden, nil)
			for _, action := range []string{"accept", "decline"} {
				request(id, http.MethodPost, actionPath(pending.ID, action), nil, http.StatusNotFound, nil)
			}
		}
		request(0, http.MethodGet, "/api/v1/invitations", nil, http.StatusUnauthorized, nil)
		list := inbox(futureID)
		if len(list) != 1 || list[0].ID != pending.ID || list[0].TripID != inboxTrip.ID || list[0].TripName != inboxTrip.Name || list[0].Destination != inboxTrip.Destination || list[0].StartDate != inboxTrip.StartDate || list[0].EndDate != inboxTrip.EndDate || list[0].InviterName != "owner" || list[0].Role != memberships.RoleMember {
			t.Fatalf("incorrect recipient summary: %+v", list)
		}
		if len(inbox(ownerID)) != 0 || len(inbox(wrongID)) != 0 {
			t.Fatal("inbox was not isolated to recipient email")
		}
		request(futureID, http.MethodGet, inboxBase, nil, http.StatusNotFound, nil)
		for _, action := range []string{"accept", "decline"} {
			request(wrongID, http.MethodPost, actionPath(pending.ID, action), nil, http.StatusNotFound, nil)
			request(futureID, http.MethodPost, actionPath(old.ID, action), nil, http.StatusNotFound, nil)
			request(futureID, http.MethodPost, actionPath(9223372036854775807, action), nil, http.StatusNotFound, nil)
		}
		request(futureID, http.MethodPost, actionPath(pending.ID, "decline"), nil, http.StatusNoContent, nil)
		request(futureID, http.MethodPost, actionPath(pending.ID, "decline"), nil, http.StatusNotFound, nil)
		request(futureID, http.MethodPost, actionPath(pending.ID, "accept"), nil, http.StatusNotFound, nil)
		request(futureID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: pending.Token}, http.StatusBadRequest, nil)
		request(futureID, http.MethodGet, inboxBase, nil, http.StatusNotFound, nil)
		if len(inbox(futureID)) != 0 {
			t.Fatal("declined invitation stayed in inbox")
		}
		fresh := invite("future@example.com")
		var history []memberships.Invitation
		request(ownerID, http.MethodGet, inboxBase+"/invitations", nil, http.StatusOK, &history)
		if len(history) != 3 || history[1].DeclinedAt == nil || history[1].RevokedAt != nil {
			t.Fatalf("decline history lost when reinviting: %+v", history)
		}
		request(ownerID, http.MethodDelete, fmt.Sprintf("%s/invitations/%d", inboxBase, pending.ID), nil, http.StatusNotFound, nil)
		request(futureID, http.MethodPost, actionPath(fresh.ID, "accept"), nil, http.StatusOK, nil)
		request(futureID, http.MethodPost, actionPath(fresh.ID, "accept"), nil, http.StatusOK, nil)
		request(futureID, http.MethodPost, actionPath(fresh.ID, "decline"), nil, http.StatusNotFound, nil)
		if len(inbox(futureID)) != 0 {
			t.Fatal("accepted invitation stayed in inbox")
		}
		request(futureID, http.MethodGet, inboxBase, nil, http.StatusOK, nil)
		request(ownerID, http.MethodDelete, fmt.Sprintf("%s/members/%d", inboxBase, futureID), nil, http.StatusNoContent, nil)
		request(futureID, http.MethodPost, actionPath(fresh.ID, "accept"), nil, http.StatusNotFound, nil)
		request(futureID, http.MethodPost, "/api/v1/invitations/accept", memberships.AcceptInput{Token: fresh.Token}, http.StatusBadRequest, nil)
		expiring := invite("future@example.com")
		if _, err := pool.Exec(ctx, "UPDATE trip_invitations SET expires_at=now()-interval '1 second' WHERE id=$1", expiring.ID); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"accept", "decline"} {
			request(futureID, http.MethodPost, actionPath(expiring.ID, action), nil, http.StatusNotFound, nil)
		}
		if len(inbox(futureID)) != 0 {
			t.Fatal("expired invitation stayed in inbox")
		}
		revoking := invite("future@example.com")
		request(ownerID, http.MethodDelete, fmt.Sprintf("%s/invitations/%d", inboxBase, revoking.ID), nil, http.StatusNoContent, nil)
		if len(inbox(futureID)) != 0 {
			t.Fatal("revoked invitation stayed in inbox")
		}
		for _, action := range []string{"accept", "decline"} {
			request(futureID, http.MethodPost, actionPath(revoking.ID, action), nil, http.StatusNotFound, nil)
		}
		for _, competing := range []string{"accept", "revoke"} {
			race := invite("future@example.com")
			results := make(chan int, 2)
			for _, action := range []string{competing, "decline"} {
				wg.Add(1)
				go func(action string) {
					defer wg.Done()
					method, path, caller := http.MethodPost, actionPath(race.ID, action), futureID
					if action == "revoke" {
						method, path, caller = http.MethodDelete, fmt.Sprintf("%s/invitations/%d", inboxBase, race.ID), ownerID
					}
					w := httptest.NewRecorder()
					req := httptest.NewRequest(method, path, nil).WithContext(ctx)
					req.Header.Set("X-Test-User-ID", strconv.FormatInt(caller, 10))
					handler.ServeHTTP(w, req)
					results <- w.Code
				}(action)
			}
			wg.Wait()
			close(results)
			succeeded, failed := 0, 0
			for status := range results {
				switch status {
				case http.StatusOK, http.StatusNoContent:
					succeeded++
				case http.StatusNotFound:
					failed++
				default:
					t.Fatalf("unexpected race status: %d", status)
				}
			}
			if succeeded != 1 || failed != 1 {
				t.Fatalf("%s vs decline: %d succeeded, %d failed", competing, succeeded, failed)
			}
			// Remove membership only if acceptance won, before the next race.
			if _, err := pool.Exec(ctx, "DELETE FROM trip_members WHERE trip_id=$1 AND user_id=$2", inboxTrip.ID, futureID); err != nil {
				t.Fatal(err)
			}
		}
	})

}
