package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/AnnieAnyWay/banking-api/internal/auth"
	"github.com/AnnieAnyWay/banking-api/internal/model"
	"github.com/AnnieAnyWay/banking-api/internal/validation"
)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decodeBody(w, r, &c) {
		return
	}
	if errs := auth.ValidateCredentials(c.Username, c.Password); len(errs) > 0 {
		fields := make([]string, 0, len(errs))
		for f := range errs {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		details := make([]validation.FieldError, 0, len(errs))
		for _, f := range fields {
			details = append(details, validation.FieldError{Field: f, Message: errs[f]})
		}
		writeValidation(w, details)
		return
	}
	u, err := a.users.Register(c.Username, c.Password)
	if errors.Is(err, auth.ErrUserExists) {
		writeError(w, http.StatusConflict, "Username already taken")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create user")
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decodeBody(w, r, &c) {
		return
	}
	u, err := a.users.Authenticate(c.Username, c.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}
	token, exp := a.tokens.Issue(u.Username)
	writeJSON(w, http.StatusOK, map[string]any{
		"accessToken": token,
		"tokenType":   "Bearer",
		"expiresIn":   int(time.Until(exp).Seconds()),
		"expiresAt":   exp.UTC(),
	})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"username": user, "accounts": a.store.AccountsOf(user)})
}

func (a *API) createAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if r.ContentLength != 0 && !decodeBody(w, r, &body) {
		return
	}
	if len(body.Name) > 64 {
		writeValidation(w, []validation.FieldError{{Field: "name", Message: "Name must be at most 64 characters"}})
		return
	}
	acc := a.store.CreateAccount(auth.UserFrom(r.Context()), strings.TrimSpace(body.Name))
	w.Header().Set("Location", "/accounts/"+acc.ID+"/balance")
	writeJSON(w, http.StatusCreated, acc)
}

func (a *API) listAccounts(w http.ResponseWriter, r *http.Request) {
	accs := a.store.AccountsOf(auth.UserFrom(r.Context()))
	writeJSON(w, http.StatusOK, map[string]any{"count": len(accs), "accounts": accs})
}

// authorizeAccount writes 404 for unknown accounts and 403 for other users' accounts.
func (a *API) authorizeAccount(w http.ResponseWriter, r *http.Request, id string) (model.Account, bool) {
	acc, ok := a.store.Account(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Account not found: "+id)
		return acc, false
	}
	if acc.Owner != auth.UserFrom(r.Context()) {
		writeError(w, http.StatusForbidden, "You do not own account "+id)
		return acc, false
	}
	return acc, true
}

// decodeBody parses a strict JSON body, writing a 400 on failure.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			writeValidation(w, []validation.FieldError{{Field: typeErr.Field, Message: "Field has the wrong type"}})
			return false
		}
		msg := "Invalid request body: " + err.Error()
		if err.Error() == "EOF" {
			msg = "Request body is empty"
		}
		writeError(w, http.StatusBadRequest, msg)
		return false
	}
	return true
}
