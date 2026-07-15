package yuri

import "testing"

func TestTonChainAndDecimals(t *testing.T) {
	p := NewTon(TonWithApi(nil))

	if p.Chain() != Ton {
		t.Fatal("expected TON chain")
	}

	if p.Decimals() != 9 {
		t.Fatal("expected 9 decimals")
	}
}
