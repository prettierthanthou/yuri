package yuri

import (
	"context"
	"errors"
	"math"
	"testing"
)

// testingFixedPriceProvider is a small utility for testing
// against a fake pricing provider.
type testingFixedPriceProvider struct {
	price int64
	err   error
}

func (f testingFixedPriceProvider) WantsFullChainName() bool {
	return true
}

func (f testingFixedPriceProvider) Get(
	_ context.Context,
	_ Currency,
	_ string,
	_ Token,
) (int64, error) {
	return f.price, f.err
}

func TestPricingFiatConversion(t *testing.T) {
	f := USD.Of(10.50).Minor
	if f != 1050 {
		t.Fatalf("Minor = %d expected %d", f, 1050)
	}
}

func TestPricingFiatExceedsMaxDecimalsTruncates(t *testing.T) {
	f := USD.Of(10.501111111111).Minor
	if f != 1050 {
		t.Fatalf("Minor = %d expected %d", f, 1050)
	}
}

func TestPricingMedianPriceAggregator_Aggregate(t *testing.T) {
	errTestProvider := errors.New("test provider error")
	tests := []struct {
		name    string
		quotes  []PriceQuote
		want    int64
		wantErr bool
	}{
		{
			name: "odd number of prices returns middle value",
			quotes: []PriceQuote{
				{FiatMinorUnits: 100},
				{FiatMinorUnits: 300},
				{FiatMinorUnits: 200},
			},
			want: 200,
		},
		{
			name: "even number of prices returns average of middle values",
			quotes: []PriceQuote{
				{FiatMinorUnits: 100},
				{FiatMinorUnits: 400},
				{FiatMinorUnits: 200},
				{FiatMinorUnits: 300},
			},
			want: 250,
		},
		{
			name: "errored quotes are ignored",
			quotes: []PriceQuote{
				{FiatMinorUnits: 100},
				{Err: errTestProvider},
				{FiatMinorUnits: 300},
				{FiatMinorUnits: 200},
			},
			want: 200,
		},
		{
			name: "no valid quotes returns error",
			quotes: []PriceQuote{
				{Err: errTestProvider},
				{Err: errTestProvider},
			},
			wantErr: true,
		},
	}

	agg := MedianPriceAggregator{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := agg.Aggregate(context.Background(), tt.quotes)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPricingMedianPriceAggregator_DoesNotMutateInput(t *testing.T) {
	quotes := []PriceQuote{
		{FiatMinorUnits: 300},
		{FiatMinorUnits: 100},
		{FiatMinorUnits: 200},
	}

	original := append([]PriceQuote(nil), quotes...)

	agg := MedianPriceAggregator{}
	_, err := agg.Aggregate(context.Background(), quotes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := range quotes {
		if quotes[i] != original[i] {
			t.Fatalf("input mutated at index %d: got %+v want %+v", i, quotes[i], original[i])
		}
	}
}

func TestPricingMedian_DoesNotOverflowWhenAveragingMiddleValues(t *testing.T) {
	max := int64(math.MaxInt64)

	values := []int64{
		max - 10,
		max - 5,
	}

	got := median(values)

	// expected: (MaxInt64-10 + MaxInt64-5) / 2 without overflowing.
	want := max - 8

	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}
