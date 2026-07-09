package yuri

import (
	"context"
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
