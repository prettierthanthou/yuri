// yuri is a relatively self contained cryptocurrency invoice library.
//
// we handle creating invoices, polling known cryptocurrencies (via [CryptoProvider])
// users are expected to bring their own [Storage] implementation
//
// This project is licensed under the GNU AGPLv3 accessible in the License file at the root of the package.
package yuri

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// Hooks are lifecycle state to that you don't need to handle
// lifecycle updates in Storage handlers.
type Hooks struct {
	// If [Storage.UpdateInvoices] fails this method is not called for the Invoice(s)
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

// Instance represents a Yuri invoice instance, which is
// used to manage [CryptoProvider]s. Instance manages
// creation of invoices, alongside emitting event updates
// to you, the end user, using [Hooks]
type Instance struct {
	opts   Options
	chains map[Chain]CryptoProvider
}

// New creates a new [Instance] and validates the provided [Options]
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
		if err := i.poll(derivedCtx, chain, provider); err != nil {
			i.reportErr(err)
		}
		cancel()

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

func (i *Instance) avgPrice(ctx context.Context, currency Currency, chain string, token Token) (int64, error) {
	if len(i.opts.Pricing) == 0 {
		return 0, errors.New("no price providers configured")
	}

	var sum int64
	var count int64

	for _, p := range i.opts.Pricing {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		price, err := p.Get(ctx, currency, chain, token)
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

func pricingSymbol(provider CryptoProvider) string {
	symbol := strings.ToUpper(string(provider.Chain()))
	if symbolProvider, ok := provider.(PricingSymbolProvider); ok {
		if custom := strings.TrimSpace(symbolProvider.PriceSymbol()); custom != "" {
			return custom
		}
	}

	return symbol
}

var ten = big.NewInt(10)

// NewInvoice creates a new invoice with the [Instance] using the respective
// [CryptoProvider] and [PriceProvider]
func (i *Instance) NewInvoice(ctx context.Context, invoiceCreate InvoiceCreate) (Invoice, error) {
	chain, ok := i.chains[invoiceCreate.Chain]
	if !ok {
		return Invoice{}, fmt.Errorf("chain %s is not registered", invoiceCreate.Chain)
	}

	avgPrice, err := i.avgPrice(ctx, invoiceCreate.AmountFiat.Currency, pricingSymbol(chain), invoiceCreate.Token)
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
