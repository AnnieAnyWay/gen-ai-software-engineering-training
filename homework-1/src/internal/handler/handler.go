// Package handler exposes the HTTP API.
package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/AnnieAnyWay/banking-api/internal/auth"
	"github.com/AnnieAnyWay/banking-api/internal/docs"
	"github.com/AnnieAnyWay/banking-api/internal/model"
	"github.com/AnnieAnyWay/banking-api/internal/ratelimit"
	"github.com/AnnieAnyWay/banking-api/internal/store"
	"github.com/AnnieAnyWay/banking-api/internal/validation"
)

const maxBodyBytes = 1 << 20

// Deps are the services the API is built from.
type Deps struct {
	Store   *store.Store
	Users   *auth.Users
	Tokens  *auth.Tokens
	Limiter *ratelimit.Limiter // nil disables rate limiting
}

type API struct {
	store  *store.Store
	users  *auth.Users
	tokens *auth.Tokens
}

// New builds the router with all routes registered.
func New(d Deps) http.Handler {
	a := &API{store: d.Store, users: d.Users, tokens: d.Tokens}
	protect := d.Tokens.Require
	mux := http.NewServeMux()

	// public
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /openapi.yaml", docs.ServeSpec)
	mux.HandleFunc("GET /docs", docs.ServeUI)
	mux.HandleFunc("POST /auth/register", a.register)
	mux.HandleFunc("POST /auth/login", a.login)

	// authenticated
	mux.HandleFunc("GET /auth/me", protect(a.me))
	mux.HandleFunc("POST /accounts", protect(a.createAccount))
	mux.HandleFunc("GET /accounts", protect(a.listAccounts))
	mux.HandleFunc("POST /transactions", protect(a.createTransaction))
	mux.HandleFunc("GET /transactions", protect(a.listTransactions))
	mux.HandleFunc("GET /transactions/{id}", protect(a.getTransaction))
	mux.HandleFunc("GET /accounts/{accountId}/balance", protect(a.balance))
	mux.HandleFunc("GET /accounts/{accountId}/summary", protect(a.summary))

	var h http.Handler = mux
	if d.Limiter != nil {
		h = d.Limiter.Middleware(h)
	}
	return logRequests(recoverPanics(h))
}

func (a *API) createTransaction(w http.ResponseWriter, r *http.Request) {
	var req validation.CreateTransactionRequest
	if !decodeBody(w, r, &req) {
		return
	}
	v, errs := validation.Validate(req)
	if len(errs) > 0 {
		writeValidation(w, errs)
		return
	}
	// Ownership: you may only move money out of (or deposit into) your own
	// accounts. A transfer may go to any existing account.
	switch v.Type {
	case model.Deposit:
		if _, ok := a.authorizeAccount(w, r, v.ToAccount); !ok {
			return
		}
	default:
		if _, ok := a.authorizeAccount(w, r, v.FromAccount); !ok {
			return
		}
		if v.ToAccount != "" {
			if _, ok := a.store.Account(v.ToAccount); !ok {
				writeError(w, http.StatusNotFound, "Account not found: "+v.ToAccount)
				return
			}
		}
	}
	tx := a.store.Create(v)
	w.Header().Set("Location", "/transactions/"+tx.ID)
	writeJSON(w, http.StatusCreated, tx)
}

func (a *API) listTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := model.Filter{Owner: auth.UserFrom(r.Context())}
	var errs []validation.FieldError

	if acc := q.Get("accountId"); acc != "" {
		if !validation.ValidAccount(acc) {
			errs = append(errs, validation.FieldError{Field: "accountId", Message: "Account must match format ACC-XXXXX"})
		}
		f.AccountID = acc
	}
	if t := q.Get("type"); t != "" {
		switch model.TransactionType(t) {
		case model.Deposit, model.Withdrawal, model.Transfer:
			f.Type = model.TransactionType(t)
		default:
			errs = append(errs, validation.FieldError{Field: "type", Message: "Type must be one of: deposit, withdrawal, transfer"})
		}
	}
	var err error
	if s := q.Get("from"); s != "" {
		if f.From, _, err = parseDate(s); err != nil {
			errs = append(errs, validation.FieldError{Field: "from", Message: "Use YYYY-MM-DD or RFC 3339"})
		}
	}
	if s := q.Get("to"); s != "" {
		var dateOnly bool
		if f.To, dateOnly, err = parseDate(s); err != nil {
			errs = append(errs, validation.FieldError{Field: "to", Message: "Use YYYY-MM-DD or RFC 3339"})
		} else if dateOnly {
			f.To = f.To.AddDate(0, 0, 1) // a plain date includes the whole day
		} else {
			f.To = f.To.Add(time.Nanosecond) // RFC 3339 "to" is inclusive
		}
	}
	if !f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To) {
		errs = append(errs, validation.FieldError{Field: "from", Message: "from must be before to"})
	}
	if len(errs) > 0 {
		writeValidation(w, errs)
		return
	}
	if f.AccountID != "" {
		if _, ok := a.authorizeAccount(w, r, f.AccountID); !ok {
			return
		}
	}
	txs := a.store.List(f)
	writeJSON(w, http.StatusOK, map[string]any{"count": len(txs), "transactions": txs})
}

func (a *API) getTransaction(w http.ResponseWriter, r *http.Request) {
	tx, ok := a.store.Get(r.PathValue("id"))
	// Transactions that don't touch the caller's accounts are reported as
	// missing, so IDs of other users' transactions are never confirmed.
	if ok {
		user := auth.UserFrom(r.Context())
		from, _ := a.store.Account(tx.FromAccount)
		to, _ := a.store.Account(tx.ToAccount)
		ok = from.Owner == user || to.Owner == user
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Transaction not found")
		return
	}
	writeJSON(w, http.StatusOK, tx)
}

func (a *API) balance(w http.ResponseWriter, r *http.Request) {
	acc, ok := accountParam(w, r)
	if !ok {
		return
	}
	if _, ok := a.authorizeAccount(w, r, acc); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accountId": acc, "balances": a.store.Balances(acc)})
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	acc, ok := accountParam(w, r)
	if !ok {
		return
	}
	if _, ok := a.authorizeAccount(w, r, acc); !ok {
		return
	}
	writeJSON(w, http.StatusOK, a.store.Summary(acc))
}

func accountParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	acc := r.PathValue("accountId")
	if !validation.ValidAccount(acc) {
		writeValidation(w, []validation.FieldError{{Field: "accountId", Message: "Account must match format ACC-XXXXX"}})
		return "", false
	}
	return acc, true
}

// parseDate accepts 2024-01-31 or 2024-01-31T10:00:00Z.
func parseDate(s string) (time.Time, bool, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t, true, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	return t.UTC(), false, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeValidation(w http.ResponseWriter, details []validation.FieldError) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Validation failed", "details": details})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic: %v", v)
				writeError(w, http.StatusInternalServerError, "Internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.RequestURI(), rec.status, time.Since(start).Round(time.Microsecond))
	})
}
