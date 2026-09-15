package yurid

import (
	"context"
	"math/big"
	"testing"
	"time"

	"codeberg.org/lewdest/yuri"
)

func newTestDB(t *testing.T) *database {
	t.Helper()
	db, err := NewDatabase(DatabaseConfig{Type: DatabaseTypeSqlite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	db.db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGetActiveInvoices_FiltersPaidNonPending(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// insert a partially paid invoice should be returned.
	_, err := db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0x1",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(500),
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert partial: %v", err)
	}

	// insert a fully paid, non-pending invoice should be excluded.
	_, err = db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0x2",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(1000),
		Pending:    false,
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert paid: %v", err)
	}

	// insert a fully paid but still pending invoice should be returned.
	_, err = db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0x3",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(1000),
		Pending:    true,
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert pending+paid: %v", err)
	}

	invoices, err := db.GetActiveInvoices(ctx, yuri.Ethereum)
	if err != nil {
		t.Fatalf("GetActiveInvoices: %v", err)
	}

	if len(invoices) != 2 {
		t.Fatalf("got %d invoices, want 2", len(invoices))
	}

	addrs := map[string]bool{}
	for _, inv := range invoices {
		addrs[inv.Address] = true
	}
	if !addrs["0x1"] {
		t.Error("partially paid invoice not returned")
	}
	if !addrs["0x3"] {
		t.Error("pending+paid invoice not returned")
	}
	if addrs["0x2"] {
		t.Error("fully paid non-pending invoice should be excluded")
	}
}

func TestGetActiveInvoices_ExcludesExpired(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// expired invoice
	_, err := db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0xexpired",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(0),
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("insert expired: %v", err)
	}

	// non-expired invoice
	_, err = db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0xactive",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(0),
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert active: %v", err)
	}

	invoices, err := db.GetActiveInvoices(ctx, yuri.Ethereum)
	if err != nil {
		t.Fatalf("GetActiveInvoices: %v", err)
	}

	if len(invoices) != 1 {
		t.Fatalf("got %d invoices, want 1", len(invoices))
	}
	if invoices[0].Address != "0xactive" {
		t.Errorf("got address %s, want 0xactive", invoices[0].Address)
	}
}

func TestGetActiveInvoices_ChainFiltering(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0xeth",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(0),
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert eth: %v", err)
	}

	_, err = db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Bitcoin,
		Address:    "btc1",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(0),
		Token:      yuri.Token{Symbol: "BTC", Decimals: 8},
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert btc: %v", err)
	}

	invoices, err := db.GetActiveInvoices(ctx, yuri.Ethereum)
	if err != nil {
		t.Fatalf("GetActiveInvoices: %v", err)
	}

	if len(invoices) != 1 {
		t.Fatalf("got %d invoices, want 1", len(invoices))
	}
	if invoices[0].Chain != yuri.Ethereum {
		t.Errorf("got chain %s, want ethereum", invoices[0].Chain)
	}
}

func TestGetActiveInvoices_ZeroAmounts(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// invoice with 0 paid and 0 owed should be excluded (0 >= 0).
	_, err := db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0xzero",
		AmountOwed: big.NewInt(0),
		AmountPaid: big.NewInt(0),
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	invoices, err := db.GetActiveInvoices(ctx, yuri.Ethereum)
	if err != nil {
		t.Fatalf("GetActiveInvoices: %v", err)
	}

	if len(invoices) != 0 {
		t.Fatalf("got %d invoices, want 0 (0 >= 0, should be excluded)", len(invoices))
	}
}

func TestGetActiveInvoices_LargeAmounts(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	bigOwed := new(big.Int)
	bigOwed.SetString("100000000000000000000", 10) // 10^20

	bigPaid := new(big.Int)
	bigPaid.SetString("99999999999999999999", 10) // one less

	_, err := db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0xlarge",
		AmountOwed: bigOwed,
		AmountPaid: bigPaid,
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	invoices, err := db.GetActiveInvoices(ctx, yuri.Ethereum)
	if err != nil {
		t.Fatalf("GetActiveInvoices: %v", err)
	}

	if len(invoices) != 1 {
		t.Fatalf("got %d invoices, want 1 (large unpaid amount should be returned)", len(invoices))
	}
}

func TestGetActiveInvoices_DifferentDigitCounts(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// paid 99 of 1000 different digit count should be returned
	_, err := db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0xdiff",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(99),
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// paid 9999 of 1000 overpaid, different digit count, should be excluded.
	_, err = db.NewInvoiceWithExpirey(ctx, yuri.Invoice{
		Chain:      yuri.Ethereum,
		Address:    "0xover",
		AmountOwed: big.NewInt(1000),
		AmountPaid: big.NewInt(9999),
		Token:      yuri.EthereumUSDT,
	}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("insert overpaid: %v", err)
	}

	invoices, err := db.GetActiveInvoices(ctx, yuri.Ethereum)
	if err != nil {
		t.Fatalf("GetActiveInvoices: %v", err)
	}

	if len(invoices) != 1 {
		t.Fatalf("got %d invoices, want 1", len(invoices))
	}
	if invoices[0].Address != "0xdiff" {
		t.Errorf("got address %s, want 0xdiff", invoices[0].Address)
	}
}
