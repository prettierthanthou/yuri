package yuri

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestMarketProviderBuildsAssetSpecificURLs(t *testing.T) {
	tests := []struct {
		name         string
		p            marketProvider
		currency     Currency
		chain        string
		token        Token
		wantPath     string
		wantQuery    map[string]string
		responseBody string
	}{
		{
			name:         "bitnob-ltc",
			p:            NewBitnobPriceProvider(nil).(marketProvider),
			currency:     USD,
			chain:        "LTC",
			wantPath:     "/api/v1/rates/ltc/price",
			responseBody: `{"data":{"rate":1}}`,
		},
		{
			name:         "bylls-eth",
			p:            NewByllsPriceProvider(nil).(marketProvider),
			currency:     Currency{Code: "CAD", Decimals: 2},
			chain:        "ETH",
			wantPath:     "/api/price",
			wantQuery:    map[string]string{"from_currency": "ETH", "to_currency": "CAD"},
			responseBody: `{"public_price":{"to_price":1}}`,
		},
		{
			name:         "freecurrencyrates-xmr",
			p:            NewFreeCurrencyRatesPriceProvider(nil).(marketProvider),
			currency:     USD,
			chain:        "XMR",
			wantPath:     "/v1/currencies/xmr.min.json",
			responseBody: `{"xmr":{"usd":1}}`,
		},
		{
			name:         "yadio-ltc",
			p:            NewYadioPriceProvider(nil).(marketProvider),
			currency:     USD,
			chain:        "LTC",
			wantPath:     "/exrates/LTC",
			responseBody: `{"LTC":{"USD":1}}`,
		},
		{
			name:         "buda-eth",
			p:            NewBudaPriceProvider(nil).(marketProvider),
			currency:     Currency{Code: "CLP", Decimals: 0},
			chain:        "ETH",
			wantPath:     "/api/v2/markets/eth-clp/ticker",
			responseBody: `{"ticker":{"max_bid":"1","min_ask":"1"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURI := ""
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotURI = r.URL.RequestURI()
				w.Write([]byte(tt.responseBody))
			}))
			defer srv.Close()

			tt.p.url = srv.URL + tt.wantPath

			got, err := tt.p.Get(context.Background(), tt.currency, tt.chain, tt.token)
			if err != nil {
				t.Fatalf("Get() error: %v", err)
			}

			if got == 0 {
				t.Fatalf("expected non-zero price")
			}

			if tt.wantQuery == nil {
				if gotURI != tt.wantPath {
					t.Fatalf("request uri = %q want %q", gotURI, tt.wantPath)
				}
			} else {
				parsed, err := url.ParseRequestURI(gotURI)
				if err != nil {
					t.Fatalf("ParseRequestURI: %v", err)
				}
				if parsed.Path != tt.wantPath {
					t.Fatalf("request path = %q want %q", parsed.Path, tt.wantPath)
				}
				for k, want := range tt.wantQuery {
					if got := parsed.Query().Get(k); got != want {
						t.Fatalf("query %s = %q want %q", k, got, want)
					}
				}
			}
		})
	}
}

func TestMarketProviderUsesTokenSymbol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rates/usdt/price" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"data":{"rate":1.00}}`))
	}))
	defer srv.Close()

	p := NewBitnobPriceProvider(nil).(marketProvider)
	p.url = srv.URL + "/api/v1/rates/bitcoin/price"

	got, err := p.Get(context.Background(), USD, string(Ethereum), EthereumUSDT)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if got != 100 {
		t.Fatalf("Get() = %d want %d", got, 100)
	}
}
