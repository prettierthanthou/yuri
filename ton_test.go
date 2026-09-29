package yuri

import (
	"context"
	"crypto"
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
)

func TestTonChainAndDecimals(t *testing.T) {
	p := tonProvider{api: nil, hooks: ProviderHooks{}}

	if p.Chain() != Ton {
		t.Fatal("expected TON chain")
	}

	if p.Decimals() != 9 {
		t.Fatal("expected 9 decimals")
	}
}

var _ chainClient = &fakeChainClient{}

type fakeChainClient struct {
	address string
	block   *chainBlock

	native *big.Int
	jetton *big.Int

	nftOwner string
	nftErr   error

	err error
}

// NFTOwner implements [chainClient].
func (f *fakeChainClient) NFTOwner(
	context.Context,
	*chainBlock,
	string,
	string,
) (string, error) {
	if f.nftErr != nil {
		return "", f.nftErr
	}

	return f.nftOwner, f.err
}

func (f *fakeChainClient) CreateAddress(context.Context, ProviderHooks) (string, error) {
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
	t.Parallel()

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

func TestTonChainClientCreateAddress(t *testing.T) {
	t.Parallel()

	// deriving an address is pure key derivation, it must work without
	// ever touching the liteclient pool.
	c := &tonChainClient{}

	hookCalls := 0
	hooks := ProviderHooks{
		OnNewAddress: func(context.Context, crypto.PublicKey, crypto.PrivateKey) error {
			hookCalls++
			return nil
		},
	}

	addr, err := c.CreateAddress(context.Background(), hooks)
	if err != nil {
		t.Fatalf("CreateAddress = %v expected = nil", err)
	}

	if hookCalls != 1 {
		t.Fatalf("OnNewAddress calls = %d expected = 1", hookCalls)
	}

	if addr == "" {
		t.Fatal("CreateAddress returned an empty address")
	}

	if _, err := address.ParseAddr(addr); err != nil {
		t.Fatalf("CreateAddress returned an unparseable address (%s): %v", addr, err)
	}
}

func TestTonProviderPollNativeBalanceChanged(t *testing.T) {
	t.Parallel()

	client := &fakeChainClient{
		block:  &chainBlock{},
		native: big.NewInt(100),
	}

	p := tonProvider{api: client}

	invoice := Invoice{
		Address:    "addr",
		Pending:    false,
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

	if invoice.Pending {
		t.Fatalf("original invoice was mutated. expected pending = false got = true")
	}

	if invoice.AmountPaid.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("original invoice was mutated. expected amountpaid = 0 got = %s", invoice.AmountPaid.String())
	}
}

func TestTonProviderPollNoChanges(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestTonProviderPollNFTReceived(t *testing.T) {
	t.Parallel()

	client := &fakeChainClient{
		block:    &chainBlock{},
		nftOwner: "addr",
	}

	p := tonProvider{api: client}

	invoice := Invoice{
		Address: "addr",
		Token: Token{
			Symbol:   NftSymbol,
			Contract: "collection|||123",
			Decimals: 1,
		},
		Pending:    true,
		AmountPaid: big.NewInt(0),
		AmountOwed: big.NewInt(1),
		Chain:      Ton,
	}

	updated, err := p.Poll(context.Background(), []Invoice{invoice})
	if err != nil {
		t.Fatalf("Poll failed = %v expected = nil", err)
	}

	if len(updated) != 1 {
		t.Fatalf("expected 1 updated invoice, got %d", len(updated))
	}

	updatedInv := updated[0]

	if updatedInv.AmountPaid.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf(
			"expected NFT amountpaid = 1 got %s",
			updatedInv.AmountPaid.String(),
		)
	}

	if updatedInv.Pending {
		t.Fatalf("expected pending = false got true")
	}

	if invoice.Pending != true {
		t.Fatalf("original invoice was mutated")
	}

	if invoice.AmountPaid.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf(
			"original invoice amountpaid mutated = %s",
			invoice.AmountPaid.String(),
		)
	}
}

func TestTonProviderPollNFTNotReceived(t *testing.T) {
	t.Parallel()

	client := &fakeChainClient{
		block:    &chainBlock{},
		nftOwner: "different-owner",
	}

	p := tonProvider{api: client}

	invoice := Invoice{
		Address: "addr",
		Token: Token{
			Symbol:   NftSymbol,
			Contract: "collection|||123",
			Decimals: 1,
		},
		Pending:    false,
		AmountPaid: big.NewInt(0),
		AmountOwed: big.NewInt(1),
		Chain:      Ton,
	}

	updated, err := p.Poll(context.Background(), []Invoice{invoice})
	if err != nil {
		t.Fatalf("Poll failed = %v expected = nil", err)
	}

	if len(updated) != 0 {
		t.Fatalf("expected no update, got %+v", updated)
	}
}

func TestTonProviderPollWithoutBlockErrors(t *testing.T) {
	t.Parallel()

	// a chain client which cannot produce a block must fail loudly rather
	// than hand a nil block to every balance lookup below it.
	client := &fakeChainClient{native: big.NewInt(100)}
	p := tonProvider{api: client}

	invoice := Invoice{
		Address:    "addr",
		AmountPaid: big.NewInt(0),
		AmountOwed: big.NewInt(10),
		Chain:      Ton,
	}

	updated, err := p.Poll(context.Background(), []Invoice{invoice})
	if err == nil {
		t.Fatal("Poll without a block expected an error, got nil")
	}

	if updated != nil {
		t.Fatalf("Poll = %+v expected nil", updated)
	}
}

func TestTonProviderPollNFTErrorIsReported(t *testing.T) {
	t.Parallel()

	client := &fakeChainClient{
		block:  &chainBlock{},
		nftErr: errors.New("liteserver unavailable"),
	}

	p := tonProvider{api: client}

	invoice := Invoice{
		Address: "addr",
		Token: Token{
			Symbol:   NftSymbol,
			Contract: "collection|||123",
			Decimals: 1,
		},
		AmountPaid: big.NewInt(0),
		AmountOwed: big.NewInt(1),
		Chain:      Ton,
	}

	updated, err := p.Poll(context.Background(), []Invoice{invoice})
	if err == nil {
		t.Fatal("Poll expected the NFT lookup failure to be reported, got nil")
	}

	if len(updated) != 0 {
		t.Fatalf("Poll = %+v expected no updated invoices", updated)
	}
}
