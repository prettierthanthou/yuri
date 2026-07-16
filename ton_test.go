package yuri

import (
	"context"
	"math/big"
	"testing"
)

func TestTonChainAndDecimals(t *testing.T) {
	p := NewTon(TonWithApi(nil))

	if p.Chain() != Ton {
		t.Fatal("expected TON chain")
	}

	if p.Decimals() != 9 {
		t.Fatal("expected 9 decimals")
	}
}

type fakeChainClient struct {
	address string
	block   *chainBlock

	native *big.Int
	jetton *big.Int

	err error
}

func (f *fakeChainClient) CreateAddress(context.Context) (string, error) {
	return f.address, f.err
}

func (f *fakeChainClient) CurrentBlock(context.Context) (*chainBlock, error) {
	return f.block, f.err
}

func (f *fakeChainClient) NativeBalance(
	context.Context,
	*chainBlock,
	string,
) (*big.Int, error) {
	return f.native, f.err
}

func (f *fakeChainClient) JettonBalance(
	context.Context,
	*chainBlock,
	string,
	string,
) (*big.Int, error) {
	return f.jetton, f.err
}

func TestTonProviderCreateAddress(t *testing.T) {
	const expectedTonAddr = "ton123"
	client := &fakeChainClient{
		address: expectedTonAddr,
	}

	p := tonProvider{api: client}
	addr, err := p.CreateAddress(context.Background())

	if err != nil {
		t.Fatalf("CreateAddress = %+v expected = nil", err)
	}

	if addr != expectedTonAddr {
		t.Fatalf("CreateAddress = %s expected %s", addr, expectedTonAddr)
	}
}

func TestTonProviderPollNativeBalanceChanged(t *testing.T) {
	client := &fakeChainClient{
		block:  &chainBlock{},
		native: big.NewInt(100),
	}

	p := tonProvider{api: client}

	invoice := Invoice{
		Address:    "addr",
		Pending:    true,
		AmountPaid: big.NewInt(0),
		AmountOwed: big.NewInt(10),
		Chain:      Ton,
	}

	updated, err := p.Poll(context.Background(), []Invoice{invoice})
	if err != nil {
		t.Fatalf("Poll = %+v expected = nil", err)
	}

	if len(updated) != 1 {
		t.Fatalf("Poll len = %d expected = 1", len(updated))
	}

	updatedInv := updated[0]
	if big.NewInt(100).Cmp(updatedInv.AmountPaid) != 0 {
		t.Fatalf("Poll native invoice = %s expected = 100", updatedInv.AmountPaid.String())
	}

	if updatedInv.Pending {
		t.Fatalf("pending = %v expected = false", updatedInv.Pending)
	}

	if !invoice.Pending {
		t.Fatalf("original invoice was mutated. expected pending = false got = true")
	}

	if invoice.AmountPaid.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("original invoice was mutated. expected amountpaid = 0 got = %s", invoice.AmountPaid.String())
	}
}

func TestTonProviderPollNoChanges(t *testing.T) {
	client := &fakeChainClient{
		block:  &chainBlock{},
		native: big.NewInt(2),
	}

	p := tonProvider{api: client}

	invoice := Invoice{
		Address:    "addr",
		Pending:    false,
		AmountPaid: big.NewInt(2),
		AmountOwed: big.NewInt(10),
		Chain:      Ton,
	}

	updated, err := p.Poll(context.Background(), []Invoice{invoice})
	if err != nil {
		t.Fatalf("Poll failed = %v expected = nil", err)
	}

	if len(updated) != 0 {
		t.Fatalf("Poll inproperly returned non-updated invoices. got = %+v", updated)
	}
}

func TestTonProviderPollJettonBalanceChanged(t *testing.T) {
	client := &fakeChainClient{
		block:  &chainBlock{},
		jetton: big.NewInt(42),
	}

	p := tonProvider{api: client}
	invoice := Invoice{
		Address: "addr",
		Token: Token{
			Contract: "jetton",
		},
		Pending:    false,
		AmountPaid: big.NewInt(0),
		AmountOwed: big.NewInt(50),
	}

	updated, err := p.Poll(context.Background(), []Invoice{invoice})
	if err != nil {
		t.Fatalf("Poll failed = %v expected = nil", err)
	}

	if len(updated) != 1 {
		t.Fatalf("expected 1 updated invoice. got = %+v", updated)
	}

	updatedInv := updated[0]
	if updatedInv.AmountPaid.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("expected amountpaid = 42 got %s", updatedInv.AmountPaid.String())
	}

	if updatedInv.Pending {
		t.Fatalf("expected pending = false got = true")
	}
}
