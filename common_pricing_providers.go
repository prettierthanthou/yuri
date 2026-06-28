package yuri

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
)

// common pricing providers is a set of relatively common pricing
// providers, which i decided to implement because they were simple.

var _ PriceProvider = coinGecko{}

var coinGeckoCurrencies []string = []string{
	"btc", "eth", "ltc", "bch", "bnb", "eos", "xrp", "xlm", "link", "dot", "yfi", "sol", "usd", "aed", "ars",
	"aud", "bdt", "bhd", "bmd", "brl", "cad", "chf",
	"clp", "cny", "czk", "dkk", "eur", "gbp", "gel", "hkd", "huf", "idr", "ils", "inr", "jpy", "krw",
	"kwd", "lkr", "mmk", "mxn", "myr", "ngn", "nok", "nzd", "php", "pkr", "pln", "rub", "sar", "sek",
	"sgd", "thb", "try", "twd", "uah", "vef", "vnd", "zar", "xdr", "xag", "xau", "bits", "sats",
}

type coinGecko struct {
	client *http.Client
}

func (c coinGecko) getToken(ctx context.Context, currency Currency, chain string, token Token) (fiatMinorUnits int64, err error) {
	const tokenUrl = "https://api.coingecko.com/api/v3/simple/token_price/{CHAIN}?contract_addresses={ADDR}&vs_currencies={CURRENCY}"
	url := strings.Replace(tokenUrl, "{CHAIN}", chain, 1)
	url = strings.Replace(url, "{ADDR}", token.Contract, 1)
	url = strings.Replace(url, "{CURRENCY}", strings.ToLower(currency.Code), 1)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return -1, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, err
	}

	if resp.StatusCode == 404 {
		return -1, ChainNotSupportedErr
	}

	if resp.StatusCode == 429 {
		return -1, errors.New("coingecko ratelimited")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	var parsedBody map[string]map[string]float64
	err = json.Unmarshal(body, &parsedBody)
	if err != nil {
		return -1, err
	}

	val, ok := parsedBody[token.Contract]
	if !ok {
		return -1, errors.New("invalid response from coingecko for contract")
	}

	price, ok := val[strings.ToLower(currency.Code)]
	if !ok {
		return -1, errors.New("missing price from contract for contract")
	}

	return currency.ToMinor(price), nil
}

// Get implements [PriceProvider].
func (c coinGecko) Get(ctx context.Context, currency Currency, chain string, token Token) (fiatMinorUnits int64, err error) {
	if !slices.Contains(coinGeckoCurrencies, strings.ToLower(currency.Code)) {
		return -1, FiatCurrencyNotSupportedErr
	}

	if token == (Token{}) {
		return c.getToken(ctx, currency, chain, token)
	}

	const nameUrl = "https://api.coingecko.com/api/v3/simple/price?vs_currencies={CURRENCY}&names={CHAIN}"
	url := strings.Replace(nameUrl, "{CURRENCY}", strings.ToLower(currency.Code), 1)
	url = strings.Replace(nameUrl, "{CHAIN}", chain, 1)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return -1, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return -1, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	if resp.StatusCode == 404 {
		return -1, ChainNotSupportedErr
	}

	if resp.StatusCode == 429 {
		return -1, errors.New("coingecko ratelimited")
	}

	var parsedBody map[string]map[string]float64
	err = json.Unmarshal(body, &parsedBody)
	if err != nil {
		return -1, err
	}

	// NOTE: this will break for unicode characters... but who's chain isn't ASCII?
	val, ok := parsedBody[strings.ToUpper(chain[:1])+strings.ToLower(chain[1:])]
	if !ok {
		return -1, errors.New("invalid response from coingecko for contract")
	}

	price, ok := val[strings.ToLower(currency.Code)]
	if !ok {
		return -1, errors.New("missing price from contract for contract")
	}

	return currency.ToMinor(price), nil
}
