package yuri

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"
)

func ExampleInstance() {
	ctx := context.Background()
	instance, err := New(Options{
		Hooks: Hooks{
			OnError: func(err error) {
				// if an error happened during Polling/Run itll be given here
				// instead of nuking ur app. kill the run Context if you want.
				// do you.
			},
			OnInvoiceUpdated: func(ctx context.Context, i Invoice) error {
				// an invoice updated

				if i.Paid() {
					// they paid, its confirmed. do thing.
					return nil
				}

				return nil
			},
		},
		Chains: []CryptoProvider{
			NewMonero(JsonRpcClientConfig{
				Host:     "",
				Username: "",
				Password: "",
				// Client: override or uses http.DefaultClient,
			}),
		},

		Pricing: []PriceProvider{},
		Storage: &InMemoryStorage{},

		PollEvery:       15 * time.Second,
		MaxPollDuration: 15 * time.Second,
	})

	if err != nil {
		// you fucked up an option
		return
	}

	go instance.Run(ctx)

	inv, err := instance.NewInvoice(ctx, InvoiceCreate{
		Chain:      Monero,
		AmountFiat: USD.Of(10.50),
	})
	if err != nil {
		// do something
		return
	}

	log.Printf("addr = %s owed = %s", inv.Address, inv.AmountOwed.String())

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func ExamplecachedPriceProvider() {
	_, err := New(Options{
		Pricing: []PriceProvider{
			// these are the same, by default a CachedPriceProvider lives in memory,
			// if you wish to coordinate/cache the pricing between restarts you can wrap
			// [PricingProvider] with Redis etc.
			NewCachedPriceProvider(NewCoinGeckoPriceProvider(nil)),
			NewCachedPriceProviderWithTTL(NewCoinGeckoPriceProvider(nil), 3*time.Minute),
		},
		Storage: &InMemoryStorage{},

		PollEvery:       15 * time.Second,
		MaxPollDuration: 15 * time.Second,
	})

	if err != nil {
		// you fucked up an option
		return
	}
}
