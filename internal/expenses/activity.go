package expenses

import (
	"context"
	"math/big"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Allocate covers the payer first, then apportions the rest without losing minor units.
func Allocate(amount, payer int64, remaining []Share) ([]Share, error) {
	values := map[int64]int64{}
	var total int64
	for _, s := range remaining {
		v, err := minor(s.Amount, true)
		if err != nil {
			return nil, err
		}
		values[s.UserID] = v
		total, err = checkedAdd(total, v)
		if err != nil {
			return nil, err
		}
	}
	if amount <= 0 || amount > total {
		return nil, invalid("payment exceeds the remaining business amount")
	}
	own := min(amount, values[payer])
	result := map[int64]int64{}
	result[payer] = own
	values[payer] -= own
	amount -= own
	total -= own
	ids := make([]int64, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if amount > 0 {
		left := amount
		for _, id := range ids {
			n := new(big.Int).Mul(big.NewInt(amount), big.NewInt(values[id]))
			n.Quo(n, big.NewInt(total))
			result[id] += n.Int64()
			left -= n.Int64()
		}
		for _, id := range ids {
			if left > 0 && result[id] < values[id]+func() int64 {
				if id == payer {
					return own
				}
				return 0
			}() {
				result[id]++
				left--
			}
		}
	}
	out := []Share{}
	for _, id := range ids {
		if result[id] > 0 {
			out = append(out, Share{id, strconv.FormatInt(result[id], 10)})
		}
	}
	return out, nil
}

func ledger(ctx context.Context, q querier, trip int64) (Ledger, error) {
	l := Ledger{Expenses: []Record{}, Payments: []Record{}, Loans: []Record{}, Settlements: []Record{}, ReversedSettlements: []Record{}, Debts: []Debt{}}
	for _, entry := range []struct {
		k        kind
		dest     *[]Record
		reversed bool
	}{{expense, &l.Expenses, false}, {businessPayment, &l.Payments, false}, {loan, &l.Loans, false}, {settlement, &l.Settlements, false}, {settlement, &l.ReversedSettlements, true}} {
		condition := "e.deleted_at IS NULL"
		if entry.reversed {
			condition = "e.deleted_at IS NOT NULL"
		}
		rows, err := q.Query(ctx, recordSQL(entry.k)+`WHERE e.trip_id=$1 AND `+condition+` ORDER BY e.id`, trip)
		if err != nil {
			return l, err
		}
		for rows.Next() {
			r, err := scanRecord(rows, entry.k)
			if err != nil {
				rows.Close()
				return l, err
			}
			*entry.dest = append(*entry.dest, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return l, err
		}
	}
	var err error
	l.People, err = historicalPeople(ctx, q, trip)
	if err != nil {
		return l, err
	}
	l.Debts, err = debts(l)
	return l, err
}
func (r *Repository) Ledger(ctx context.Context, trip int64) (Ledger, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Ledger{}, err
	}
	defer tx.Rollback(ctx)
	l, err := ledger(ctx, tx, trip)
	if err != nil {
		return l, err
	}
	return l, tx.Commit(ctx)
}
func debts(l Ledger) ([]Debt, error) {
	out := []Debt{}
	costs := map[int64]Record{}
	for _, e := range l.Expenses {
		costs[e.ID] = e
	}
	for _, p := range l.Payments {
		e := costs[p.ExpenseID]
		for _, a := range p.Allocations {
			if a.UserID != p.PayerID && a.Amount != "0" {
				out = append(out, Debt{PaymentID: p.ID, ActivityID: e.ActivityID, ActivityName: e.ActivityName, Description: e.Description, Currency: p.Currency, FromID: a.UserID, ToID: p.PayerID, Amount: a.Amount})
			}
		}
	}
	for _, l := range l.Loans {
		out = append(out, Debt{LoanID: l.ID, ActivityID: l.ActivityID, ActivityName: l.ActivityName, Description: l.Description, Currency: l.Currency, FromID: l.ToID, ToID: l.FromID, Amount: l.Amount})
	}
	for i := range out {
		d := &out[i]
		v, _ := minor(d.Amount, true)
		for _, s := range l.Settlements {
			if s.FromID == d.FromID && s.ToID == d.ToID && s.Currency == d.Currency && ((d.PaymentID > 0 && s.PaymentID == d.PaymentID) || (d.LoanID > 0 && s.LoanID == d.LoanID)) {
				n, _ := minor(s.Amount, false)
				var err error
				v, err = checkedAdd(v, -n)
				if err != nil {
					return nil, err
				}
			}
		}
		d.Amount = strconv.FormatInt(v, 10)
	}
	return out, nil
}
