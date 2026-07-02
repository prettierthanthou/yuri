package yuri

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

var _ PriceProvider = coinGeckoProvider{}

var commonFiatCurrencies = []string{
	"aed", "ars", "aud", "bdt", "bhd", "bmd", "brl", "cad", "chf", "clp", "cny", "czk", "dkk", "eur",
	"gbp", "gel", "hkd", "huf", "idr", "ils", "inr", "jpy", "krw", "kwd", "lkr", "mmk", "mxn", "myr",
	"ngn", "nok", "nzd", "php", "pkr", "pln", "rub", "sar", "sek", "sgd", "thb", "try", "twd", "uah",
	"usd", "vef", "vnd", "zar", "xag", "xau", "xdr",
}

type coinGeckoProvider struct {
	client *http.Client
}

type marketProvider struct {
	client *http.Client
	url    string
	kind   string
}

func assetSymbol(chain string, token Token) string {
	if token != (Token{}) && token.Symbol != "" {
		return strings.ToUpper(token.Symbol)
	}

	return strings.ToUpper(chain)
}

func marketSymbol(chain string, token Token) string {
	if token != (Token{}) && token.Symbol != "" {
		return strings.ToUpper(token.Symbol)
	}

	return strings.ToUpper(chain)
}

func pairMatchesMarket(pair, base, quote string) bool {
	pair = strings.ToUpper(strings.ReplaceAll(pair, "_", ""))
	base = strings.ToUpper(base)
	quote = strings.ToUpper(quote)

	return strings.HasPrefix(pair, base) && strings.HasSuffix(pair, quote)
}

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}

	return http.DefaultClient
}

func parseBody(resp *http.Response, out any) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		return err
	}

	return json.Unmarshal(body, out)
}

func wantFiat(currency Currency) bool {
	return slices.Contains(commonFiatCurrencies, strings.ToLower(currency.Code))
}

func getJSON(ctx context.Context, client *http.Client, raw string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return ChainNotSupportedErr
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return errors.New("pricing provider rate limited")
	}

	return parseBody(resp, out)
}

func buildURL(base, path string, query map[string]string) string {
	u, _ := url.Parse(base)
	u.Path = path
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func minor(currency Currency, rate float64) int64 {
	return currency.ToMinor(rate)
}

func pickRate(m map[string]float64, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := m[strings.ToLower(k)]; ok {
			return v, true
		}
		if v, ok := m[strings.ToUpper(k)]; ok {
			return v, true
		}
	}
	return 0, false
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func NewCoinGeckoPriceProvider(client *http.Client) PriceProvider {
	return coinGeckoProvider{client: client}
}

func NewBtcTurkPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.btcturk.com/api/v2/ticker"}
}

func NewBareBitcoinPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.bb.no/v1/price/nok"}
}

func NewBitbankPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://public.bitbank.cc/tickers"}
}

func NewBitcoinKenyaPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://trex.bitcoin.co.ke/btcpay/rates"}
}

func NewBitflyerPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.bitflyer.jp/v1/ticker"}
}

func NewBitmyntPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://ny.bitmynt.no/data/rates.json"}
}

func NewBitnobPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.bitnob.co/api/v1/rates/bitcoin/price", kind: "bitnob"}
}

func NewBitpayPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://bitpay.com/rates"}
}

func NewBudaPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://www.buda.com/api/v2/markets/btc-clp/ticker", kind: "buda"}
}

func NewByllsPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://bylls.com/api/price?from_currency=BTC&to_currency=CAD", kind: "bylls"}
}

func NewCoinDCXPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.coindcx.com/exchange/ticker"}
}

func NewCoinmatePriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://coinmate.io/api/tickerAll"}
}

func NewCryptoMarketPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.exchange.cryptomkt.com/api/3/public/ticker/"}
}

func NewDesiboardPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://desiboard.thevikas.com/api/price"}
}

func NewFreeCurrencyRatesPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://currency-api.pages.dev/v1/currencies/btc.min.json", kind: "freecurrencyrates"}
}

func NewHitBTCPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.hitbtc.com/api/2/public/ticker"}
}

func NewKrakenPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.kraken.com/0/public/Ticker"}
}

func NewLunoPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.luno.com/api/1/tickers"}
}

func NewRipioPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.ripiotrade.co/v4/public/tickers"}
}

func NewYadioPriceProvider(client *http.Client) PriceProvider {
	return marketProvider{client: client, url: "https://api.yadio.io/exrates/BTC", kind: "yadio"}
}

func NewNullPriceProvider() PriceProvider { return nullProvider{} }

func (p coinGeckoProvider) Get(ctx context.Context, currency Currency, chain string, token Token) (int64, error) {
	if !wantFiat(currency) {
		return -1, FiatCurrencyNotSupportedErr
	}

	if token != (Token{}) {
		return geckoToken(ctx, httpClient(p.client), currency, chain, token)
	}

	raw := buildURL("https://api.coingecko.com", "/api/v3/simple/price", map[string]string{
		"ids":           strings.ToLower(chain),
		"vs_currencies": strings.ToLower(currency.Code),
	})

	var parsed map[string]map[string]float64
	if err := getJSON(ctx, httpClient(p.client), raw, &parsed); err != nil {
		return -1, err
	}

	row, ok := parsed[strings.ToLower(chain)]
	if !ok {
		return -1, ChainNotSupportedErr
	}

	rate, ok := pickRate(row, currency.Code)
	if !ok {
		return -1, fmt.Errorf("missing price from coingecko response")
	}

	return minor(currency, rate), nil
}

func geckoToken(ctx context.Context, client *http.Client, currency Currency, chain string, token Token) (int64, error) {
	raw := buildURL("https://api.coingecko.com", "/api/v3/simple/token_price/"+strings.ToLower(chain), map[string]string{
		"contract_addresses": token.Contract,
		"vs_currencies":      strings.ToLower(currency.Code),
	})

	var parsed map[string]map[string]float64
	if err := getJSON(ctx, client, raw, &parsed); err != nil {
		return -1, err
	}
	row, ok := parsed[strings.ToLower(token.Contract)]
	if !ok {
		return -1, ChainNotSupportedErr
	}
	rate, ok := pickRate(row, currency.Code)
	if !ok {
		return -1, fmt.Errorf("missing token price from coingecko response")
	}
	return minor(currency, rate), nil
}

type nullProvider struct{}

func (nullProvider) Get(context.Context, Currency, string, Token) (int64, error) { return 0, nil }

func (p marketProvider) Get(ctx context.Context, currency Currency, chain string, token Token) (int64, error) {
	if !wantFiat(currency) {
		return -1, FiatCurrencyNotSupportedErr
	}

	client := httpClient(p.client)
	base := assetSymbol(chain, token)
	market := marketSymbol(chain, token)
	requestURL := p.url
	switch p.kind {
	case "bitnob":
		requestURL = buildURL(p.url, "/api/v1/rates/"+strings.ToLower(market)+"/price", nil)
	case "bylls":
		requestURL = buildURL(p.url, "/api/price", map[string]string{
			"from_currency": market,
			"to_currency":   strings.ToUpper(currency.Code),
		})
	case "freecurrencyrates":
		requestURL = buildURL(p.url, "/v1/currencies/"+strings.ToLower(market)+".min.json", nil)
	case "yadio":
		requestURL = buildURL(p.url, "/exrates/"+market, nil)
	case "buda":
		requestURL = buildURL(p.url, "/api/v2/markets/"+strings.ToLower(market)+"-"+strings.ToLower(currency.Code)+"/ticker", nil)
	}

	var payload any
	if err := getJSON(ctx, client, requestURL, &payload); err != nil {
		return -1, err
	}

	switch p.kind {
	case "bylls":
		if m, ok := asMap(payload); ok {
			if pub, ok := pickAnyMap(m, "public_price"); ok {
				if pubm, ok := asMap(pub); ok {
					return numberToMinor(currency, pubm["to_price"])
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "freecurrencyrates":
		if m, ok := asMap(payload); ok {
			if row, ok := pickAnyMap(m, market, strings.ToLower(market)); ok {
				if btc, ok := asMap(row); ok {
					if v, ok := pickAnyNumber(btc, currency.Code); ok {
						return numberToMinor(currency, v)
					}
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "yadio":
		if m, ok := asMap(payload); ok {
			if row, ok := pickAnyMap(m, market, strings.ToLower(market)); ok {
				if btc, ok := asMap(row); ok {
					if v, ok := pickAnyNumber(btc, currency.Code); ok {
						return numberToMinor(currency, v)
					}
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "bitnob":
		if m, ok := asMap(payload); ok {
			if data, ok := asMap(m["data"]); ok {
				for _, v := range data {
					return numberToMinor(currency, v)
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "buda":
		if m, ok := asMap(payload); ok {
			if ticker, ok := pickAnyMap(m, "ticker"); ok {
				if tickerm, ok := asMap(ticker); ok {
					return valueFromStrings(currency, tickerm["max_bid"], tickerm["min_ask"])
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	}

	switch p.url {
	case "https://api.bb.no/v1/price/nok":
		m := payload.(map[string]any)
		return valueFromAny(m["bid"], m["ask"], currency)
	case "https://api.btcturk.com/api/v2/ticker":
		m := payload.(map[string]any)
		if data, ok := m["data"].([]any); ok {
			for _, item := range data {
				row := item.(map[string]any)
				if pairMatchesMarket(fmt.Sprint(row["pairNormalized"]), base, currency.Code) {
					return valueFromAny(row["bid"], row["ask"], currency)
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://public.bitbank.cc/tickers":
		m := payload.(map[string]any)
		if data, ok := m["data"].([]any); ok {
			for _, item := range data {
				row := item.(map[string]any)
				if pairMatchesMarket(fmt.Sprint(row["pair"]), base, currency.Code) {
					return valueFromAny(row["buy"], row["sell"], currency)
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://trex.bitcoin.co.ke/btcpay/rates":
		m := payload.(map[string]any)
		if btc, ok := m["BTC"].(map[string]any); ok {
			if v, ok := pickAnyNumber(btc, currency.Code); ok {
				return numberToMinor(currency, v)
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://ny.bitmynt.no/data/rates.json":
		m := payload.(map[string]any)
		cur := m["current_rate"].(map[string]any)
		return valueFromAny(cur["bid"], cur["ask"], currency)
	case "https://bylls.com/api/price?from_currency=BTC&to_currency=CAD":
		m := payload.(map[string]any)
		pub := m["public_price"].(map[string]any)
		return numberToMinor(currency, pub["to_price"])
	case "https://currency-api.pages.dev/v1/currencies/btc.min.json":
		m := payload.(map[string]any)
		btc := m["btc"].(map[string]any)
		if v, ok := pickAnyNumber(btc, currency.Code); ok {
			return numberToMinor(currency, v)
		}
	case "https://desiboard.thevikas.com/api/price":
		m := payload.(map[string]any)
		if v, ok := pickAnyNumber(m, "BTC"+currency.Code); ok {
			return numberToMinor(currency, v)
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.yadio.io/exrates/BTC":
		m := payload.(map[string]any)
		btc := m["BTC"].(map[string]any)
		if v, ok := pickAnyNumber(btc, currency.Code); ok {
			return numberToMinor(currency, v)
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.bitflyer.jp/v1/ticker":
		m := payload.(map[string]any)
		if strings.Contains(strings.ToUpper(fmt.Sprint(m["product_code"])), assetSymbol(chain, token)) {
			return valueFromAny(m["best_bid"], m["best_ask"], currency)
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.bitpay.com/rates":
		m := payload.(map[string]any)
		if data, ok := m["data"].([]any); ok {
			for _, item := range data {
				row := item.(map[string]any)
				if strings.EqualFold(fmt.Sprint(row["code"]), currency.Code) {
					return numberToMinor(currency, row["rate"])
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.coindcx.com/exchange/ticker":
		if data, ok := payload.([]any); ok {
			for _, item := range data {
				row := item.(map[string]any)
				market := fmt.Sprint(row["market"])
				if market == "" {
					continue
				}

				bid, bok := asFloat(row["bid"])
				ask, aok := asFloat(row["ask"])
				if bok && aok && pairMatchesMarket(market, base, currency.Code) {
					return numberToMinor(currency, (bid+ask)/2)
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://coinmate.io/api/tickerAll":
		m := payload.(map[string]any)
		if data, ok := m["data"].(map[string]any); ok {
			for _, v := range data {
				row := v.(map[string]any)
				if pairMatchesMarket(fmt.Sprint(row["pair"]), base, currency.Code) {
					return valueFromAny(row["bid"], row["ask"], currency)
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.exchange.cryptomkt.com/api/3/public/ticker/":
		m := payload.(map[string]any)
		for _, pair := range []string{base + "ARS", base + "CLP", base + "BRL"} {
			if row, ok := m[pair].(map[string]any); ok {
				if bid, bok := asFloat(row["bid"]); bok {
					if ask, aok := asFloat(row["ask"]); aok {
						return numberToMinor(currency, (bid+ask)/2)
					}
				}

				if last, ok := asFloat(row["last"]); ok {
					return numberToMinor(currency, last)
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.hitbtc.com/api/2/public/ticker":
		if data, ok := payload.([]any); ok {
			for _, item := range data {
				row := item.(map[string]any)
				if pairMatchesMarket(fmt.Sprint(row["symbol"]), base, currency.Code) {
					return valueFromStrings(currency, row["bid"], row["ask"])
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.kraken.com/0/public/Ticker":
		m := payload.(map[string]any)
		if result, ok := m["result"].(map[string]any); ok {
			for _, rowv := range result {
				row := rowv.(map[string]any)
				return valueFromStrings(currency, row["b"].([]any), row["a"].([]any))
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.luno.com/api/1/tickers":
		m := payload.(map[string]any)
		if data, ok := m["tickers"].([]any); ok {
			for _, item := range data {
				row := item.(map[string]any)
				if pairMatchesMarket(fmt.Sprint(row["pair"]), base, currency.Code) {
					bid, bok := asFloat(row["bid"])
					ask, aok := asFloat(row["ask"])
					if bok && aok && bid <= ask {
						return numberToMinor(currency, (bid+ask)/2)
					}
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.ripiotrade.co/v4/public/tickers":
		m := payload.(map[string]any)
		if data, ok := m["data"].([]any); ok {
			for _, item := range data {
				row := item.(map[string]any)
				if pairMatchesMarket(fmt.Sprint(row["pair"]), base, currency.Code) {
					bid, bok := asFloat(row["bid"])
					ask, aok := asFloat(row["ask"])
					if bok && aok && bid <= ask {
						return numberToMinor(currency, (bid+ask)/2)
					}
				}
			}
		}
		return -1, ChainNotSupportedErr
	}

	return -1, unsupportedPairError(chain, token, currency)
}

func unsupportedPairError(chain string, token Token, currency Currency) error {
	if token != (Token{}) {
		return fmt.Errorf("%w: %s token %s/%s", ChainNotSupportedErr, chain, token.Symbol, currency.Code)
	}
	return fmt.Errorf("%w: %s/%s", ChainNotSupportedErr, chain, currency.Code)
}

func valueFromAny(bid any, ask any, currency Currency) (int64, error) {
	b, bok := asFloat(bid)
	a, aok := asFloat(ask)
	if !bok || !aok {
		return -1, ChainNotSupportedErr
	}

	return numberToMinor(currency, (b+a)/2)
}

func valueFromStrings(currency Currency, bid any, ask any) (int64, error) {
	return valueFromAny(bid, ask, currency)
}

func pickAnyNumber(m map[string]any, key string) (any, bool) {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}

	return nil, false
}

func pickAnyMap(m map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if v, ok := pickAnyNumber(m, key); ok {
			return v, true
		}
	}

	return nil, false
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	case []any:
		if len(t) == 0 {
			return 0, false
		}

		return asFloat(t[0])
	default:
		return 0, false
	}
}

func numberToMinor(currency Currency, v any) (int64, error) {
	f, ok := asFloat(v)
	if !ok {
		return -1, ChainNotSupportedErr
	}

	return currency.ToMinor(f), nil
}
