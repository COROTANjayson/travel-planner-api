package expenses

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"travel-planner/travel-planner-api/internal/apperror"
)

type Store interface {
	PreviewCost(context.Context, int64, int64, int64, writeInput) (CostPreview, error)
	PaymentStatus(context.Context, int64, int64) (PaymentStatus, error)
	PreviewPayment(context.Context, int64, int64, writeInput) (PaymentPreview, error)
	List(context.Context, int64, kind, string, int, int) ([]Record, error)
	Get(context.Context, int64, kind, int64) (Record, error)
	Save(context.Context, int64, int64, kind, int64, writeInput, string, []byte) (Record, bool, error)
	Delete(context.Context, int64, int64, kind, int64, int64, string) error
	Summary(context.Context, int64) (Summary, error)
	Ledger(context.Context, int64) (Ledger, error)
	ReceiptHistory(context.Context, int64, int64) ([]ReceiptHistory, error)
}
type Authorizer interface {
	RequireParticipant(context.Context, int64, int64) error
}
type Service struct {
	store Store
	authz Authorizer
}

func NewService(store Store, authz Authorizer) *Service { return &Service{store, authz} }
func invalid(message string) error                      { return fmt.Errorf("%w: %s", apperror.ErrInvalid, message) }
func conflict(message string) error                     { return fmt.Errorf("%w: %s", apperror.ErrConflict, message) }
func supported(code string) bool {
	for _, c := range currencies {
		if c.Code == code {
			return true
		}
	}
	return false
}
func minor(text string, zero bool) (int64, error) {
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 || (!zero && value == 0) || strconv.FormatInt(value, 10) != text {
		return 0, invalid("amount_minor must be a canonical integer string within the signed 64-bit range")
	}
	return value, nil
}
func checkedAdd(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, invalid("money total exceeds the supported range")
	}
	return a + b, nil
}
func validate(in writeInput, k kind) (writeInput, error) {
	if !supported(in.Currency) {
		return in, invalid("unsupported currency")
	}
	amount, err := minor(in.Amount, false)
	if err != nil {
		return in, err
	}
	d, err := time.Parse(time.DateOnly, in.Date)
	if err != nil || d.Year() < 1 || d.Year() > 9999 || d.Format(time.DateOnly) != in.Date {
		return in, invalid("date must be YYYY-MM-DD")
	}
	if k == settlement || k == loan {
		if in.FromID <= 0 || in.ToID <= 0 || in.FromID == in.ToID {
			return in, invalid("payment needs two different travelers")
		}
		if in.PaymentID < 0 || in.LoanID < 0 || (in.PaymentID > 0 && in.LoanID > 0) {
			return in, invalid("choose one repayment source")
		}
		if k == loan && (in.ActivityID <= 0 || len(strings.TrimSpace(in.Description)) == 0 || len(in.Description) > 200) {
			return in, invalid("loan needs activity and description")
		}
		return in, nil
	}
	if k == businessPayment {
		if in.ExpenseID <= 0 || in.PayerID <= 0 {
			return in, invalid("payment needs expense and payer")
		}
		return in, nil
	}
	if in.PlannedCovererID != 0 || in.PlannedCoverAmount != "" {
		planned, err := minor(in.PlannedCoverAmount, false)
		if err != nil || in.PlannedCovererID <= 0 || planned > amount {
			return in, invalid("planned coverage needs a traveler and a positive amount within the cost")
		}
	}
	in.Description = strings.TrimSpace(in.Description)
	if in.Description == "" || len(in.Description) > 200 || in.ActivityID < 0 || in.PayerID < 0 {
		return in, invalid("description (up to 200 bytes) is required")
	}
	if in.AmountBasis == "" {
		in.AmountBasis = "group"
	}
	if in.AmountBasis != "group" && in.AmountBasis != "per_person" {
		return in, invalid("amount_basis must be group or per_person")
	}
	if in.AmountBasis == "per_person" && (in.SplitMode != "equal" || len(in.Shares) == 0 || amount%int64(len(in.Shares)) != 0) {
		return in, invalid("per-person costs need equal whole minor-unit shares")
	}
	if in.SplitMode != "equal" && in.SplitMode != "exact" {
		return in, invalid("split_mode must be equal or exact")
	}
	if len(in.Shares) == 0 || len(in.Shares) > 1000 {
		return in, invalid("select between 1 and 1000 travelers")
	}
	in.Shares = append([]Share(nil), in.Shares...)
	sort.Slice(in.Shares, func(i, j int) bool { return in.Shares[i].UserID < in.Shares[j].UserID })
	var sum int64
	for i := range in.Shares {
		share := &in.Shares[i]
		if share.UserID <= 0 || (i > 0 && share.UserID == in.Shares[i-1].UserID) {
			return in, invalid("share users must be unique positive IDs")
		}
		if in.SplitMode == "equal" {
			if share.Amount != "" {
				return in, invalid("equal split shares must omit amount_minor")
			}
			value := amount / int64(len(in.Shares))
			if int64(i) < amount%int64(len(in.Shares)) {
				value++
			}
			share.Amount = strconv.FormatInt(value, 10)
		} else {
			value, err := minor(share.Amount, true)
			if err != nil {
				return in, err
			}
			sum, err = checkedAdd(sum, value)
			if err != nil {
				return in, err
			}
		}
	}
	if in.SplitMode == "exact" && sum != amount {
		return in, invalid("exact shares must total the expense")
	}
	return in, nil
}
func mayWrite(role string, actor, creator int64) bool {
	return role == "owner" || role == "editor" || (role == "member" && actor == creator)
}

func (s *Service) save(ctx context.Context, actor, trip int64, k kind, id int64, in writeInput, key string) (Record, bool, error) {
	if err := s.authz.RequireParticipant(ctx, actor, trip); err != nil {
		return Record{}, false, err
	}
	if id == 0 && (len(key) == 0 || len(key) > 128 || strings.TrimSpace(key) != key) {
		return Record{}, false, invalid("Idempotency-Key (1 to 128 bytes) is required")
	}
	if (id == 0 && in.Version != 0) || (id != 0 && in.Version < 1) {
		return Record{}, false, invalid("version is required only when replacing a record")
	}
	in, err := validate(in, k)
	if err != nil {
		return Record{}, false, err
	}
	canonical, _ := json.Marshal(in)
	hash := sha256.Sum256(canonical)
	return s.store.Save(ctx, actor, trip, k, id, in, key, hash[:])
}
func (s *Service) list(ctx context.Context, actor, trip int64, k kind, currency string, limit, offset int) ([]Record, error) {
	if err := s.authz.RequireParticipant(ctx, actor, trip); err != nil {
		return nil, err
	}
	if currency != "" && !supported(currency) {
		return nil, invalid("unsupported currency")
	}
	return s.store.List(ctx, trip, k, currency, limit, offset)
}
func (s *Service) get(ctx context.Context, actor, trip int64, k kind, id int64) (Record, error) {
	if err := s.authz.RequireParticipant(ctx, actor, trip); err != nil {
		return Record{}, err
	}
	return s.store.Get(ctx, trip, k, id)
}
func (s *Service) remove(ctx context.Context, actor, trip int64, k kind, id, version int64, reason string) error {
	if err := s.authz.RequireParticipant(ctx, actor, trip); err != nil {
		return err
	}
	if version < 1 {
		return invalid("positive version is required")
	}
	return s.store.Delete(ctx, actor, trip, k, id, version, reason)
}
func (s *Service) balances(ctx context.Context, actor, trip int64) (Summary, error) {
	if err := s.authz.RequireParticipant(ctx, actor, trip); err != nil {
		return Summary{}, err
	}
	return s.store.Summary(ctx, trip)
}

func suggest(balances []Balance) ([]Transfer, error) {
	type remaining struct {
		id     int64
		amount uint64
	}
	debtors, creditors := []remaining{}, []remaining{}
	for _, b := range balances {
		v, err := strconv.ParseInt(b.Amount, 10, 64)
		if err != nil {
			return nil, invalid("balance exceeds the supported range")
		}
		if v < 0 {
			debtors = append(debtors, remaining{b.UserID, uint64(-(v + 1)) + 1})
		} else if v > 0 {
			creditors = append(creditors, remaining{b.UserID, uint64(v)})
		}
	}
	sort.Slice(debtors, func(i, j int) bool { return debtors[i].id < debtors[j].id })
	sort.Slice(creditors, func(i, j int) bool { return creditors[i].id < creditors[j].id })
	result := []Transfer{}
	for i, j := 0, 0; i < len(debtors) && j < len(creditors); {
		amount := min(debtors[i].amount, creditors[j].amount)
		result = append(result, Transfer{debtors[i].id, creditors[j].id, strconv.FormatUint(amount, 10)})
		debtors[i].amount -= amount
		creditors[j].amount -= amount
		if debtors[i].amount == 0 {
			i++
		}
		if creditors[j].amount == 0 {
			j++
		}
	}
	return result, nil
}
