package expenses

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
	"travel-planner/travel-planner-api/internal/apperror"
)

type CostChange struct {
	ID int64 `json:"id,omitempty"`
	ExpenseInput
}

// SaveActivityCosts joins the activity's transaction so a failed cost cannot leave a partial plan.
func SaveActivityCosts(ctx context.Context, tx pgx.Tx, actor, trip, activity int64, changes []CostChange, key string) error {
	if len(changes) == 0 {
		return nil
	}
	roles, err := lockWriter(ctx, tx, actor, trip)
	if err != nil {
		return err
	}
	if !editor(roles[actor]) {
		return apperror.ErrForbidden
	}
	seen := map[int64]bool{}
	for index, change := range changes {
		if change.ID < 0 || change.PayerID != 0 {
			return invalid("activity planning cannot record business payments")
		}
		if (change.ID == 0 && change.Version != 0) || (change.ID > 0 && change.Version < 1) {
			return invalid("existing costs require a version")
		}
		if change.ID > 0 {
			if seen[change.ID] {
				return invalid("each cost can appear once in an activity save")
			}
			seen[change.ID] = true
			old, err := get(ctx, tx, trip, expense, change.ID)
			if err != nil {
				return err
			}
			if old.ActivityID != activity {
				return invalid("cost must belong to the activity being saved")
			}
		}
		change.ActivityID = activity
		in, err := validate(writeInput{ExpenseInput: change.ExpenseInput}, expense)
		if err != nil {
			return err
		}
		canonical, err := json.Marshal(in)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(canonical)
		costKeyHash := sha256.Sum256([]byte(key + ":" + strconv.Itoa(index)))
		costKey := "activity-plan:" + hex.EncodeToString(costKeyHash[:])
		if _, _, err := saveTx(ctx, tx, actor, trip, expense, change.ID, in, costKey, hash[:]); err != nil {
			return err
		}
	}
	return nil
}
