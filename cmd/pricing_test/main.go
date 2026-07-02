package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"codeberg.org/lewdest/yuri"
)

type caseItem struct {
	name  string
	p     yuri.PriceProvider
	c     yuri.Currency
	chain string
	token yuri.Token
}

func main() {
	cases := []caseItem{
		{name: "CoinGecko", p: yuri.NewCachedPriceProvider(yuri.NewCoinGeckoPriceProvider(nil)), c: yuri.USD, chain: "bitcoin"},
		{name: "CoinGecko Monero", p: yuri.NewCachedPriceProvider(yuri.NewCoinGeckoPriceProvider(nil)), c: yuri.USD, chain: string(yuri.Monero)},
		{name: "CoinGecko Ethereum", p: yuri.NewCachedPriceProvider(yuri.NewCoinGeckoPriceProvider(nil)), c: yuri.USD, chain: string(yuri.Ethereum)},
		{name: "CoinGecko Ethereum USDT", p: yuri.NewCachedPriceProvider(yuri.NewCoinGeckoPriceProvider(nil)), c: yuri.USD, chain: string(yuri.Ethereum), token: yuri.EthereumUSDT},
		{name: "CoinGecko Ethereum USDC", p: yuri.NewCachedPriceProvider(yuri.NewCoinGeckoPriceProvider(nil)), c: yuri.USD, chain: string(yuri.Ethereum), token: yuri.EthereumUSDC},
		{name: "CoinGecko Litecoin", p: yuri.NewCachedPriceProvider(yuri.NewCoinGeckoPriceProvider(nil)), c: yuri.USD, chain: string(yuri.Litecoin)},
		{name: "BtcTurk", p: yuri.NewBtcTurkPriceProvider(nil), c: yuri.Currency{Code: "TRY", Decimals: 2}, chain: "btc"},
		{name: "Bitbank Ethereum", p: yuri.NewBitbankPriceProvider(nil), c: yuri.Currency{Code: "JPY", Decimals: 0}, chain: string(yuri.Ethereum)},
		{name: "Bitbank", p: yuri.NewBitbankPriceProvider(nil), c: yuri.Currency{Code: "JPY", Decimals: 0}, chain: "btc"},
		{name: "Bitflyer Ethereum", p: yuri.NewBitflyerPriceProvider(nil), c: yuri.Currency{Code: "JPY", Decimals: 0}, chain: string(yuri.Ethereum)},
		{name: "Bitflyer", p: yuri.NewBitflyerPriceProvider(nil), c: yuri.Currency{Code: "JPY", Decimals: 0}, chain: "btc"},
		{name: "Bitmynt", p: yuri.NewBitmyntPriceProvider(nil), c: yuri.Currency{Code: "NOK", Decimals: 2}, chain: "btc"},
		{name: "Bitnob Litecoin", p: yuri.NewBitnobPriceProvider(nil), c: yuri.USD, chain: string(yuri.Litecoin)},
		{name: "Bitnob", p: yuri.NewBitnobPriceProvider(nil), c: yuri.USD, chain: "btc"},
		{name: "Bitpay", p: yuri.NewBitpayPriceProvider(nil), c: yuri.USD, chain: "btc"},
		{name: "Buda", p: yuri.NewBudaPriceProvider(nil), c: yuri.Currency{Code: "CLP", Decimals: 0}, chain: "btc"},
		{name: "Buda Ethereum", p: yuri.NewBudaPriceProvider(nil), c: yuri.Currency{Code: "CLP", Decimals: 0}, chain: string(yuri.Ethereum)},
		{name: "Bylls", p: yuri.NewByllsPriceProvider(nil), c: yuri.Currency{Code: "CAD", Decimals: 2}, chain: "btc"},
		{name: "Bylls Ethereum", p: yuri.NewByllsPriceProvider(nil), c: yuri.Currency{Code: "CAD", Decimals: 2}, chain: string(yuri.Ethereum)},
		{name: "CoinDCX", p: yuri.NewCoinDCXPriceProvider(nil), c: yuri.Currency{Code: "INR", Decimals: 2}, chain: "btc"},
		{name: "Coinmate", p: yuri.NewCoinmatePriceProvider(nil), c: yuri.EUR, chain: "btc"},
		{name: "CryptoMarket", p: yuri.NewCryptoMarketPriceProvider(nil), c: yuri.Currency{Code: "ARS", Decimals: 2}, chain: "btc"},
		{name: "Desiboard", p: yuri.NewDesiboardPriceProvider(nil), c: yuri.USD, chain: "btc"},
		{name: "FreeCurrencyRates", p: yuri.NewFreeCurrencyRatesPriceProvider(nil), c: yuri.USD, chain: "btc"},
		{name: "FreeCurrencyRates Monero", p: yuri.NewFreeCurrencyRatesPriceProvider(nil), c: yuri.USD, chain: string(yuri.Monero)},
		{name: "HitBTC", p: yuri.NewHitBTCPriceProvider(nil), c: yuri.USD, chain: "btc"},
		{name: "Kraken", p: yuri.NewKrakenPriceProvider(nil), c: yuri.USD, chain: "btc"},
		{name: "Luno", p: yuri.NewLunoPriceProvider(nil), c: yuri.USD, chain: "btc"},
		{name: "Ripio Ethereum", p: yuri.NewRipioPriceProvider(nil), c: yuri.Currency{Code: "ARS", Decimals: 2}, chain: string(yuri.Ethereum)},
		{name: "Ripio", p: yuri.NewRipioPriceProvider(nil), c: yuri.Currency{Code: "ARS", Decimals: 2}, chain: "btc"},
		{name: "Yadio Litecoin", p: yuri.NewYadioPriceProvider(nil), c: yuri.USD, chain: string(yuri.Litecoin)},
		{name: "Yadio", p: yuri.NewYadioPriceProvider(nil), c: yuri.USD, chain: "btc"},
	}

	var failed int
	for _, tc := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		price, err := tc.p.Get(ctx, tc.c, tc.chain, tc.token)
		cancel()

		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: error: %v\n", tc.name, err)
			failed++
			continue
		}

		fmt.Printf("%s: ok %d\n", tc.name, price)
		if len(tc.name) >= 9 && tc.name[:9] == "CoinGecko" {
			time.Sleep(2 * time.Second)
		}
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d provider checks failed\n", failed)
	}
}
