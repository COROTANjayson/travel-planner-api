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
	"travel-planner/travel-planner-api/internal/expenses"
	"travel-planner/travel-planner-api/internal/itinerary"
	"travel-planner/travel-planner-api/internal/memberships"
	"travel-planner/travel-planner-api/internal/server"
	"travel-planner/travel-planner-api/internal/trips"
)

func TestExpenseLedger(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated _test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var databaseName string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(databaseName, "_test") {
		t.Fatal("integration database must end in _test")
	}
	users := make([]int64, 5)
	stamp := time.Now().UnixNano()
	var tripID int64
	t.Cleanup(func() {
		cleanup, err := database.Open(context.Background(), url)
		if err != nil {
			t.Error(err)
			return
		}
		defer cleanup.Close()
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := cleanup.Exec(cleanupCtx, "DELETE FROM trips WHERE id=$1", tripID); err != nil {
			t.Error(err)
		}
		if _, err := cleanup.Exec(cleanupCtx, "DELETE FROM users WHERE id=ANY($1)", users); err != nil {
			t.Error(err)
		}
	})
	for i := range users {
		if err := pool.QueryRow(ctx, "INSERT INTO users (auth_subject,display_name) VALUES ($1,$2) RETURNING id", fmt.Sprintf("expense-test|%d|%d", stamp, i), fmt.Sprintf("Traveler %d", i)).Scan(&users[i]); err != nil {
			t.Fatal(err)
		}
	}
	owner, editor, member, viewer, outsider := users[0], users[1], users[2], users[3], users[4]
	place := destinationFixture(t, ctx, pool, url)
	if err := pool.QueryRow(ctx, `INSERT INTO trips (owner_user_id,name,destination,start_date,end_date,time_zone,destination_place_id) VALUES ($1,'Expense test','Cebu','2026-10-01','2026-10-03','Asia/Manila',$2) RETURNING id`, owner, place).Scan(&tripID); err != nil {
		t.Fatal(err)
	}
	for i, role := range []string{"editor", "member", "viewer"} {
		if _, err := pool.Exec(ctx, "INSERT INTO trip_members (trip_id,user_id,role) VALUES ($1,$2,$3)", tripID, users[i+1], role); err != nil {
			t.Fatal(err)
		}
	}
	authz := memberships.NewService(memberships.NewRepository(pool))
	identity := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.Header.Get("X-Test-User-ID"), 10, 64)
			valid := false
			for _, user := range users {
				if user == id {
					valid = true
				}
			}
			if err != nil || !valid {
				w.WriteHeader(401)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), auth.User{ID: id})))
		})
	}
	handler := server.Router(trips.NewService(trips.NewRepository(pool), authz), itinerary.NewService(itinerary.NewRepository(pool), authz), authz, expenses.NewService(expenses.NewRepository(pool), authz), nil, identity)
	base := fmt.Sprintf("/api/v1/trips/%d", tripID)
	perform := func(actor int64, method, path string, body any, key string) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload)).WithContext(ctx)
		req.Header.Set("X-Test-User-ID", strconv.FormatInt(actor, 10))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	request := func(actor int64, method, path string, body any, key string, want int, target any) {
		t.Helper()
		w := perform(actor, method, path, body, key)
		if w.Code != want {
			t.Fatalf("%s %s status %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		if target != nil {
			if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
				t.Fatal(err)
			}
		}
	}
	activityExpenseScenario(t, ctx, pool, tripID, users, request)
	reversePurchase := func(id int64) {
		var history expenses.Ledger
		request(owner, "GET", base+"/expense-ledger", nil, "", 200, &history)
		for _, p := range history.Payments {
			if p.ExpenseID == id {
				request(owner, "DELETE", fmt.Sprintf("%s/expense_payments/%d?version=%d", base, p.ID, p.Version), nil, "", 204, nil)
			}
		}
	}
	var currencyList []expenses.Currency
	request(owner, "GET", "/api/v1/expense-currencies", nil, "", 200, &currencyList)
	if len(currencyList) != 17 {
		t.Fatal("missing currencies")
	}
	request(outsider, "GET", base+"/expenses", nil, "", 404, nil)
	request(owner, "GET", base+"/expenses?limit=101", nil, "", 400, nil)
	request(owner, "GET", base+"/expenses?currency=XYZ", nil, "", 400, nil)
	in := expenses.ExpenseInput{Description: "Advance booking", Currency: "PHP", Amount: "101", Date: "2026-09-01", PayerID: member, SplitMode: "equal", Shares: []expenses.Share{{UserID: member}, {UserID: owner}}}
	request(viewer, "POST", base+"/expenses", in, "viewer", 403, nil)
	request(member, "POST", base+"/expenses", in, "", 400, nil)
	var original expenses.Record
	request(member, "POST", base+"/expenses", in, "booking", 201, &original)
	if original.CreatorID != member || original.Version != 1 || len(original.People) != 2 || original.Shares[0].Amount != "51" || original.Shares[1].Amount != "50" {
		t.Fatalf("bad record: %+v", original)
	}
	var retry expenses.Record
	request(member, "POST", base+"/expenses", in, "booking", 200, &retry)
	if retry.ID != original.ID {
		t.Fatal("retry duplicated expense")
	}
	changed := in
	changed.Amount = "102"
	request(member, "POST", base+"/expenses", changed, "booking", 409, nil)
	path := fmt.Sprintf("%s/expenses/%d", base, original.ID)
	request(outsider, "GET", path, nil, "", 404, nil)
	request(viewer, "GET", path, nil, "", 200, nil)
	changed.Version = 1
	// Being payer alone gives no permission: use a separate member's record below.
	request(viewer, "PUT", path, changed, "", 403, nil)
	request(editor, "PUT", path, changed, "", 200, &retry)
	if retry.Version != 2 {
		t.Fatal("update did not increment version")
	}
	request(member, "PUT", path, changed, "", 409, nil)
	request(member, "DELETE", path+"?version=1", nil, "", 409, nil)
	request(owner, "DELETE", path, nil, "", 400, nil)
	in.Version = retry.Version
	request(member, "PUT", path, in, "", 200, &original)
	var summary expenses.Summary
	request(owner, "GET", base+"/balances", nil, "", 200, &summary)
	if len(summary.Currencies) != 1 || summary.Currencies[0].Total != "101" || summary.Currencies[0].Transfers[0].Amount != "51" {
		t.Fatalf("wrong balances: %+v", summary)
	}
	// Former members lose access, but retained shares and repayments remain valid.
	request(owner, "DELETE", fmt.Sprintf("%s/members/%d", base, member), nil, "", 204, nil)
	request(member, "GET", base+"/balances", nil, "", 404, nil)
	in.Version = original.Version
	request(owner, "PUT", path, in, "", 200, &original)
	payment := expenses.SettlementInput{Currency: "PHP", Amount: "51", Date: "2026-11-01", FromID: owner, ToID: member}
	var settlement expenses.Record
	request(owner, "POST", base+"/settlements", payment, "paid", 201, &settlement)
	request(owner, "POST", base+"/settlements", payment, "paid", 200, nil)
	request(owner, "GET", base+"/balances", nil, "", 200, &summary)
	for _, b := range summary.Currencies[0].Balances {
		if b.Amount != "0" {
			t.Fatalf("payment sign incorrect: %+v", summary)
		}
	}
	payment.Amount = "75"
	payment.Version = settlement.Version
	payment.CorrectionReason = "Correct historical receipt"
	settlementPath := fmt.Sprintf("%s/settlements/%d", base, settlement.ID)
	request(editor, "PUT", settlementPath, payment, "", 200, &settlement)
	request(owner, "GET", base+"/balances", nil, "", 200, &summary)
	if summary.Currencies[0].Transfers[0].FromID != member || summary.Currencies[0].Transfers[0].Amount != "24" {
		t.Fatal("overpayment did not reverse balance")
	}
	request(owner, "DELETE", fmt.Sprintf("%s?version=%d&correction_reason=Correction", settlementPath, settlement.Version), nil, "", 204, nil)
	payment.Version = 0
	payment.Amount = "50"
	request(owner, "POST", base+"/settlements", payment, "paid", 409, nil)
	request(owner, "GET", settlementPath, nil, "", 404, nil)
	// Different currencies and filtered pagination do not change whole-ledger totals.
	jpy := expenses.ExpenseInput{Description: "Train", Currency: "JPY", Amount: "1", Date: "2026-10-02", PayerID: viewer, SplitMode: "equal", Shares: []expenses.Share{{UserID: owner}, {UserID: viewer}, {UserID: editor}}}
	var tiny expenses.Record
	request(editor, "POST", base+"/expenses", jpy, "train", 201, &tiny)
	if tiny.Shares[1].Amount != "0" || tiny.Shares[2].Amount != "0" {
		t.Fatal("zero shares were lost")
	}
	request(viewer, "PUT", fmt.Sprintf("%s/expenses/%d", base, tiny.ID), jpy, "", 400, nil)
	jpy.Version = tiny.Version
	request(viewer, "PUT", fmt.Sprintf("%s/expenses/%d", base, tiny.ID), jpy, "", 403, nil)
	var filtered []expenses.Record
	request(owner, "GET", base+"/expenses?currency=PHP&limit=1", nil, "", 200, &filtered)
	if len(filtered) != 1 || filtered[0].ID != original.ID {
		t.Fatal("currency filtering failed")
	}
	request(owner, "GET", base+"/balances", nil, "", 200, &summary)
	if len(summary.Currencies) != 2 {
		t.Fatal("currencies combined")
	}
	// Rollback after aggregate overflow restores both expense and shares.
	maxExpense := expenses.ExpenseInput{Description: "Range boundary", Currency: "USD", Amount: "9223372036854775807", Date: "2026-10-01", PayerID: owner, SplitMode: "exact", Shares: []expenses.Share{{UserID: viewer, Amount: "9223372036854775807"}}}
	var boundary expenses.Record
	request(owner, "POST", base+"/expenses", maxExpense, "max", 201, &boundary)
	maxExpense.Amount = "1"
	maxExpense.Shares = []expenses.Share{{UserID: viewer, Amount: "1"}}
	request(owner, "POST", base+"/expenses", maxExpense, "overflow", 400, nil)
	request(owner, "GET", base+"/expenses?currency=USD", nil, "", 200, &filtered)
	if len(filtered) != 1 || filtered[0].Amount != "9223372036854775807" {
		t.Fatal("overflow did not rollback")
	}
	// A failure on the second share must also roll back replacing the first.
	if _, err := pool.Exec(ctx, `CREATE FUNCTION expense_test_reject_share() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.amount_minor=999 THEN RAISE EXCEPTION 'test share rejection'; END IF; RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER expense_test_reject_share BEFORE INSERT ON expense_shares FOR EACH ROW EXECUTE FUNCTION expense_test_reject_share()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, err := database.Open(context.Background(), url)
		if err != nil {
			t.Error(err)
			return
		}
		defer cleanup.Close()
		if _, err := cleanup.Exec(context.Background(), `DROP TRIGGER IF EXISTS expense_test_reject_share ON expense_shares; DROP FUNCTION IF EXISTS expense_test_reject_share()`); err != nil {
			t.Error(err)
		}
	})
	replace := in
	replace.Version = original.Version
	replace.Amount = "1000"
	replace.SplitMode = "exact"
	replace.Shares = []expenses.Share{{UserID: owner, Amount: "1"}, {UserID: member, Amount: "999"}}
	request(owner, "PUT", path, replace, "", 500, nil)
	request(owner, "GET", path, nil, "", 200, &retry)
	if retry.Version != original.Version || retry.Amount != original.Amount || retry.Shares[0].Amount != original.Shares[0].Amount {
		t.Fatal("failed share replacement lost original data")
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER expense_test_reject_share ON expense_shares; DROP FUNCTION expense_test_reject_share()`); err != nil {
		t.Fatal(err)
	}
	// Concurrent replacements must not silently overwrite one another.
	updates := make(chan *httptest.ResponseRecorder, 2)
	for _, title := range []string{"First correction", "Second correction"} {
		update := in
		update.Version = original.Version
		update.Description = title
		go func() { updates <- perform(owner, "PUT", path, update, "") }()
	}
	updated, stale := 0, 0
	for range 2 {
		w := <-updates
		switch w.Code {
		case 200:
			updated++
			if err := json.Unmarshal(w.Body.Bytes(), &original); err != nil {
				t.Fatal(err)
			}
		case 409:
			stale++
		default:
			t.Fatalf("concurrent update: %d %s", w.Code, w.Body.String())
		}
	}
	if updated != 1 || stale != 1 {
		t.Fatal("concurrent replacements lost version protection")
	}
	// Concurrent retries create exactly one entry.
	jpy.Version = 0
	results := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- perform(editor, "POST", base+"/expenses", jpy, "concurrent") }()
	}
	wg.Wait()
	close(results)
	created := 0
	var sameID int64
	for response := range results {
		if response.Code == 201 {
			created++
		} else if response.Code != 200 {
			t.Fatalf("concurrent status %d: %s", response.Code, response.Body.String())
		}
		var record expenses.Record
		if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if sameID != 0 && record.ID != sameID {
			t.Fatal("duplicate IDs")
		}
		sameID = record.ID
	}
	if created != 1 {
		t.Fatalf("created %d records", created)
	}
	// An authorization preflight cannot authorize a write after removal wins.
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	var lockedTrip int64
	if err := blocker.QueryRow(ctx, "SELECT id FROM trips WHERE id=$1 FOR UPDATE", tripID).Scan(&lockedTrip); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, "DELETE FROM trip_members WHERE trip_id=$1 AND user_id=$2", tripID, editor); err != nil {
		t.Fatal(err)
	}
	waiting := make(chan *httptest.ResponseRecorder, 1)
	go func() { waiting <- perform(editor, "POST", base+"/expenses", jpy, "removed-race") }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT owner_user_id FROM trips WHERE id=%FOR UPDATE')`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ledger did not wait on the trip lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if w := <-waiting; w.Code != 404 {
		t.Fatalf("removed caller wrote after preflight: %d %s", w.Code, w.Body.String())
	}
	if _, err := pool.Exec(ctx, "INSERT INTO trip_members (trip_id,user_id,role) VALUES ($1,$2,'editor')", tripID, editor); err != nil {
		t.Fatal(err)
	}
	// Viewer downgrade removes creator mutation rights immediately.
	request(owner, "PUT", fmt.Sprintf("%s/members/%d", base, editor), map[string]string{"role": "viewer"}, "", 200, nil)
	request(editor, "DELETE", fmt.Sprintf("%s/expenses/%d?version=1", base, tiny.ID), nil, "", 403, nil)
	request(owner, "DELETE", fmt.Sprintf("%s?version=%d", path, original.Version), nil, "", 409, nil)
	reversePurchase(original.ID)
	request(owner, "DELETE", fmt.Sprintf("%s?version=%d", path, original.Version), nil, "", 204, nil)
	request(owner, "GET", path, nil, "", 404, nil)
	// Deleted creation keys remain reserved, even after ownership changes.
	reversePurchase(boundary.ID)
	request(owner, "DELETE", fmt.Sprintf("%s/expenses/%d?version=%d", base, boundary.ID, boundary.Version), nil, "", 204, nil)
	maxExpense.Amount = "9223372036854775807"
	maxExpense.Shares = []expenses.Share{{UserID: viewer, Amount: "9223372036854775807"}}
	request(owner, "POST", base+"/expenses", maxExpense, "max", 409, nil)
	request(owner, "POST", base+"/transfer-ownership", map[string]int64{"user_id": viewer}, "", 204, nil)
	request(owner, "DELETE", base, nil, "", 403, nil)
	request(viewer, "DELETE", base, nil, "", 204, nil)
	var count int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM expenses WHERE trip_id=$1)+(SELECT count(*) FROM settlements WHERE trip_id=$1)", tripID).Scan(&count); err != nil || count != 0 {
		t.Fatal("trip deletion did not cascade", err)
	}
}
