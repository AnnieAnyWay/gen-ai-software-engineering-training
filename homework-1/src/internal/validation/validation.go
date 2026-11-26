// Package validation checks incoming transaction requests.
package validation

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/AnnieAnyWay/banking-api/internal/model"
)

// FieldError describes one invalid field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// CreateTransactionRequest is the POST /transactions body.
// Amount is kept raw so we can reject strings and check decimal places exactly.
type CreateTransactionRequest struct {
	FromAccount string          `json:"fromAccount"`
	ToAccount   string          `json:"toAccount"`
	Amount      json.RawMessage `json:"amount"`
	Currency    string          `json:"currency"`
	Type        string          `json:"type"`
}

var (
	accountRe = regexp.MustCompile(`^ACC-[A-Za-z0-9]{5}$`)
	amountRe  = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)
)

// ISO 4217 codes accepted by the API.
var currencies = map[string]bool{
	"USD": true, "EUR": true, "GBP": true, "JPY": true, "CHF": true,
	"CAD": true, "AUD": true, "NZD": true, "CNY": true, "SEK": true,
	"NOK": true, "DKK": true, "PLN": true, "CZK": true, "HUF": true,
	"UAH": true, "INR": true, "BRL": true, "MXN": true, "SGD": true,
	"HKD": true, "KRW": true, "TRY": true, "ZAR": true, "ILS": true,
}

// ValidAccount reports whether id matches ACC-XXXXX.
func ValidAccount(id string) bool { return accountRe.MatchString(id) }

// ValidCurrency reports whether code is a supported ISO 4217 code.
func ValidCurrency(code string) bool { return currencies[code] }

// Validated is a request that passed validation.
type Validated struct {
	FromAccount string
	ToAccount   string
	Amount      model.Money
	Currency    string
	Type        model.TransactionType
}

// Validate checks every field and returns all problems at once.
func Validate(r CreateTransactionRequest) (Validated, []FieldError) {
	var errs []FieldError
	add := func(f, m string) { errs = append(errs, FieldError{f, m}) }
	v := Validated{FromAccount: r.FromAccount, ToAccount: r.ToAccount, Currency: strings.ToUpper(r.Currency)}

	// amount
	amt := strings.TrimSpace(string(r.Amount))
	switch {
	case amt == "" || amt == "null":
		add("amount", "Amount is required")
	case !json.Valid(r.Amount) || strings.ContainsAny(amt[:1], "\"[{tf"):
		add("amount", "Amount must be a number")
	case strings.ContainsAny(amt, "eE"):
		add("amount", "Amount must be a plain decimal number")
	case strings.HasPrefix(amt, "-") || !isPositive(amt):
		add("amount", "Amount must be a positive number")
	case !amountRe.MatchString(amt):
		add("amount", "Amount must have at most 2 decimal places")
	default:
		if m, ok := toMinor(amt); ok {
			v.Amount = m
		} else {
			add("amount", "Amount is too large")
		}
	}

	// currency
	if r.Currency == "" {
		add("currency", "Currency is required")
	} else if !ValidCurrency(v.Currency) {
		add("currency", "Invalid currency code")
	}

	// type + accounts
	t := model.TransactionType(r.Type)
	v.Type = t
	needFrom, needTo := false, false
	switch t {
	case model.Deposit:
		needTo = true
	case model.Withdrawal:
		needFrom = true
	case model.Transfer:
		needFrom, needTo = true, true
	case "":
		add("type", "Type is required")
	default:
		add("type", "Type must be one of: deposit, withdrawal, transfer")
	}
	checkAccount := func(field, val string, needed bool) {
		switch {
		case val == "" && needed:
			add(field, field+" is required for "+r.Type)
		case val != "" && !needed && t != "":
			add(field, field+" must not be set for "+r.Type)
		case val != "" && !ValidAccount(val):
			add(field, "Account must match format ACC-XXXXX (X is alphanumeric)")
		}
	}
	checkAccount("fromAccount", r.FromAccount, needFrom)
	checkAccount("toAccount", r.ToAccount, needTo)
	if t == model.Transfer && r.FromAccount != "" && r.FromAccount == r.ToAccount {
		add("toAccount", "toAccount must differ from fromAccount")
	}
	return v, errs
}

func isPositive(s string) bool {
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f > 0
}

// toMinor converts "12.3" to 1230 without floating point.
func toMinor(s string) (model.Money, bool) {
	whole, frac, _ := strings.Cut(s, ".")
	frac = (frac + "00")[:2]
	if len(whole) > 13 { // keep well inside int64
		return 0, false
	}
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	return model.Money(n), err == nil
}
