// Package model defines the domain types of the banking API.
package model

import "time"

type TransactionType string

const (
	Deposit    TransactionType = "deposit"
	Withdrawal TransactionType = "withdrawal"
	Transfer   TransactionType = "transfer"
)

type TransactionStatus string

const (
	StatusPending   TransactionStatus = "pending"
	StatusCompleted TransactionStatus = "completed"
	StatusFailed    TransactionStatus = "failed"
)

// Transaction is a single money movement.
// Amount is stored in minor units (cents) to avoid floating point errors,
// and exposed as a decimal number in JSON.
type Transaction struct {
	ID            string            `json:"id"`
	FromAccount   string            `json:"fromAccount,omitempty"`
	ToAccount     string            `json:"toAccount,omitempty"`
	Amount        Money             `json:"amount"`
	Currency      string            `json:"currency"`
	Type          TransactionType   `json:"type"`
	Timestamp     time.Time         `json:"timestamp"`
	Status        TransactionStatus `json:"status"`
	FailureReason string            `json:"failureReason,omitempty"`
}

// Filter narrows a transaction listing. Zero values mean "no filter".
type Filter struct {
	AccountID string
	Owner     string // only transactions touching this user's accounts
	Type      TransactionType
	From      time.Time // inclusive
	To        time.Time // exclusive
}

// Account is a bank account owned by a user.
type Account struct {
	ID        string    `json:"id"`
	Owner     string    `json:"owner"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}
