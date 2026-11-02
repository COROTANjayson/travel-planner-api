package expenses

import "time"

type Currency struct {
	Code     string `json:"code"`
	Decimals int    `json:"decimals"`
}

// ISO 4217 minor units; source: SIX List One. No exchange rates are used.
var currencies = []Currency{{"PHP", 2}, {"USD", 2}, {"EUR", 2}, {"GBP", 2}, {"JPY", 0}, {"SGD", 2}, {"THB", 2}, {"VND", 0}, {"IDR", 2}, {"MYR", 2}, {"AUD", 2}, {"CAD", 2}, {"CNY", 2}, {"HKD", 2}, {"KRW", 0}, {"TWD", 2}, {"CHF", 2}}

type Person struct {
	UserID      int64   `json:"user_id"`
	DisplayName string  `json:"display_name"`
	Email       *string `json:"email"`
}
type Share struct {
	UserID int64  `json:"user_id"`
	Amount string `json:"amount_minor"`
}
type ExpenseInput struct {
	PlannedCovererID   int64   `json:"planned_coverer_user_id,omitempty"`
	PlannedCoverAmount string  `json:"planned_cover_amount_minor,omitempty"`
	ActivityID         int64   `json:"activity_id,omitempty"`
	AmountBasis        string  `json:"amount_basis,omitempty"`
	Description        string  `json:"description"`
	Currency           string  `json:"currency"`
	Amount             string  `json:"amount_minor"`
	Date               string  `json:"incurred_on"`
	PayerID            int64   `json:"payer_user_id"`
	SplitMode          string  `json:"split_mode"`
	Shares             []Share `json:"shares"`
	Version            int64   `json:"version,omitempty"`
}
type SettlementInput struct {
	CorrectionReason string `json:"correction_reason,omitempty"`
	PaymentID        int64  `json:"payment_id,omitempty"`
	LoanID           int64  `json:"loan_id,omitempty"`
	Currency         string `json:"currency"`
	Amount           string `json:"amount_minor"`
	Date             string `json:"paid_on"`
	FromID           int64  `json:"from_user_id"`
	ToID             int64  `json:"to_user_id"`
	Version          int64  `json:"version,omitempty"`
}
type Record struct {
	PlannedCovererID   int64     `json:"planned_coverer_user_id,omitempty"`
	PlannedCoverAmount string    `json:"planned_cover_amount_minor,omitempty"`
	ActivityID         int64     `json:"activity_id,omitempty"`
	ActivityName       string    `json:"activity_name,omitempty"`
	AmountBasis        string    `json:"amount_basis,omitempty"`
	ExpenseID          int64     `json:"expense_id,omitempty"`
	PaymentID          int64     `json:"payment_id,omitempty"`
	LoanID             int64     `json:"loan_id,omitempty"`
	Allocations        []Share   `json:"allocations,omitempty"`
	ID                 int64     `json:"id"`
	TripID             int64     `json:"trip_id"`
	CreatorID          int64     `json:"created_by_user_id"`
	Currency           string    `json:"currency"`
	Amount             string    `json:"amount_minor"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Description        string    `json:"description,omitempty"`
	Date               string    `json:"incurred_on,omitempty"`
	PayerID            int64     `json:"payer_user_id,omitempty"`
	SplitMode          string    `json:"split_mode,omitempty"`
	Shares             []Share   `json:"shares,omitempty"`
	PaidOn             string    `json:"paid_on,omitempty"`
	FromID             int64     `json:"from_user_id,omitempty"`
	ToID               int64     `json:"to_user_id,omitempty"`
	People             []Person  `json:"people"`
}
type Balance struct {
	UserID int64  `json:"user_id"`
	Amount string `json:"amount_minor"`
}
type Transfer struct {
	FromID int64  `json:"from_user_id"`
	ToID   int64  `json:"to_user_id"`
	Amount string `json:"amount_minor"`
}
type CurrencyBalance struct {
	Currency  string     `json:"currency"`
	Total     string     `json:"total_spent_minor"`
	Balances  []Balance  `json:"balances"`
	Transfers []Transfer `json:"suggested_transfers"`
}
type Summary struct {
	Currencies []CurrencyBalance `json:"currencies"`
	People     []Person          `json:"people"`
}

type kind string

const expense kind = "expenses"
const settlement kind = "settlements"
const businessPayment kind = "expense_payments"
const loan kind = "expense_loans"

type writeInput struct {
	MemberID         int64  `json:"member_user_id,omitempty"`
	CorrectionReason string `json:"correction_reason,omitempty"`
	ExpenseInput
	FromID      int64
	ToID        int64
	ExpenseID   int64
	PaymentID   int64
	LoanID      int64
	Allocations []Share
}

type ReceiptHistory struct {
	ID        int64     `json:"id"`
	ActorID   int64     `json:"actor_user_id"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	Before    *Record   `json:"before"`
	After     *Record   `json:"after"`
	CreatedAt time.Time `json:"created_at"`
}

type TransactionInput struct {
	MemberID    int64   `json:"member_user_id,omitempty"`
	ActivityID  int64   `json:"activity_id,omitempty"`
	ExpenseID   int64   `json:"expense_id,omitempty"`
	Description string  `json:"description,omitempty"`
	Currency    string  `json:"currency"`
	Amount      string  `json:"amount_minor"`
	Date        string  `json:"paid_on"`
	PayerID     int64   `json:"payer_user_id,omitempty"`
	FromID      int64   `json:"from_user_id,omitempty"`
	ToID        int64   `json:"to_user_id,omitempty"`
	Allocations []Share `json:"allocations,omitempty"`
	Version     int64   `json:"version,omitempty"`
}

type Debt struct {
	PaymentID    int64  `json:"payment_id,omitempty"`
	LoanID       int64  `json:"loan_id,omitempty"`
	ActivityID   int64  `json:"activity_id,omitempty"`
	ActivityName string `json:"activity_name"`
	Description  string `json:"description"`
	Currency     string `json:"currency"`
	FromID       int64  `json:"from_user_id"`
	ToID         int64  `json:"to_user_id"`
	Amount       string `json:"amount_minor"`
}

type Ledger struct {
	ReversedSettlements []Record `json:"reversed_settlements"`
	Expenses            []Record `json:"expenses"`
	Payments            []Record `json:"payments"`
	Loans               []Record `json:"loans"`
	Settlements         []Record `json:"settlements"`
	People              []Person `json:"people"`
	Debts               []Debt   `json:"debts"`
}
