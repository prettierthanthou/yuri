package yuri

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
)

var (
	ErrFiatCurrencyNotSupported = errors.New("requested currency is not supported by the pricing provider")
	ErrChainNotSupported        = errors.New("requested chain/token is not supported by the pricing provider")
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
	Code     string `json:"code"`
	Decimals int    `json:"decimals"`
}

type fiat struct {
	Currency Currency `json:"currency"`
	Minor    int64    `json:"minor"`
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

type PriceQuote struct {
	FiatMinorUnits int64
	Err            error
}

// a PriceAggregator takes in all the fiat minor prices from every PriceProvider that's registered,
// and spits out the respective end output price.
//
// Generally speaking most users will not have a need to use something outside of the default
// [AvgPriceAggregator] or [MedianPriceAggregator] but if you have serious business requirements,
// or domain specific logic, this is hopefully enough to get it done.
type PriceAggregator interface {
	// Aggregate accepts the result from many [PriceProvider.Get] calls (formated nicely),
	// and performs whatever calculations needed to spit out a price it believes is correct.
	//
	// [quotes] will always be a slice with >=1 length.
	//
	// Aggregate MUST ALWAYS be safe to call concurrently, and MUST comply with Context cancellation (if doing something complex).
	Aggregate(ctx context.Context, quotes []PriceQuote) (fiatMinorUnits int64, err error)
}

var _ PriceAggregator = MedianPriceAggregator{}

type MedianPriceAggregator struct{}

func (MedianPriceAggregator) Aggregate(_ context.Context, quotes []PriceQuote) (int64, error) {
	prices := ValidPrices(quotes)
	if len(prices) == 0 {
		return 0, fmt.Errorf("MedianPriceAggregator: no valid quotes out of %d quotes", len(quotes))
	}

	return median(prices), nil
}

// ValidPrices sifts through a slice of PriceQuotes and returns the values from only
// non-errored ones. Generally speaking you'd want to use this for a [PriceAggregator]
// implementation, but it's arbitrary.
func ValidPrices(quotes []PriceQuote) []int64 {
	prices := make([]int64, 0, len(quotes))

	for _, q := range quotes {
		if q.Err != nil {
			continue
		}

		prices = append(prices, q.FiatMinorUnits)
	}

	return prices
}

func median(values []int64) int64 {
	s := slices.Clone(values)

	sort.Slice(s, func(i, j int) bool {
		return s[i] < s[j]
	})

	n := len(s)

	if n%2 == 1 {
		return s[n/2]
	}

	a := s[n/2-1]
	b := s[n/2]

	return a/2 + b/2 + (a%2+b%2)/2
}
