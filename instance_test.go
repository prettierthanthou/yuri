package yuri

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"
)

func errHasSubstr(t *testing.T, err error, str string) bool {
	t.Helper()

	if !strings.Contains(err.Error(), str) {
		t.Fatalf("New() error = \"%s\" expecting \"%s\"", err.Error(), str)
		return false
	}

	return true
}

type testingFixedPriceProvider struct {
	price int64
	err   error
}

func (f testingFixedPriceProvider) Get(
	_ context.Context,
	_ Currency,
	_ string,
	_ Token,
) (int64, error) {
	return f.price, f.err
}

var _ CryptoProvider = &testingFakeCryptoProvider{}

type testingFakeCryptoProvider struct {
	mu        sync.Mutex
	pollState int
}

// Poll implements [CryptoProvider].
func (t *testingFakeCryptoProvider) Poll(_ context.Context, invoices []Invoice) ([]Invoice, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(invoices) > 1 {
		return nil, errors.New("testingFakeCryptoProvider expects 1 invoice maximum")
	}

	if t.pollState == 0 {
		t.pollState++
		return invoices, nil
	}

	if t.pollState == 1 {
		t.pollState++

		inv := invoices[0]
		return []Invoice{
			{
				Address:    inv.Address,
				AmountOwed: inv.AmountOwed,
				AmountPaid: inv.AmountOwed,
				Token:      inv.Token,
				Pending:    true,
				Metadata:   inv.Metadata,
			},
		}, nil
	}

	if t.pollState == 2 {
		t.pollState = 0

		inv := invoices[0]
		return []Invoice{
			{
				Address:    inv.Address,
				AmountOwed: inv.AmountOwed,
				AmountPaid: inv.AmountOwed,
				Token:      inv.Token,
				Pending:    true,
				Metadata:   inv.Metadata,
			},
		}, nil
	}

	return nil, errors.New("invalid testing pollstate")
}

// Chain implements [CryptoProvider].
func (t *testingFakeCryptoProvider) Chain() Chain {
	return Chain("test")
}

// CreateAddress implements [CryptoProvider].
func (t *testingFakeCryptoProvider) CreateAddress(context.Context) (string, error) {
	return "fake_test_addr", nil
}

// Decimals implements [CryptoProvider].
func (t *testingFakeCryptoProvider) Decimals() int64 {
	return 12
}

func TestInstanceNew(t *testing.T) {
	_, err := New(Options{PollEvery: 5 * time.Second, Storage: nil})
	if !errHasSubstr(t, err, "cannot be nil") {
		return
	}

	_, err = New(Options{PollEvery: 5 * time.Second, Storage: &InMemoryStorage{}})
	if !errHasSubstr(t, err, "pricing provider") {
		return
	}

	_, err = New(Options{PollEvery: 5 * time.Second, Storage: &InMemoryStorage{}, Pricing: []PriceProvider{testingFixedPriceProvider{}}})
	if !errHasSubstr(t, err, "CryptoProvider") {
		return
	}

	_, err = New(Options{PollEvery: 5 * time.Second, Storage: &InMemoryStorage{}, Pricing: []PriceProvider{testingFixedPriceProvider{}}, Chains: []CryptoProvider{NewMonero(JsonRpcClientConfig{})}})
	if err != nil {
		t.Fatalf("New() error = %q wanted nil", err)
	}
}

func TestInstanceNewDefaults(t *testing.T) {
	instance, err := New(Options{
		Storage: &InMemoryStorage{},
		Pricing: []PriceProvider{testingFixedPriceProvider{}},
		Chains:  []CryptoProvider{&testingFakeCryptoProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if instance.opts.PollEvery != 15*time.Second {
		t.Fatalf("PollEvery = %s expected %s",
			instance.opts.PollEvery,
			15*time.Second,
		)
	}

	if instance.opts.MaxPollDuration != 10*time.Second {
		t.Fatalf("MaxPollDuration = %s expected %s",
			instance.opts.MaxPollDuration,
			10*time.Second,
		)
	}
}

func TestNewDuplicateChains(t *testing.T) {
	_, err := New(Options{
		Storage: &InMemoryStorage{},
		Pricing: []PriceProvider{testingFixedPriceProvider{}},
		Chains: []CryptoProvider{
			&testingFakeCryptoProvider{},
			&testingFakeCryptoProvider{},
		},
	})

	if err == nil {
		t.Fatal("expected duplicate chain error")
	}

	if !strings.Contains(err.Error(), "duplicate chain") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstanceNewInvoiceWithToken(t *testing.T) {
	storage := &InMemoryStorage{}

	instance, err := New(Options{
		PollEvery: 5 * time.Second,
		Storage:   storage,
		Pricing:   []PriceProvider{testingFixedPriceProvider{price: 100}},
		Chains:    []CryptoProvider{&testingFakeCryptoProvider{}},
	})
	if err != nil {
		t.Fatalf("New() error = %q wanted nil", err)
	}

	inv, err := instance.NewInvoice(t.Context(), InvoiceCreate{
		Chain:      Chain("test"),
		AmountFiat: USD.Of(3.50),
		Token: Token{
			Symbol:   "FAKE",
			Contract: "0xFAKEFAKEFAKEFAKE",
			Decimals: 18,
		},
	})

	if err != nil {
		t.Fatalf("NewInvoice() error = %q wanted nil", err)
	}

	if inv.Address != "fake_test_addr" {
		t.Fatalf("Address = %s expected fake_test_addr", inv.Address)
	}

	if inv.AmountPaid.BitLen() != 0 {
		t.Fatalf("AmountPaid = %d expected 0", inv.AmountPaid.BitLen())
	}

	const treeFiddyInCents = 3500000000000000000
	if inv.AmountOwed.Cmp(big.NewInt(3500000000000000000)) != 0 {
		t.Fatalf("AmountOwed = %s expected %d", inv.AmountOwed.String(), treeFiddyInCents)
	}

	if inv.Pending {
		t.Fatalf("Pending = true expected false")
	}

	if inv.Metadata != nil {
		t.Fatalf("Metadata should be nil by default")
	}
}

func TestInstanceNewInvoice(t *testing.T) {
	storage := &InMemoryStorage{}

	instance, err := New(Options{
		PollEvery: 5 * time.Second,
		Storage:   storage,
		Pricing:   []PriceProvider{testingFixedPriceProvider{price: 100}},
		Chains:    []CryptoProvider{&testingFakeCryptoProvider{}},
	})
	if err != nil {
		t.Fatalf("New() error = %q wanted nil", err)
	}

	inv, err := instance.NewInvoice(t.Context(), InvoiceCreate{
		Chain:      Chain("test"),
		AmountFiat: USD.Of(3.50),
	})

	if err != nil {
		t.Fatalf("NewInvoice() error = %q wanted nil", err)
	}

	// NOTE: we do not check if NewInvoice() created the invoice in our Storage
	// as that comes down to Storage's tests (if any.) and isn't the point of this
	// Nor do we check the Monero JSON-RPC if the address was properly created,
	// as that's the responsibility of monero_test

	if inv.Address != "fake_test_addr" {
		t.Fatalf("Address = %s expected fake_test_addr", inv.Address)
	}

	if inv.AmountPaid.BitLen() != 0 {
		t.Fatalf("AmountPaid = %d expected 0", inv.AmountPaid.BitLen())
	}

	const treeFiddyInCents = 3500000000000
	if inv.AmountOwed.Cmp(big.NewInt(3500000000000)) != 0 {
		t.Fatalf("AmountOwed = %s expected %d", inv.AmountOwed.String(), treeFiddyInCents)
	}

	if inv.Pending {
		t.Fatalf("Pending = true expected false")
	}

	if inv.Metadata != nil {
		t.Fatalf("Metadata should be nil by default")
	}
}

func TestInstanceNewInvoiceWithMetadata(t *testing.T) {
	storage := &InMemoryStorage{}

	instance, err := New(Options{
		PollEvery: 5 * time.Second,
		Storage:   storage,
		Pricing:   []PriceProvider{testingFixedPriceProvider{price: 100}},
		Chains:    []CryptoProvider{&testingFakeCryptoProvider{}},
	})
	if err != nil {
		t.Fatalf("New() error = %q wanted nil", err)
	}

	inv, err := instance.NewInvoice(t.Context(), InvoiceCreate{
		Chain:      Chain("test"),
		AmountFiat: USD.Of(3.50),
		Metadata: map[string]any{
			"id": "123456",
		},
	})

	if err != nil {
		t.Fatalf("NewInvoice() error = %q wanted nil", err)
	}

	// NOTE: we do not check if NewInvoice() created the invoice in our Storage
	// as that comes down to Storage's tests (if any.) and isn't the point of this
	// Nor do we check the Monero JSON-RPC if the address was properly created,
	// as that's the responsibility of monero_test

	if inv.Address != "fake_test_addr" {
		t.Fatalf("Address = %s expected fake_test_addr", inv.Address)
	}

	if inv.AmountPaid.BitLen() != 0 {
		t.Fatalf("AmountPaid = %d expected 0", inv.AmountPaid.BitLen())
	}

	const treeFiddyInCents = 3500000000000
	if inv.AmountOwed.Cmp(big.NewInt(3500000000000)) != 0 {
		t.Fatalf("AmountOwed = %s expected %d", inv.AmountOwed.String(), treeFiddyInCents)
	}

	if inv.Pending {
		t.Fatalf("Pending = true expected false")
	}

	if inv.Metadata == nil {
		t.Fatalf("Metadata shouldn't be nil")
	}

	val, ok := inv.Metadata["id"]
	if !ok {
		t.Fatalf("Metadata[id] missing")
	}

	if val != "123456" {
		t.Fatalf("Metadata[id] = %v expected 123456", val)
	}
}

type recordingStorage struct {
	newInvoiceCalled bool
	newInvoice       Invoice
}

func (r *recordingStorage) NewInvoice(_ context.Context, invoice Invoice) error {
	r.newInvoiceCalled = true
	r.newInvoice = invoice
	return nil
}

func (r *recordingStorage) GetActiveInvoices(context.Context, Chain) ([]Invoice, error) {
	return nil, nil
}

func (r *recordingStorage) UpdateInvoices(context.Context, []Invoice) error {
	return nil
}

func TestInstanceNewInvoicePersistsToStorage(t *testing.T) {
	storage := &recordingStorage{}

	instance, err := New(Options{
		PollEvery: 5 * time.Second,
		Storage:   storage,
		Pricing:   []PriceProvider{testingFixedPriceProvider{price: 100}},
		Chains:    []CryptoProvider{&testingFakeCryptoProvider{}},
	})
	if err != nil {
		t.Fatalf("New() error = %q wanted nil", err)
	}

	inv, err := instance.NewInvoice(t.Context(), InvoiceCreate{
		Chain:      Chain("test"),
		AmountFiat: USD.Of(3.50),
		Metadata: map[string]any{
			"id": "abc123",
		},
	})
	if err != nil {
		t.Fatalf("NewInvoice() error = %q wanted nil", err)
	}

	if !storage.newInvoiceCalled {
		t.Fatal("Storage.NewInvoice was not called")
	}

	if storage.newInvoice.Address != inv.Address {
		t.Fatalf("stored Address = %s expected %s", storage.newInvoice.Address, inv.Address)
	}

	if storage.newInvoice.Metadata["id"] != "abc123" {
		t.Fatalf("stored Metadata[id] = %v expected abc123", storage.newInvoice.Metadata["id"])
	}
}

func TestNewInvoiceUnknownChain(t *testing.T) {
	instance, err := New(Options{
		Storage: &InMemoryStorage{},
		Pricing: []PriceProvider{testingFixedPriceProvider{}},
		Chains:  []CryptoProvider{&testingFakeCryptoProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = instance.NewInvoice(t.Context(), InvoiceCreate{
		Chain:      Chain("unknown"),
		AmountFiat: USD.Of(1),
	})

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAvgPrice(t *testing.T) {
	instance, err := New(Options{
		Storage: &InMemoryStorage{},
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 100},
			testingFixedPriceProvider{price: 200},
			testingFixedPriceProvider{price: 300},
		},
		Chains: []CryptoProvider{
			&testingFakeCryptoProvider{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	price, err := instance.avgPrice(
		context.Background(),
		USD,
		"test",
		Token{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if price != 200 {
		t.Fatalf("price = %d expected 200", price)
	}
}

func TestAvgPriceIgnoresFailedProviders(t *testing.T) {
	instance, err := New(Options{
		Storage: &InMemoryStorage{},
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 100},
			testingFixedPriceProvider{err: errors.New("boom")},
			testingFixedPriceProvider{price: 300},
		},
		Chains: []CryptoProvider{
			&testingFakeCryptoProvider{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	price, err := instance.avgPrice(
		context.Background(),
		USD,
		"test",
		Token{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if price != 200 {
		t.Fatalf("price = %d expected 200", price)
	}
}

func TestAvgPriceAllProvidersFail(t *testing.T) {
	instance, err := New(Options{
		Storage: &InMemoryStorage{},
		Pricing: []PriceProvider{
			testingFixedPriceProvider{err: errors.New("boom")},
			testingFixedPriceProvider{err: errors.New("boom")},
		},
		Chains: []CryptoProvider{
			&testingFakeCryptoProvider{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = instance.avgPrice(
		context.Background(),
		USD,
		"test",
		Token{},
	)

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewInvoiceRejectsZeroPrice(t *testing.T) {
	instance, err := New(Options{
		Storage: &InMemoryStorage{},
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 0},
		},
		Chains: []CryptoProvider{
			&testingFakeCryptoProvider{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = instance.NewInvoice(t.Context(), InvoiceCreate{
		Chain:      Chain("test"),
		AmountFiat: USD.Of(1),
	})

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewInvoiceAmountPaidNotShared(t *testing.T) {
	instance, err := New(Options{
		Storage: &InMemoryStorage{},
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 100},
		},
		Chains: []CryptoProvider{
			&testingFakeCryptoProvider{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	a, err := instance.NewInvoice(t.Context(), InvoiceCreate{
		Chain:      Chain("test"),
		AmountFiat: USD.Of(1),
	})
	if err != nil {
		t.Fatal(err)
	}

	b, err := instance.NewInvoice(t.Context(), InvoiceCreate{
		Chain:      Chain("test"),
		AmountFiat: USD.Of(1),
	})
	if err != nil {
		t.Fatal(err)
	}

	a.AmountPaid.SetInt64(123)

	if b.AmountPaid.Int64() != 0 {
		t.Fatal("AmountPaid is shared between invoices")
	}
}

// polling

var _ Storage = &pollStorage{}

type pollStorage struct {
	invoices  []Invoice
	updateErr error

	getCalled       bool
	updateCalled    bool
	updatedInvoices []Invoice
}

// NewInvoice implements [Storage].
func (p *pollStorage) NewInvoice(context.Context, Invoice) error {
	panic("unimplemented")
}

func (p *pollStorage) GetActiveInvoices(
	_ context.Context,
	_ Chain,
) ([]Invoice, error) {
	p.getCalled = true
	return p.invoices, nil
}

func (p *pollStorage) UpdateInvoices(
	_ context.Context,
	invoices []Invoice,
) error {
	p.updateCalled = true
	p.updatedInvoices = invoices
	return p.updateErr
}

var _ CryptoProvider = &pollProvider{}

type pollProvider struct {
	updated []Invoice
	err     error

	called bool
}

func (p *pollProvider) Poll(
	_ context.Context,
	_ []Invoice,
) ([]Invoice, error) {
	p.called = true
	return p.updated, p.err
}

func (p *pollProvider) Chain() Chain {
	return Chain("test")
}

func (p *pollProvider) CreateAddress(context.Context) (string, error) {
	return "", nil
}

func (p *pollProvider) Decimals() int64 {
	return 12
}

func TestInvoicePollCallsOnInvoiceUpdated(t *testing.T) {
	inv := Invoice{
		Address: "abc",
	}

	storage := &pollStorage{
		invoices: []Invoice{inv},
	}

	provider := &pollProvider{
		updated: []Invoice{
			{
				Address: "abc",
				Pending: true,
			},
		},
	}

	var hookCalls int

	instance, err := New(Options{
		Storage: storage,
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 100},
		},
		Chains: []CryptoProvider{
			provider,
		},
		Hooks: Hooks{
			OnInvoiceUpdated: func(
				_ context.Context,
				invoice Invoice,
			) error {
				hookCalls++

				if invoice.Address != "abc" {
					t.Fatalf("unexpected invoice")
				}

				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = instance.poll(
		context.Background(),
		Chain("test"),
		provider,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !storage.getCalled {
		t.Fatal("GetActiveInvoices not called")
	}

	if !storage.updateCalled {
		t.Fatal("UpdateInvoices not called")
	}

	if !provider.called {
		t.Fatal("Poll not called")
	}

	if hookCalls != 1 {
		t.Fatalf("hookCalls = %d expected 1", hookCalls)
	}
}

func TestInstancePollDoesNotCallHookWhenUpdateFails(t *testing.T) {
	storage := &pollStorage{
		invoices: []Invoice{
			{Address: "abc"},
		},
		updateErr: errors.New("db failure"),
	}

	provider := &pollProvider{
		updated: []Invoice{
			{
				Address: "abc",
				Pending: true,
			},
		},
	}

	var hookCalls int

	instance, err := New(Options{
		Storage: storage,
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 100},
		},
		Chains: []CryptoProvider{
			provider,
		},
		Hooks: Hooks{
			OnInvoiceUpdated: func(
				context.Context,
				Invoice,
			) error {
				hookCalls++
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = instance.poll(
		context.Background(),
		Chain("test"),
		provider,
	)

	if err == nil {
		t.Fatal("expected error")
	}

	if hookCalls != 0 {
		t.Fatalf(
			"hookCalls = %d expected 0",
			hookCalls,
		)
	}
}

func TestInstancePollReportsHookErrors(t *testing.T) {
	storage := &pollStorage{
		invoices: []Invoice{
			{Address: "abc"},
		},
	}

	provider := &pollProvider{
		updated: []Invoice{
			{Address: "abc"},
		},
	}

	var reported error

	instance, err := New(Options{
		Storage: storage,
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 100},
		},
		Chains: []CryptoProvider{
			provider,
		},
		Hooks: Hooks{
			OnInvoiceUpdated: func(
				context.Context,
				Invoice,
			) error {
				return errors.New("hook failed")
			},
			OnError: func(err error) {
				reported = err
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = instance.poll(
		context.Background(),
		Chain("test"),
		provider,
	)
	if err != nil {
		t.Fatal(err)
	}

	if reported == nil {
		t.Fatal("expected error to be reported")
	}
}

func TestInstancePollProviderError(t *testing.T) {
	storage := &pollStorage{
		invoices: []Invoice{
			{Address: "abc"},
		},
	}

	provider := &pollProvider{
		err: errors.New("provider failure"),
	}

	instance, err := New(Options{
		Storage: storage,
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 100},
		},
		Chains: []CryptoProvider{
			provider,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = instance.poll(
		context.Background(),
		Chain("test"),
		provider,
	)

	if err == nil {
		t.Fatal("expected error")
	}
}

var _ CryptoProvider = &contextObservedProvider{}

type contextObservedProvider struct {
	cancelled chan struct{}
}

func (p *contextObservedProvider) Chain() Chain {
	return "test"
}

func (p *contextObservedProvider) CreateAddress(context.Context) (string, error) {
	return "test", nil
}

func (p *contextObservedProvider) Decimals() int64 {
	return 12
}

func (p *contextObservedProvider) Poll(
	ctx context.Context,
	_ []Invoice,
) ([]Invoice, error) {
	<-ctx.Done()

	select {
	case <-p.cancelled:
		// already closed
	default:
		close(p.cancelled)
	}

	return nil, ctx.Err()
}

func TestInstanceRunStopsOnContextCancel(t *testing.T) {
	provider := &contextObservedProvider{
		cancelled: make(chan struct{}),
	}

	instance, err := New(Options{
		PollEvery: time.Hour,
		Storage:   &InMemoryStorage{},
		Pricing: []PriceProvider{
			testingFixedPriceProvider{},
		},
		Chains: []CryptoProvider{
			provider,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		instance.Run(ctx)
	}()

	cancel()

	select {
	case <-provider.cancelled:
		// Poll observed cancellation.
	case <-time.After(time.Second):
		t.Fatal("Poll() context was not cancelled")
	}

	select {
	case <-done:
		// Run exited.
	case <-time.After(time.Second):
		t.Fatal("Run() did not exit after cancellation")
	}
}

type cancelAwareStorage struct {
	getCalled    bool
	updateCalled bool
	active       []Invoice
}

func (s *cancelAwareStorage) NewInvoice(context.Context, Invoice) error {
	return nil
}

func (s *cancelAwareStorage) GetActiveInvoices(_ context.Context, _ Chain) ([]Invoice, error) {
	s.getCalled = true
	return s.active, nil
}

func (s *cancelAwareStorage) UpdateInvoices(_ context.Context, _ []Invoice) error {
	s.updateCalled = true
	return nil
}

type cancelAwareProvider struct {
	called chan struct{}
	done   chan struct{}
}

func (p *cancelAwareProvider) Poll(ctx context.Context, _ []Invoice) ([]Invoice, error) {
	close(p.called)
	<-ctx.Done()
	close(p.done)
	return nil, ctx.Err()
}

func (p *cancelAwareProvider) Chain() Chain {
	return Chain("test")
}

func (p *cancelAwareProvider) CreateAddress(context.Context) (string, error) {
	return "test", nil
}

func (p *cancelAwareProvider) Decimals() int64 {
	return 12
}

func TestInstanceRunUsesMaxPollDuration(t *testing.T) {
	storage := &cancelAwareStorage{
		active: []Invoice{{Address: "abc"}},
	}
	provider := &cancelAwareProvider{
		called: make(chan struct{}),
		done:   make(chan struct{}),
	}

	instance, err := New(Options{
		PollEvery:       time.Hour,
		MaxPollDuration: 20 * time.Millisecond,
		Storage:         storage,
		Pricing: []PriceProvider{
			testingFixedPriceProvider{price: 100},
		},
		Chains: []CryptoProvider{
			provider,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		instance.Run(ctx)
	}()

	select {
	case <-provider.called:
	case <-time.After(time.Second):
		t.Fatal("Poll() was not called")
	}

	select {
	case <-provider.done:
	case <-time.After(time.Second):
		t.Fatal("Poll() context was not canceled by MaxPollDuration")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not exit after cancellation")
	}
}
