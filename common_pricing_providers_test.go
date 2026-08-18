package yuri

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
			responseBody: `{"data":{"USD":1}}`,
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
				if _, err := w.Write([]byte(tt.responseBody)); err != nil {
					t.Fatalf("failed to write: %+v", err)
				}
			}))
			defer srv.Close()

			tt.p.url = srv.URL + tt.wantPath

			got, err := tt.p.Get(context.Background(), tt.currency, tt.chain, Token{})
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

func TestPairMatchesMarket(t *testing.T) {
	tests := []struct {
		pair  string
		base  string
		quote string
		want  bool
	}{
		{"BTCUSD", "BTC", "USD", true},
		{"BTC_TRY", "BTC", "TRY", true},
		{"BTCJPY", "BTC", "JPY", true},
		{"XBTUSD", "XBT", "USD", true},
		{"ETHUSDT", "ETH", "USDT", true},
		{"BTCUSDT", "BTC", "USDT", true},
		{"BTCUSDT", "BTC", "USD", false},
		{"ETHUSDT", "ETH", "USD", false},
		{"EURUSD", "BTC", "USD", false},
		{"BTC", "BTC", "USD", false},
		{"BTCUSD", "BTC", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.pair+tt.base+tt.quote, func(t *testing.T) {
			if got := pairMatchesMarket(tt.pair, tt.base, tt.quote); got != tt.want {
				t.Fatalf("pairMatchesMarket(%q, %q, %q) = %v want %v", tt.pair, tt.base, tt.quote, got, tt.want)
			}
		})
	}
}

func TestPairMatchesKraken(t *testing.T) {
	tests := []struct {
		pair  string
		base  string
		quote string
		want  bool
	}{
		{"XXBTZUSD", "XBT", "USD", true},
		{"XETHZUSD", "ETH", "USD", true},
		{"XXRPZUSD", "XRP", "USD", true},
		{"XZECZUSD", "ZEC", "USD", true},
		{"SOLUSD", "SOL", "USD", true},
		{"AAVEEUR", "AAVE", "EUR", true},
		{"XBTUSD", "XBT", "USD", true},
		{"BTCUSD", "XBT", "USD", false},
		{"XBTUSDT", "XBT", "USD", false},
		{"XXBTZUSD", "XBT", "JPY", false},
		{"XETHZUSD", "XBT", "USD", false},
	}

	for _, tt := range tests {
		t.Run(tt.pair+tt.base+tt.quote, func(t *testing.T) {
			if got := pairMatchesKraken(tt.pair, tt.base, tt.quote); got != tt.want {
				t.Fatalf("pairMatchesKraken(%q, %q, %q) = %v want %v", tt.pair, tt.base, tt.quote, got, tt.want)
			}
		})
	}
}

func TestParseKrakenTicker(t *testing.T) {
	payload := map[string]any{
		"result": map[string]any{
			"XXBTZUSD": map[string]any{
				"b": []any{"50000.0", "1", "2"},
				"a": []any{"50001.0", "1", "2"},
			},
			"XXBTZJPY": map[string]any{
				"b": []any{"15000000.0", "1", "2"},
				"a": []any{"15000002.0", "1", "2"},
			},
			"XETHZUSD": map[string]any{
				"b": []any{"3000.0", "1", "2"},
				"a": []any{"3001.0", "1", "2"},
			},
		},
	}

	if got, ok := parseKrakenTicker(payload, "BTC", USD); !ok || got != 5000050 {
		t.Fatalf("BTC/USD = (%d, %v) want (5000050, true)", got, ok)
	}

	if got, ok := parseKrakenTicker(payload, "BTC", JPY); !ok || got != 15000001 {
		t.Fatalf("BTC/JPY = (%d, %v) want (15000001, true)", got, ok)
	}

	if got, ok := parseKrakenTicker(payload, "XMR", USD); ok {
		t.Fatalf("XMR/USD should not be found, got (%d, true)", got)
	}

	if got, ok := parseKrakenTicker(map[string]any{"result": map[string]any{}}, "BTC", USD); ok {
		t.Fatalf("BTC/USD on empty result should not be found, got (%d, true)", got)
	}
}

func TestParseKrakenTickerWsname(t *testing.T) {
	// websockets/v2 style payloads expose an exact wsname per row.
	payload := map[string]any{
		"result": map[string]any{
			"XBTUSDX": map[string]any{
				"wsname": "XBT/USD",
				"b":      []any{"50000.0", "1", "2"},
				"a":      []any{"50002.0", "1", "2"},
			},
		},
	}

	if got, ok := parseKrakenTicker(payload, "BTC", USD); !ok || got != 5000100 {
		t.Fatalf("BTC/USD via wsname = (%d, %v) want (5000100, true)", got, ok)
	}

	// the wsname is authoritative: an otherwise-matching key is rejected
	// when the wsname points at a different pair.
	wrong := map[string]any{
		"result": map[string]any{
			"XBTUSD": map[string]any{
				"wsname": "XBT/JPY",
				"b":      []any{"50000.0", "1", "2"},
				"a":      []any{"50002.0", "1", "2"},
			},
		},
	}

	if got, ok := parseKrakenTicker(wrong, "BTC", USD); ok {
		t.Fatalf("wsname mismatch should not be matched, got (%d, true)", got)
	}
}

func TestParseBitflyerTicker(t *testing.T) {
	payload := map[string]any{
		"product_code": "BTC_JPY",
		"best_bid":     15000000.0,
		"best_ask":     15000002.0,
	}

	if got, ok := parseBitflyerTicker(payload, "BTC", JPY); !ok || got != 15000001 {
		t.Fatalf("BTC/JPY = (%d, %v) want (15000001, true)", got, ok)
	}

	// currency mismatch must be rejected instead of returning a JPY price
	if got, ok := parseBitflyerTicker(payload, "BTC", USD); ok {
		t.Fatalf("BTC/USD should not be served a JPY price, got (%d, true)", got)
	}

	// asset mismatch must be rejected
	if got, ok := parseBitflyerTicker(payload, "ETH", JPY); ok {
		t.Fatalf("ETH/JPY should not be served a BTC price, got (%d, true)", got)
	}
}

func TestParseBitpayRates(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{"code": "USD", "name": "US Dollar", "rate": "50000.00"},
			map[string]any{"code": "EUR", "name": "Euro", "rate": "42000.00"},
			map[string]any{"code": "BTC", "name": "Bitcoin", "rate": 1},
		},
	}

	if got, ok := parseBitpayRates(payload, "BTC", USD); !ok || got != 5000000 {
		t.Fatalf("BTC/USD = (%d, %v) want (5000000, true)", got, ok)
	}

	if got, ok := parseBitpayRates(payload, "BTC", EUR); !ok || got != 4200000 {
		t.Fatalf("BTC/EUR = (%d, %v) want (4200000, true)", got, ok)
	}

	// bitpay only publishes BTC rates
	if got, ok := parseBitpayRates(payload, "LTC", USD); ok {
		t.Fatalf("LTC/USD should not be found, got (%d, true)", got)
	}
}

func TestParseCoinmateTicker(t *testing.T) {
	payload := map[string]any{
		"error": false,
		"data": map[string]any{
			"BTC_EUR": map[string]any{"bid": 50000.0, "ask": 50002.0},
			"BTC_CZK": map[string]any{"bid": 1000000.0, "ask": 1000002.0},
		},
	}

	if got, ok := parseCoinmateTicker(payload, "BTC", EUR); !ok || got != 5000100 {
		t.Fatalf("BTC/EUR = (%d, %v) want (5000100, true)", got, ok)
	}

	if got, ok := parseCoinmateTicker(payload, "BTC", Currency{Code: "CZK", Decimals: 2}); !ok || got != 100000100 {
		t.Fatalf("BTC/CZK = (%d, %v) want (100000100, true)", got, ok)
	}

	if got, ok := parseCoinmateTicker(payload, "ETH", EUR); ok {
		t.Fatalf("ETH/EUR should not be found, got (%d, true)", got)
	}
}

func TestParseCryptoMarketTicker(t *testing.T) {
	payload := map[string]any{
		"BTCARS": map[string]any{"bid": 100.0, "ask": 100.5, "last": 100.1},
		"BTCCLP": map[string]any{"bid": 1.0, "ask": 1.1, "last": 1.05},
	}

	if got, ok := parseCryptoMarketTicker(payload, "BTC", Currency{Code: "ARS", Decimals: 2}); !ok || got != 10025 {
		t.Fatalf("BTC/ARS = (%d, %v) want (10025, true)", got, ok)
	}

	if got, ok := parseCryptoMarketTicker(payload, "BTC", Currency{Code: "CLP", Decimals: 0}); !ok || got != 1 {
		t.Fatalf("BTC/CLP = (%d, %v) want (1, true)", got, ok)
	}

	// cryptomarket does not publish USD pairs
	if got, ok := parseCryptoMarketTicker(payload, "BTC", USD); ok {
		t.Fatalf("BTC/USD should not be found, got (%d, true)", got)
	}
}

func TestMarketProviderUsesTokenSymbol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rates/usdt/price" {
			t.Fatalf("path = %q", r.URL.Path)
		}

		if _, err := w.Write([]byte(`{"data":{"USD":1.00}}`)); err != nil {
			t.Fatalf("failed to write: %+v", err)
		}
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

// cannedTransport serves a fixed body for every request, so tests can hit
// URL-based providers without network access.
type cannedTransport struct {
	body string
	urls []string
}

func (c *cannedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.urls = append(c.urls, r.URL.String())
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(c.body)),
	}, nil
}

func TestMarketProviderQuotesRequestedCurrency(t *testing.T) {
	tests := []struct {
		name         string
		p            marketProvider
		currency     Currency
		chain        string
		responseBody string
		want         int64
	}{
		{
			name:         "bitnob-usd-from-map",
			p:            NewBitnobPriceProvider(nil).(marketProvider),
			currency:     USD,
			chain:        "LTC",
			responseBody: `{"data":{"USD":50000,"NGN":80000000}}`,
			want:         5000000,
		},
		{
			name:         "bitnob-ngn-from-map",
			p:            NewBitnobPriceProvider(nil).(marketProvider),
			currency:     Currency{Code: "NGN", Decimals: 2},
			chain:        "LTC",
			responseBody: `{"data":{"USD":50000,"NGN":80000000}}`,
			want:         8000000000,
		},
		{
			name:         "bb-no-nok",
			p:            NewBareBitcoinPriceProvider(nil).(marketProvider),
			currency:     Currency{Code: "NOK", Decimals: 2},
			chain:        "BTC",
			responseBody: `{"bid":500000,"ask":500020}`,
			want:         50001000,
		},
		{
			name:         "bitmynt-nok",
			p:            NewBitmyntPriceProvider(nil).(marketProvider),
			currency:     Currency{Code: "NOK", Decimals: 2},
			chain:        "BTC",
			responseBody: `{"current_rate":{"bid":500000,"ask":500020}}`,
			want:         50001000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &cannedTransport{body: tt.responseBody}
			tt.p.client = httpClient(&http.Client{Transport: transport})

			got, err := tt.p.Get(context.Background(), tt.currency, tt.chain, Token{})
			if err != nil {
				t.Fatalf("Get() error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("Get() = %d want %d", got, tt.want)
			}

			if len(transport.urls) != 1 {
				t.Fatalf("expected 1 request, got %d", len(transport.urls))
			}
		})
	}
}

func TestMarketProviderRejectsUnsupportedCurrency(t *testing.T) {
	tests := []struct {
		name         string
		p            marketProvider
		currency     Currency
		chain        string
		responseBody string
	}{
		{
			name:         "bitnob-usd-missing",
			p:            NewBitnobPriceProvider(nil).(marketProvider),
			currency:     USD,
			chain:        "LTC",
			responseBody: `{"data":{"NGN":80000000}}`,
		},
		{
			name:         "bb-no-usd",
			p:            NewBareBitcoinPriceProvider(nil).(marketProvider),
			currency:     USD,
			chain:        "BTC",
			responseBody: `{"bid":500000,"ask":500020}`,
		},
		{
			name:         "bb-no-eur",
			p:            NewBareBitcoinPriceProvider(nil).(marketProvider),
			currency:     EUR,
			chain:        "BTC",
			responseBody: `{"bid":500000,"ask":500020}`,
		},
		{
			name:         "bitmynt-usd",
			p:            NewBitmyntPriceProvider(nil).(marketProvider),
			currency:     USD,
			chain:        "BTC",
			responseBody: `{"current_rate":{"bid":500000,"ask":500020}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.p.client = httpClient(&http.Client{Transport: &cannedTransport{body: tt.responseBody}})

			got, err := tt.p.Get(context.Background(), tt.currency, tt.chain, Token{})
			if err == nil {
				t.Fatalf("Get() = %d, want error", got)
			}
		})
	}
}
