package yuri

import (
	"testing"
)

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
