package yuri

import (
	"context"
	"math/big"
	"testing"
)

func TestGetActiveInvoicesDoesNotShareBigIntState(t *testing.T) {
	storage := &InMemoryStorage{}

	original := Invoice{
		Chain:      "test",
		Address:    "addr1",
		AmountOwed: big.NewInt(500),
		AmountPaid: big.NewInt(0),
	}

	if err := storage.NewInvoice(context.Background(), original); err != nil {
		t.Fatal(err)
	}

	invoices, err := storage.GetActiveInvoices(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	if len(invoices) == 0 {
		t.Fatal("expected at least one invoice")
	}

	var retrieved *Invoice
	for i := range invoices {
		if invoices[i].Address == "addr1" {
			retrieved = &invoices[i]
			break
		}
	}

	if retrieved == nil {
		t.Fatal("could not find invoice with address addr1")
	}

	retrieved.AmountPaid.SetInt64(999)

	invoices2, err := storage.GetActiveInvoices(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	var stored *Invoice
	for i := range invoices2 {
		if invoices2[i].Address == "addr1" {
			stored = &invoices2[i]
			break
		}
	}

	if stored == nil {
		t.Fatal("could not find invoice with address addr1 on second fetch")
	}

	if stored.AmountPaid.Int64() != 0 {
		t.Fatalf("AmountPaid was mutated through GetActiveInvoices: got %d want 0", stored.AmountPaid.Int64())
	}
}
