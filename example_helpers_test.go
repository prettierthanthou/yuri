package yuri_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"

	"codeberg.org/lewdest/yuri"
)

// fakeBitcoinNode mocks bitcoind for the sake of the examples
// this implements the smallest subset of bitcoind for yuri to work
func fakeBitcoinNode() (provider yuri.CryptoProvider, pay func()) {
	const invoiceAddress = "bcrt1qfakeprovideraddressforinvoice"

	var (
		mu   sync.Mutex
		paid bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req struct {
			Id     string `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		state := paid
		mu.Unlock()

		var result any
		switch req.Method {
		case "getnewaddress":
			result = invoiceAddress

		case "listunspent":
			result = []any{}
			if state {
				// 0.00010500 BTC, the exact amount a $10.50 invoice
				// at $100,000 per BTC works out to.
				result = []map[string]any{{
					"address":       invoiceAddress,
					"amount":        "0.00010500",
					"confirmations": 6,
				}}
			}

		default:
			http.Error(w, "unexpected method: "+req.Method, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.Id,
			"result":  result,
		})
	}))

	pay = func() {
		mu.Lock()
		paid = true
		mu.Unlock()
	}

	provider = yuri.NewBitcoin(yuri.JsonRpcClientConfig{
		Host: srv.URL,
	})

	return provider, pay
}

// exampleMapStorage is a minimal [yuri.Storage] implementation that
// keeps invoices in a map. it is used by [Example_storage] to show
// what bringing your own storage looks like.
type exampleMapStorage struct {
	mu   sync.Mutex
	invs map[yuri.Chain][]yuri.Invoice
}

// NewInvoice implements [yuri.Storage].
func (s *exampleMapStorage) NewInvoice(_ context.Context, inv yuri.Invoice) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.invs == nil {
		s.invs = map[yuri.Chain][]yuri.Invoice{}
	}

	s.invs[inv.Chain] = append(s.invs[inv.Chain], inv)

	return nil
}

// GetActiveInvoices implements [yuri.Storage].
func (s *exampleMapStorage) GetActiveInvoices(_ context.Context, chain yuri.Chain) ([]yuri.Invoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]yuri.Invoice, 0, len(s.invs[chain]))
	for _, inv := range s.invs[chain] {
		// clone so a caller mutating the invoice cannot corrupt
		// our stored state.
		out = append(out, inv.Clone())
	}

	return out, nil
}

// UpdateInvoices implements [yuri.Storage].
func (s *exampleMapStorage) UpdateInvoices(_ context.Context, invoices []yuri.Invoice) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, inv := range invoices {
		idx := -1
		for i, existing := range s.invs[inv.Chain] {
			if existing.Address == inv.Address {
				idx = i
				break
			}
		}

		if idx == -1 {
			return errors.New("invoice not found")
		}

		// paid invoices are done, no point keeping them active.
		if inv.Paid() {
			s.invs[inv.Chain] = append(s.invs[inv.Chain][:idx], s.invs[inv.Chain][idx+1:]...)
			continue
		}

		s.invs[inv.Chain][idx] = inv
	}

	return nil
}
