package expenses

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"travel-planner/travel-planner-api/internal/apperror"
)

func validCorrectionReason(reason string) bool {
	return strings.TrimSpace(reason) != "" && utf8.RuneCountInString(reason) <= 500
}

func appendReceiptHistory(ctx context.Context, tx pgx.Tx, actor, id int64, action, reason string, before, after *Record) error {
	previous, err := json.Marshal(before)
	if err != nil {
		return err
	}
	next, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO settlement_history(settlement_id,actor_user_id,action,reason,before_record,after_record) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb)`, id, actor, action, strings.TrimSpace(reason), previous, next)
	return err
}

func (r *Repository) ReceiptHistory(ctx context.Context, trip, id int64) ([]ReceiptHistory, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlements WHERE trip_id=$1 AND id=$2)`, trip, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, apperror.ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT h.id,h.actor_user_id,h.action,h.reason,h.before_record,h.after_record,h.created_at FROM settlement_history h JOIN settlements s ON s.id=h.settlement_id WHERE s.trip_id=$1 AND s.id=$2 ORDER BY h.id`, trip, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReceiptHistory{}
	for rows.Next() {
		var item ReceiptHistory
		var before, after []byte
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.Reason, &before, &after, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(before) > 0 {
			if err := json.Unmarshal(before, &item.Before); err != nil {
				return nil, err
			}
		}
		if len(after) > 0 {
			if err := json.Unmarshal(after, &item.After); err != nil {
				return nil, err
			}
		}
		item.CreatedAt = item.CreatedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}
