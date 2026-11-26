// Package store keeps transactions and balances in memory.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/AnnieAnyWay/banking-api/internal/model"
	"github.com/AnnieAnyWay/banking-api/internal/validation"
)

// Store is a concurrency-safe in-memory ledger.
type Store struct {
	mu       sync.RWMutex
	txs      []model.Transaction
	byID     map[string]int
	balances map[string]map[string]model.Money // account -> currency -> amount
	accounts map[string]model.Account          // account id -> account
	now      func() time.Time
}

func New() *Store {
	return &Store{
		byID:     map[string]int{},
		balances: map[string]map[string]model.Money{},
		accounts: map[string]model.Account{},
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Create records a transaction. Withdrawals and transfers that exceed the
// source balance are stored with status "failed" so the history stays complete.
func (s *Store) Create(v validation.Validated) model.Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx := model.Transaction{
		ID:          newID(),
		FromAccount: v.FromAccount,
		ToAccount:   v.ToAccount,
		Amount:      v.Amount,
		Currency:    v.Currency,
		Type:        v.Type,
		Timestamp:   s.now(),
		Status:      model.StatusCompleted,
	}
	if tx.FromAccount != "" && s.balances[tx.FromAccount][tx.Currency] < tx.Amount {
		tx.Status = model.StatusFailed
		tx.FailureReason = "insufficient funds"
	} else {
		if tx.FromAccount != "" {
			s.adjust(tx.FromAccount, tx.Currency, -tx.Amount)
		}
		if tx.ToAccount != "" {
			s.adjust(tx.ToAccount, tx.Currency, tx.Amount)
		}
	}
	s.byID[tx.ID] = len(s.txs)
	s.txs = append(s.txs, tx)
	return tx
}

func (s *Store) adjust(acc, cur string, delta model.Money) {
	if s.balances[acc] == nil {
		s.balances[acc] = map[string]model.Money{}
	}
	s.balances[acc][cur] += delta
}

func (s *Store) Get(id string) (model.Transaction, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := s.byID[id]
	if !ok {
		return model.Transaction{}, false
	}
	return s.txs[i], true
}

// List returns transactions matching f, oldest first.
func (s *Store) List(f model.Filter) []model.Transaction {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Transaction{}
	for _, tx := range s.txs {
		if f.AccountID != "" && tx.FromAccount != f.AccountID && tx.ToAccount != f.AccountID {
			continue
		}
		if f.Owner != "" && s.accounts[tx.FromAccount].Owner != f.Owner && s.accounts[tx.ToAccount].Owner != f.Owner {
			continue
		}
		if f.Type != "" && tx.Type != f.Type {
			continue
		}
		if !f.From.IsZero() && tx.Timestamp.Before(f.From) {
			continue
		}
		if !f.To.IsZero() && !tx.Timestamp.Before(f.To) {
			continue
		}
		out = append(out, tx)
	}
	return out
}

// Balances returns a copy of the account's balance per currency.
func (s *Store) Balances(acc string) map[string]model.Money {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]model.Money{}
	for c, m := range s.balances[acc] {
		out[c] = m
	}
	return out
}

// CreateAccount opens a new account with a random ACC-XXXXX id for owner.
func (s *Store) CreateAccount(owner, name string) model.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		id := "ACC-" + randomAlnum(5)
		if _, taken := s.accounts[id]; !taken {
			a := model.Account{ID: id, Owner: owner, Name: name, CreatedAt: s.now()}
			s.accounts[id] = a
			return a
		}
	}
}

// Account looks up an account by id.
func (s *Store) Account(id string) (model.Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[id]
	return a, ok
}

// AccountsOf returns the ids of all accounts owned by owner, sorted.
func (s *Store) AccountsOf(owner string) []model.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Account{}
	for _, a := range s.accounts {
		if a.Owner == owner {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CurrencyTotals is the per-currency part of an account summary.
type CurrencyTotals struct {
	TotalDeposits    model.Money `json:"totalDeposits"`
	TotalWithdrawals model.Money `json:"totalWithdrawals"`
	TotalIn          model.Money `json:"totalTransfersIn"`
	TotalOut         model.Money `json:"totalTransfersOut"`
}

// Summary aggregates completed transactions for an account.
type Summary struct {
	AccountID             string                    `json:"accountId"`
	TransactionCount      int                       `json:"transactionCount"`
	MostRecentTransaction *time.Time                `json:"mostRecentTransactionDate"`
	ByCurrency            map[string]CurrencyTotals `json:"byCurrency"`
}

func (s *Store) Summary(acc string) Summary {
	txs := s.List(model.Filter{AccountID: acc})
	sum := Summary{AccountID: acc, TransactionCount: len(txs), ByCurrency: map[string]CurrencyTotals{}}
	for _, tx := range txs {
		t := tx.Timestamp
		if sum.MostRecentTransaction == nil || t.After(*sum.MostRecentTransaction) {
			sum.MostRecentTransaction = &t
		}
		if tx.Status != model.StatusCompleted {
			continue
		}
		ct := sum.ByCurrency[tx.Currency]
		switch {
		case tx.Type == model.Deposit:
			ct.TotalDeposits += tx.Amount
		case tx.Type == model.Withdrawal:
			ct.TotalWithdrawals += tx.Amount
		case tx.FromAccount == acc:
			ct.TotalOut += tx.Amount
		default:
			ct.TotalIn += tx.Amount
		}
		sum.ByCurrency[tx.Currency] = ct
	}
	return sum
}

const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomAlnum(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alnum[int(b[i])%len(alnum)]
	}
	return string(b)
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40 // UUID v4
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
