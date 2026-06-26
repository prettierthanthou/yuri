package yuri

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// Hooks are lifecycle state to that you don't need to handle
// lifecycle updates in Storage handlers.
type Hooks struct {
	OnInvoiceUpdated func(context.Context, Invoice) error
	OnError          func(error)
}

type Options struct {
	Chains  []CryptoProvider
	Pricing []PriceProvider
	Hooks   Hooks
	Storage Storage
	// How often we should poll for new changes on each provided
	// CryptoProvider. The default is 15 seconds.
	PollEvery time.Duration
	// MaxPollDuration is the maximum amount of time a CryptoProvider
	// is allowed to Poll for. (how long each crypto can Poll for)
	MaxPollDuration time.Duration
}

type Instance struct {
	opts   Options
	chains map[Chain]CryptoProvider
}

func New(options Options) (*Instance, error) {
	if options.PollEvery <= 0 {
		options.PollEvery = 15 * time.Second
	}

	if options.MaxPollDuration <= 0 {
		options.MaxPollDuration = 10 * time.Second
	}

	if options.Storage == nil {
		return nil, fmt.Errorf("Storage cannot be nil")
	}

	if len(options.Pricing) == 0 {
		return nil, fmt.Errorf("Pricing must have atleast 1 pricing provider")
	}

	if len(options.Chains) == 0 {
		return nil, fmt.Errorf("Chains must have atleast 1 CryptoProvider")
	}

	chains := make(map[Chain]CryptoProvider, len(options.Chains))
	for _, chain := range options.Chains {
		if _, exists := chains[chain.Chain()]; exists {
			return nil, fmt.Errorf("duplicate chain: %s", chain.Chain())
		}

		chains[chain.Chain()] = chain
	}

	return &Instance{opts: options, chains: chains}, nil
}

func (i *Instance) reportErr(err error) {
	if err == nil {
		return
	}

	if i.opts.Hooks.OnError != nil {
		i.opts.Hooks.OnError(err)
	}
}

// Run begins polling every Options.PollEvery and blocks whilst doing so until the context is canceled.
//
// If `Storage.UpdateInvoices` fails at any point we do not emit `Hooks.OnInvoiceUpdated` for the Invoice.
func (i *Instance) Run(ctx context.Context) {
	var wg sync.WaitGroup

	for chain, provider := range i.chains {
		wg.Add(1)

		go func(
			chain Chain,
			provider CryptoProvider,
		) {
			defer wg.Done()

			i.runChain(ctx, chain, provider)
		}(chain, provider)
	}

	wg.Wait()
}

func (i *Instance) runChain(
	ctx context.Context,
	chain Chain,
	provider CryptoProvider,
) {
	ticker := time.NewTicker(i.opts.PollEvery)
	defer ticker.Stop()

	for {
		derivedCtx, cancel := context.WithTimeout(ctx, i.opts.MaxPollDuration)
		defer cancel()

		if err := i.poll(derivedCtx, chain, provider); err != nil {
			i.reportErr(err)
		}

		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
		}
	}
}

func (i *Instance) poll(
	ctx context.Context,
	chain Chain,
	provider CryptoProvider,
) error {
	invoices, err := i.opts.Storage.GetActiveInvoices(ctx, chain)
	if err != nil {
		return err
	}

	updatedInvoices, err := provider.Poll(ctx, invoices)
	if err != nil {
		return err
	}

	if err := i.opts.Storage.UpdateInvoices(ctx, updatedInvoices); err != nil {
		return err
	}

	for _, updated := range updatedInvoices {
		if i.opts.Hooks.OnInvoiceUpdated != nil {
			if err := i.opts.Hooks.OnInvoiceUpdated(ctx, updated); err != nil {
				i.reportErr(err)
			}
		}
	}

	return nil
}

func (i *Instance) avgPrice(currency Currency, chain string, token Token) (int64, error) {
	if len(i.opts.Pricing) == 0 {
		return 0, errors.New("no price providers configured")
	}

	var sum int64
	var count int64

	for _, p := range i.opts.Pricing {
		price, err := p.Get(currency, chain, token)
		if err != nil {
			continue
		}

		sum += price
		count++
	}

	if count == 0 {
		return 0, errors.New("no providers returned valid prices")
	}

	return sum / count, nil
}

var ten = big.NewInt(10)

func (i *Instance) NewInvoice(ctx context.Context, invoiceCreate InvoiceCreate) (Invoice, error) {
	chain, ok := i.chains[invoiceCreate.Chain]
	if !ok {
		return Invoice{}, fmt.Errorf("chain %s is not registered", invoiceCreate.Chain)
	}

	// TODO: coordinate with pricing providers
	avgPrice, err := i.avgPrice(invoiceCreate.AmountFiat.Currency, string(invoiceCreate.Chain), invoiceCreate.Token)
	if err != nil {
		return Invoice{}, err
	}

	if avgPrice <= 0 {
		return Invoice{}, fmt.Errorf("invalid average price: %d", avgPrice)
	}

	cryptoDecimals := chain.Decimals()
	if invoiceCreate.Token != (Token{}) {
		cryptoDecimals = invoiceCreate.Token.Decimals
	}

	fiat := big.NewInt(invoiceCreate.AmountFiat.Minor)
	price := big.NewInt(avgPrice)

	scale := new(big.Int).Exp(
		ten,
		big.NewInt(int64(cryptoDecimals)),
		nil,
	)

	// numerator = fiat * scale
	numerator := new(big.Int).Mul(fiat, scale)

	// quotient and remainder
	cryptoOwed := new(big.Int)
	remainder := new(big.Int)

	cryptoOwed.DivMod(numerator, price, remainder)

	// round up if remainder != 0
	if remainder.Sign() != 0 {
		cryptoOwed.Add(cryptoOwed, big.NewInt(1))
	}

	addr, err := chain.CreateAddress(ctx)
	if err != nil {
		return Invoice{}, err
	}

	inv := Invoice{
		Chain:      invoiceCreate.Chain,
		AmountOwed: cryptoOwed,
		AmountPaid: new(big.Int),
		Token:      invoiceCreate.Token,
		Pending:    false,
		Metadata:   invoiceCreate.Metadata,
		Address:    addr,
	}

	if err := i.opts.Storage.NewInvoice(ctx, inv); err != nil {
		return Invoice{}, err
	}

	return inv, nil
}
