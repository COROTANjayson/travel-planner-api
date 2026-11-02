package expenses

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"travel-planner/travel-planner-api/internal/auth"
	"travel-planner/travel-planner-api/internal/httpx"
)

func Register(r chi.Router, s *Service) {
	r.Get("/expense-currencies", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.CurrentUser(r.Context()); !ok {
			httpx.JSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		httpx.JSON(w, 200, currencies)
	})
	r.Get("/trips/{tripID}/balances", func(w http.ResponseWriter, r *http.Request) {
		actor, trip, ok := requestIDs(w, r)
		if !ok {
			return
		}
		result, err := s.balances(r.Context(), actor, trip)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, 200, result)
	})
	r.Get("/trips/{tripID}/expense-ledger", func(w http.ResponseWriter, r *http.Request) {
		actor, trip, ok := requestIDs(w, r)
		if !ok {
			return
		}
		if err := s.authz.RequireParticipant(r.Context(), actor, trip); err != nil {
			httpx.Error(w, err)
			return
		}
		result, err := s.store.Ledger(r.Context(), trip)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, 200, result)
	})
	r.Get("/trips/{tripID}/settlements/{recordID}/history", func(w http.ResponseWriter, r *http.Request) {
		actor, trip, ok := requestIDs(w, r)
		if !ok {
			return
		}
		id, err := httpx.ID(r, "recordID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := s.authz.RequireParticipant(r.Context(), actor, trip); err != nil {
			httpx.Error(w, err)
			return
		}
		result, err := s.store.ReceiptHistory(r.Context(), trip, id)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, 200, result)
	})

	r.Get("/trips/{tripID}/expenses/{recordID}/payment-status", func(w http.ResponseWriter, r *http.Request) {
		actor, trip, ok := requestIDs(w, r)
		if !ok {
			return
		}
		id, err := httpx.ID(r, "recordID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := s.authz.RequireParticipant(r.Context(), actor, trip); err != nil {
			httpx.Error(w, err)
			return
		}
		result, err := s.store.PaymentStatus(r.Context(), trip, id)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, 200, result)
	})
	r.Post("/trips/{tripID}/expenses/{recordID}/payment-preview", func(w http.ResponseWriter, r *http.Request) {
		actor, trip, ok := requestIDs(w, r)
		if !ok {
			return
		}
		id, err := httpx.ID(r, "recordID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := s.authz.RequireParticipant(r.Context(), actor, trip); err != nil {
			httpx.Error(w, err)
			return
		}
		var input TransactionInput
		if err := httpx.Decode(w, r, &input); err != nil {
			httpx.Error(w, err)
			return
		}
		in, err := validate(writeInput{ExpenseInput: ExpenseInput{Currency: input.Currency, Amount: input.Amount, Date: input.Date, PayerID: input.PayerID}, ExpenseID: id, MemberID: input.MemberID, Allocations: input.Allocations}, businessPayment)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		result, err := s.store.PreviewPayment(r.Context(), actor, trip, in)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, 200, result)
	})
	r.Post("/trips/{tripID}/expenses/{recordID}/cost-preview", func(w http.ResponseWriter, r *http.Request) {
		actor, trip, ok := requestIDs(w, r)
		if !ok {
			return
		}
		id, err := httpx.ID(r, "recordID")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if err := s.authz.RequireParticipant(r.Context(), actor, trip); err != nil {
			httpx.Error(w, err)
			return
		}
		var input ExpenseInput
		if err := httpx.Decode(w, r, &input); err != nil {
			httpx.Error(w, err)
			return
		}
		in, err := validate(writeInput{ExpenseInput: input}, expense)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		result, err := s.store.PreviewCost(r.Context(), actor, trip, id, in)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, 200, result)
	})
	for _, k := range []kind{expense, settlement, businessPayment, loan} {
		r.Route("/trips/{tripID}/"+string(k), func(r chi.Router) {
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				actor, trip, ok := requestIDs(w, r)
				if !ok {
					return
				}
				limit, offset, err := httpx.Pagination(r)
				if err != nil {
					httpx.Error(w, err)
					return
				}
				result, err := s.list(r.Context(), actor, trip, k, r.URL.Query().Get("currency"), limit, offset)
				if err != nil {
					httpx.Error(w, err)
					return
				}
				httpx.JSON(w, 200, result)
			})
			r.Get("/{recordID}", func(w http.ResponseWriter, r *http.Request) {
				actor, trip, ok := requestIDs(w, r)
				if !ok {
					return
				}
				id, err := httpx.ID(r, "recordID")
				if err != nil {
					httpx.Error(w, err)
					return
				}
				result, err := s.get(r.Context(), actor, trip, k, id)
				if err != nil {
					httpx.Error(w, err)
					return
				}
				httpx.JSON(w, 200, result)
			})
			for _, method := range []string{http.MethodPost, http.MethodPut} {
				path := "/"
				if method == http.MethodPut {
					path = "/{recordID}"
				}
				r.MethodFunc(method, path, func(w http.ResponseWriter, r *http.Request) {
					actor, trip, ok := requestIDs(w, r)
					if !ok {
						return
					}
					var id int64
					var err error
					if method == http.MethodPut {
						id, err = httpx.ID(r, "recordID")
						if err != nil {
							httpx.Error(w, err)
							return
						}
					}
					var in writeInput
					if k == expense {
						err = httpx.Decode(w, r, &in.ExpenseInput)
					} else if k == businessPayment || k == loan {
						var t TransactionInput
						err = httpx.Decode(w, r, &t)
						in = writeInput{ExpenseInput: ExpenseInput{ActivityID: t.ActivityID, Description: t.Description, Currency: t.Currency, Amount: t.Amount, Date: t.Date, PayerID: t.PayerID, Version: t.Version}, MemberID: t.MemberID, ExpenseID: t.ExpenseID, FromID: t.FromID, ToID: t.ToID, Allocations: t.Allocations}
					} else {
						var payment SettlementInput
						err = httpx.Decode(w, r, &payment)
						in = writeInput{ExpenseInput: ExpenseInput{Currency: payment.Currency, Amount: payment.Amount, Date: payment.Date, Version: payment.Version}, FromID: payment.FromID, ToID: payment.ToID, CorrectionReason: payment.CorrectionReason, PaymentID: payment.PaymentID, LoanID: payment.LoanID}
					}
					if err != nil {
						httpx.Error(w, err)
						return
					}
					result, created, err := s.save(r.Context(), actor, trip, k, id, in, r.Header.Get("Idempotency-Key"))
					if err != nil {
						httpx.Error(w, err)
						return
					}
					status := 200
					if created {
						status = 201
						w.Header().Set("Location", fmt.Sprintf("/api/v1/trips/%d/%s/%d", trip, k, result.ID))
					}
					httpx.JSON(w, status, result)
				})
			}
			r.Delete("/{recordID}", func(w http.ResponseWriter, r *http.Request) {
				actor, trip, ok := requestIDs(w, r)
				if !ok {
					return
				}
				id, err := httpx.ID(r, "recordID")
				if err != nil {
					httpx.Error(w, err)
					return
				}
				version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
				if err != nil {
					httpx.Error(w, invalid("positive version is required"))
					return
				}
				if err := s.remove(r.Context(), actor, trip, k, id, version, r.URL.Query().Get("correction_reason")); err != nil {
					httpx.Error(w, err)
					return
				}
				w.WriteHeader(204)
			})
		})
	}
}
func requestIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		httpx.JSON(w, 401, map[string]string{"error": "unauthorized"})
		return 0, 0, false
	}
	trip, err := httpx.ID(r, "tripID")
	if err != nil {
		httpx.Error(w, err)
		return 0, 0, false
	}
	return user.ID, trip, true
}
