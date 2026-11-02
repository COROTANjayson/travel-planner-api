package itinerary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"travel-planner/travel-planner-api/internal/apperror"
	"travel-planner/travel-planner-api/internal/expenses"
)

func planHash(id int64, in Input) ([]byte, error) {
	data, err := json.Marshal(struct {
		ID    int64
		Input Input
	}{id, in})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	return hash[:], nil
}

func previousPlan(ctx context.Context, tx pgx.Tx, actor, trip, id int64, in Input) (*Activity, error) {
	if in.RequestKey == "" {
		return nil, nil
	}
	hash, err := planHash(id, in)
	if err != nil {
		return nil, err
	}
	var savedHash, response []byte
	var activityID *int64
	err = tx.QueryRow(ctx, `SELECT request_hash,activity_id,response FROM activity_plan_requests WHERE trip_id=$1 AND actor_user_id=$2 AND request_key=$3`, trip, actor, in.RequestKey).Scan(&savedHash, &activityID, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(hash, savedHash) || activityID == nil {
		return nil, fmt.Errorf("%w: this activity submission key was already used", apperror.ErrConflict)
	}
	var a Activity
	if err := json.Unmarshal(response, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func finishPlan(ctx context.Context, tx pgx.Tx, actor, trip, originalID int64, in Input, a Activity) error {
	if err := expenses.SaveActivityCosts(ctx, tx, actor, trip, a.ID, in.ExpenseChanges, in.RequestKey); err != nil {
		return err
	}
	if in.RequestKey == "" {
		return nil
	}
	hash, err := planHash(originalID, in)
	if err != nil {
		return err
	}
	response, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO activity_plan_requests(trip_id,actor_user_id,request_key,request_hash,activity_id,response) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, trip, actor, in.RequestKey, hash, a.ID, response)
	return err
}
