package yuri

import (
	"context"
	"errors"
	"math"
)

var (
	FiatCurrencyNotSupportedErr = errors.New("requested currency is not supported by the pricing provider")
	ChainNotSupportedErr        = errors.New("requested chain/token is not supported by the pricing provider")
)

// PriceProvider respresents some place where we can fetch current prices
// for a cryptocurrency. A PriceProvider MUST ALWAYS be safe to call concurrently.
type PriceProvider interface {
	WantsFullChainName() bool

	// Get fetches the price of a cryptocurrency(/token) from a remote
	// pricing provider, for example CoinGecko, in the requested currency.
	//
	// Get MUST ALWAYS be safe to call concurrently.
	//
	// The return value should be in the minor fiat unit, for example
	// cents.
	//
	// If the provider does not support the requested fiat currency
	// then CurrencyNotSupportedErr should be thrown.
	//
	// If the provider does not support the requested chain/token
	// then ChainNotSupportedErr should be thrown.
	Get(ctx context.Context, currency Currency, chain string, token Token) (fiatMinorUnits int64, err error)
}

var (
	USD = Currency{Code: "USD", Decimals: 2}
	EUR = Currency{Code: "EUR", Decimals: 2}
	GBP = Currency{Code: "GBP", Decimals: 2}
	JPY = Currency{Code: "JPY", Decimals: 0}
)

type Currency struct {
	Code     string
	Decimals int
}

type fiat struct {
	Currency Currency
	Minor    int64
}

// Of takes in the amount.
// For example, yuri.USD.Of(10.50) would mean $10.50 USD
func (c Currency) Of(amount float64) fiat {
	return fiat{
		Currency: c,
		Minor:    c.ToMinor(amount),
	}
}

func (c Currency) ToMinor(amount float64) int64 {
	return int64(math.Round(amount * float64(math.Pow10(c.Decimals))))
}
