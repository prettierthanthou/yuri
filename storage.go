package yuri

import (
	"context"
	"errors"
	"math/big"
	"slices"
	"sync"
)

// Invoice represents the smallest state an invoice can be.
// You are required to persist the following:
// [Invoice.Chain], [Invoice.Address], [Invoice.AmountOwed, [Invoice.AmountPaid], [Invoice.Token]
type Invoice struct {
	Chain      Chain
	Address    string
	AmountOwed *big.Int
	AmountPaid *big.Int
	Token      Token
	// if the amountPaid amount has not reached the required confirmations
	Pending bool

	// user provided metadata, typically an ID to allow for
	// quickly locating and updating the Invoice via [Storage.UpdateInvoice]
	Metadata map[string]any
}

// Paid determines if an Invoice is fully paid, and accounts for pending funds.
//
// AmountPaid >= AmountOwed, but one or more
// contributing transfers have not reached
// the required confirmation threshold.
//
// NOTE: depending on the CryptoProvider's implementation this may vary.
// Some implementations are naive and assume if there is any pending transaction
// to the addr that is confirming, even if the addr is >=AmountOwed in balance
// then the entire balance is pending.
//
// This is behaviour you should expect!
func (i Invoice) Paid() bool {
	if i.Pending {
		// if we are pending at all we are in fact
		// not fully paid and ready to act on this.
		return false
	}

	s := i.AmountPaid.Cmp(i.AmountOwed)
	// -1 if x < y;
	// 0 if x == y;
	// +1 if x > y.
	return s == 0 || s == 1
}

func (i Invoice) Clone() Invoice {
	owedCopied := big.NewInt(0).Set(i.AmountOwed)
	paidCopied := big.NewInt(0).Set(i.AmountPaid)
	return Invoice{
		Chain:      i.Chain,
		Address:    i.Address,
		AmountOwed: owedCopied,
		AmountPaid: paidCopied,
		Token:      i.Token,
		Pending:    i.Pending,
		Metadata:   i.Metadata,
	}
}

type InvoiceCreate struct {
	Chain      Chain
	Token      Token
	AmountFiat fiat
	Metadata   map[string]any
}

// Storage represents a user defined storage for storing invoices.
// All methods on Storage must be safe to call in any goroutine.
type Storage interface {
	GetActiveInvoices(context.Context, Chain) ([]Invoice, error)
	UpdateInvoices(context.Context, []Invoice) error
	// Please view Invoice to see what you are required to persist.
	NewInvoice(context.Context, Invoice) error
}

var _ Storage = &InMemoryStorage{}

// InMemoryStorage implements [Storage] in process memory,
// this should be used for exclusively testing, as it does
// not attempt to persist and heavily abuses mutexes.
type InMemoryStorage struct {
	activeInvoices map[Chain][]Invoice
	mu             sync.Mutex
}

// GetActiveInvoices implements [Storage].
func (i *InMemoryStorage) GetActiveInvoices(_ context.Context, chain Chain) ([]Invoice, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	invs := i.activeInvoices[chain]

	out := make([]Invoice, len(invs))
	copy(out, invs)

	return out, nil
}

// NewInvoice implements [Storage].
func (i *InMemoryStorage) NewInvoice(_ context.Context, inv Invoice) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.activeInvoices == nil {
		i.activeInvoices = map[Chain][]Invoice{}
	}

	invs := i.activeInvoices[inv.Chain]
	if invs == nil {
		invs = []Invoice{inv}
	} else {
		invs = append(invs, inv)
	}

	i.activeInvoices[inv.Chain] = invs

	return nil
}

// UpdateInvoices implements [Storage].
func (i *InMemoryStorage) UpdateInvoices(_ context.Context, invoices []Invoice) error {
	// panic("unimplemented")
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, inv := range invoices {
		idx := -1
		for it, e := range i.activeInvoices[inv.Chain] {
			if e.Address != inv.Address || e.Token != inv.Token {
				continue
			}

			idx = it
			break
		}

		if idx == -1 {
			return errors.New("invoice does not exist")
		}

		if inv.Paid() {
			i.activeInvoices[inv.Chain] = slices.Delete(i.activeInvoices[inv.Chain], idx, idx+1)
			continue
		}

		i.activeInvoices[inv.Chain][idx] = inv
	}

	return nil
}
