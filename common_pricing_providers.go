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
	"time"
)

var _ PriceProvider = coinGeckoProvider{}

func (m coinGeckoProvider) WantsFullChainName() bool {
	return true
}

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

func (m marketProvider) WantsFullChainName() bool {
	return false
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

	if base == "" || quote == "" {
		return false
	}

	// require an exact length match so that for example "BTCUSDT" is
	// never treated as a "BTC/USD" pair.
	return len(pair) == len(base)+len(quote) &&
		strings.HasPrefix(pair, base) &&
		strings.HasSuffix(pair, quote)
}

var defaultHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
}

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}

	return defaultHTTPClient
}

func parseBody(resp *http.Response, out any) (err error) {
	defer func() {
		if cerr := resp.Body.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	// 5 MB
	const maxResponse = 5 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))

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
		if err = resp.Body.Close(); err != nil {
			return err
		}

		return ErrChainNotSupported
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		if err = resp.Body.Close(); err != nil {
			return err
		}

		return errors.New("pricing provider rate limited")
	}

	if resp.StatusCode != http.StatusOK {
		if err = resp.Body.Close(); err != nil {
			return err
		}

		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
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

func NewNullPriceProvider() PriceProvider               { return staticProvider{amount: 0} }
func NewStaticPriceProvider(amount int64) PriceProvider { return staticProvider{amount: amount} }

func (p coinGeckoProvider) Get(ctx context.Context, currency Currency, chain string, token Token) (int64, error) {
	if !wantFiat(currency) {
		return -1, ErrFiatCurrencyNotSupported
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
		return -1, ErrChainNotSupported
	}

	rate, ok := pickRate(row, currency.Code)
	if !ok {
		return -1, fmt.Errorf("missing price from coingecko response")
	}

	return currency.ToMinor(rate), nil
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
		return -1, ErrChainNotSupported
	}

	rate, ok := pickRate(row, currency.Code)
	if !ok {
		return -1, fmt.Errorf("missing token price from coingecko response")
	}

	return currency.ToMinor(rate), nil
}

type staticProvider struct{ amount int64 }

func (staticProvider) WantsFullChainName() bool { return false }

func (s staticProvider) Get(context.Context, Currency, string, Token) (int64, error) {
	return s.amount, nil
}

func (p marketProvider) Get(ctx context.Context, currency Currency, chain string, token Token) (int64, error) {
	if !wantFiat(currency) {
		return -1, ErrFiatCurrencyNotSupported
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
		if m, ok := asMap(payload); ok {
			return valueFromAny(m["bid"], m["ask"], currency)
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.btcturk.com/api/v2/ticker":
		if m, ok := asMap(payload); ok {
			if data, ok := m["data"].([]any); ok {
				for _, item := range data {
					if row, ok := item.(map[string]any); ok {
						if pairMatchesMarket(fmt.Sprint(row["pairNormalized"]), base, currency.Code) {
							return valueFromAny(row["bid"], row["ask"], currency)
						}
					}
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://public.bitbank.cc/tickers":
		if m, ok := asMap(payload); ok {
			if data, ok := m["data"].([]any); ok {
				for _, item := range data {
					if row, ok := item.(map[string]any); ok {
						if pairMatchesMarket(fmt.Sprint(row["pair"]), base, currency.Code) {
							return valueFromAny(row["buy"], row["sell"], currency)
						}
					}
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://trex.bitcoin.co.ke/btcpay/rates":
		if m, ok := asMap(payload); ok {
			if btc, ok := m["BTC"].(map[string]any); ok {
				if v, ok := pickAnyNumber(btc, currency.Code); ok {
					return numberToMinor(currency, v)
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://ny.bitmynt.no/data/rates.json":
		if m, ok := asMap(payload); ok {
			if cur, ok := m["current_rate"].(map[string]any); ok {
				return valueFromAny(cur["bid"], cur["ask"], currency)
			}
		}
	case "https://bylls.com/api/price?from_currency=BTC&to_currency=CAD":
		if m, ok := asMap(payload); ok {
			if pub, ok := m["public_price"].(map[string]any); ok {
				return numberToMinor(currency, pub["to_price"])
			}
		}
	case "https://currency-api.pages.dev/v1/currencies/btc.min.json":
		if m, ok := asMap(payload); ok {
			if btc, ok := m["btc"].(map[string]any); ok {
				if v, ok := pickAnyNumber(btc, currency.Code); ok {
					return numberToMinor(currency, v)
				}
			}
		}
	case "https://desiboard.thevikas.com/api/price":
		if m, ok := asMap(payload); ok {
			if v, ok := pickAnyNumber(m, "BTC"+currency.Code); ok {
				return numberToMinor(currency, v)
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.yadio.io/exrates/BTC":
		if m, ok := asMap(payload); ok {
			if btc, ok := m["BTC"].(map[string]any); ok {
				if v, ok := pickAnyNumber(btc, currency.Code); ok {
					return numberToMinor(currency, v)
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.bitflyer.jp/v1/ticker":
		if v, ok := parseBitflyerTicker(payload, base, currency); ok {
			return v, nil
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://bitpay.com/rates":
		if v, ok := parseBitpayRates(payload, base, currency); ok {
			return v, nil
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.coindcx.com/exchange/ticker":
		if data, ok := payload.([]any); ok {
			for _, item := range data {
				if row, ok := item.(map[string]any); ok {
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
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://coinmate.io/api/tickerAll":
		if v, ok := parseCoinmateTicker(payload, base, currency); ok {
			return v, nil
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.exchange.cryptomkt.com/api/3/public/ticker/":
		if v, ok := parseCryptoMarketTicker(payload, base, currency); ok {
			return v, nil
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.hitbtc.com/api/2/public/ticker":
		if data, ok := payload.([]any); ok {
			for _, item := range data {
				if row, ok := item.(map[string]any); ok {
					if pairMatchesMarket(fmt.Sprint(row["symbol"]), base, currency.Code) {
						return valueFromStrings(currency, row["bid"], row["ask"])
					}
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.kraken.com/0/public/Ticker":
		if v, ok := parseKrakenTicker(payload, base, currency); ok {
			return v, nil
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.luno.com/api/1/tickers":
		if m, ok := asMap(payload); ok {
			if data, ok := m["tickers"].([]any); ok {
				for _, item := range data {
					if row, ok := item.(map[string]any); ok {
						if pairMatchesMarket(fmt.Sprint(row["pair"]), base, currency.Code) {
							bid, bok := asFloat(row["bid"])
							ask, aok := asFloat(row["ask"])
							if bok && aok && bid <= ask {
								return numberToMinor(currency, (bid+ask)/2)
							}
						}
					}
				}
			}
		}
		return -1, unsupportedPairError(chain, token, currency)
	case "https://api.ripiotrade.co/v4/public/tickers":
		if m, ok := asMap(payload); ok {
			if data, ok := m["data"].([]any); ok {
				for _, item := range data {
					if row, ok := item.(map[string]any); ok {
						if pairMatchesMarket(fmt.Sprint(row["pair"]), base, currency.Code) {
							bid, bok := asFloat(row["bid"])
							ask, aok := asFloat(row["ask"])
							if bok && aok && bid <= ask {
								return numberToMinor(currency, (bid+ask)/2)
							}
						}
					}
				}
			}
		}
		return -1, ErrChainNotSupported
	}

	return -1, unsupportedPairError(chain, token, currency)
}

// krakenAssetSymbol converts commonly used upstream symbols into the
// naming Kraken uses for its pairs. Bitcoin is "XBT" on Kraken.
func krakenAssetSymbol(symbol string) string {
	if strings.EqualFold(symbol, "BTC") {
		return "XBT"
	}

	return symbol
}

// pairMatchesKraken matches a Kraken pair key against a base/quote.
//
// Kraken naming is inconsistent across pairs: the api.kraken.com Ticker
// response uses one of "BASEQUOTE" (e.g. "SOLUSD", "AAVEEUR"),
// "BASEZQUOTE" (e.g. "XBTZUSD"), or "XBASEZQUOTE" (e.g. "XXBTZUSD",
// "XETHZUSD", "XXRPZUSD") depending on the asset.
func pairMatchesKraken(pair, base, quote string) bool {
	pair = strings.ToUpper(strings.ReplaceAll(pair, "_", ""))
	base = strings.ToUpper(base)
	quote = strings.ToUpper(quote)

	if base == "" || quote == "" {
		return false
	}

	return pair == base+quote ||
		pair == base+"Z"+quote ||
		pair == "X"+base+quote ||
		pair == "X"+base+"Z"+quote
}

// parseKrakenTicker finds the requested base/quote pair inside the
// `result` map of a Kraken /0/public/Ticker response.
//
// Both the `wsname` field (e.g. "XBT/USD") used by newer Kraken
// endpoints and the pair key itself are treated as authoritative. Only
// an exact base/quote match is accepted so a random ticker is never
// returned.
func parseKrakenTicker(payload any, base string, currency Currency) (int64, bool) {
	m, ok := asMap(payload)
	if !ok {
		return 0, false
	}

	result, ok := asMap(m["result"])
	if !ok {
		return 0, false
	}

	base = krakenAssetSymbol(base)

	for pair, rowv := range result {
		row, ok := rowv.(map[string]any)
		if !ok {
			continue
		}

		matched := false
		if ws, ok := row["wsname"].(string); ok {
			// when present the wsname is authoritative: a mismatching
			// wsname must never fall back to the pair key.
			parts := strings.Split(ws, "/")
			matched = len(parts) == 2 &&
				strings.EqualFold(strings.TrimSpace(parts[0]), base) &&
				strings.EqualFold(strings.TrimSpace(parts[1]), currency.Code)
		} else {
			matched = pairMatchesKraken(pair, base, currency.Code)
		}

		if !matched {
			continue
		}

		v, err := valueFromAny(row["b"], row["a"], currency)
		if err == nil {
			return v, true
		}
	}

	return 0, false
}

// parseBitflyerTicker matches a single-product Bitflyer /v1/ticker
// response against the requested base/quote. The product code is matched
// exactly so a request for XMR/USD can never be served an XMR_JPY price.
func parseBitflyerTicker(payload any, base string, currency Currency) (int64, bool) {
	m, ok := asMap(payload)
	if !ok {
		return 0, false
	}

	if !pairMatchesMarket(fmt.Sprint(m["product_code"]), base, currency.Code) {
		return 0, false
	}

	v, err := valueFromAny(m["best_bid"], m["best_ask"], currency)
	return v, err == nil
}

// parseBitpayRates matches a BitPay /rates response against the requested
// base/quote. BitPay only publishes BTC rates, so any other base is not
// supported by this provider.
func parseBitpayRates(payload any, base string, currency Currency) (int64, bool) {
	if !strings.EqualFold(base, "BTC") {
		return 0, false
	}

	m, ok := asMap(payload)
	if !ok {
		return 0, false
	}

	data, ok := m["data"].([]any)
	if !ok {
		return 0, false
	}

	for _, item := range data {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if !strings.EqualFold(fmt.Sprint(row["code"]), currency.Code) {
			continue
		}

		v, err := numberToMinor(currency, row["rate"])
		return v, err == nil
	}

	return 0, false
}

// parseCryptoMarketTicker matches a CryptoMarket /ticker/ response against
// the requested base/quote. CryptoMarket only lists ARS, CLP and BRL
// pairs, so any other requested currency is not supported.
func parseCryptoMarketTicker(payload any, base string, currency Currency) (int64, bool) {
	m, ok := asMap(payload)
	if !ok {
		return 0, false
	}

	for _, pair := range []string{base + "ARS", base + "CLP", base + "BRL"} {
		if !strings.EqualFold(strings.TrimPrefix(pair, base), currency.Code) {
			continue
		}

		row, ok := m[pair].(map[string]any)
		if !ok {
			continue
		}

		if bid, bok := asFloat(row["bid"]); bok {
			if ask, aok := asFloat(row["ask"]); aok {
				v, err := numberToMinor(currency, (bid+ask)/2)
				return v, err == nil
			}
		}

		if last, ok := asFloat(row["last"]); ok {
			v, err := numberToMinor(currency, last)
			return v, err == nil
		}
	}

	return 0, false
}

// parseCoinmateTicker matches a Coinmate /tickerAll response against the
// requested base/quote. The pair name is the map key of each ticker row,
// not a field inside the row.
func parseCoinmateTicker(payload any, base string, currency Currency) (int64, bool) {
	m, ok := asMap(payload)
	if !ok {
		return 0, false
	}

	data, ok := asMap(m["data"])
	if !ok {
		return 0, false
	}

	for pair, rowv := range data {
		row, ok := rowv.(map[string]any)
		if !ok {
			continue
		}

		if !pairMatchesMarket(pair, base, currency.Code) {
			continue
		}

		v, err := valueFromAny(row["bid"], row["ask"], currency)
		if err == nil {
			return v, true
		}
	}

	return 0, false
}

func unsupportedPairError(chain string, token Token, currency Currency) error {
	if token != (Token{}) {
		return fmt.Errorf("%w: %s token %s/%s", ErrChainNotSupported, chain, token.Symbol, currency.Code)
	}
	return fmt.Errorf("%w: %s/%s", ErrChainNotSupported, chain, currency.Code)
}

func valueFromAny(bid any, ask any, currency Currency) (int64, error) {
	b, bok := asFloat(bid)
	a, aok := asFloat(ask)
	if !bok || !aok {
		return -1, ErrChainNotSupported
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
		return -1, ErrChainNotSupported
	}

	return currency.ToMinor(f), nil
}
