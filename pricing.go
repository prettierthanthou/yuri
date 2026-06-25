package yuri

import (
	"errors"
	"math"
)

var (
	FiatCurrencyNotSupportedErr = errors.New("requested currency is not supported by the pricing provider")
	ChainNotSupportedErr        = errors.New("requested chain/token is not supported by the pricing provider")
)

type PriceProvider interface {
	// Get fetches the price of a cryptocurrency(/token) from a remote
	// pricing provider, for example CoinGecko, in the requested currency.
	//
	// The return value should be in the minor fiat unit, for example
	// cents.
	//
	// If the provider does not support the requested fiat currency
	// then CurrencyNotSupportedErr should be thrown.
	//
	// If the provider does not support the requested chain/token
	// then ChainNotSupportedErr should be thrown.
	Get(currency Currency, chain string, token Token) (fiatMinorUnits int64, err error)
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
	scale := math.Pow10(c.Decimals)
	return fiat{
		Currency: c,
		Minor:    int64(amount * float64(scale)),
	}
}
