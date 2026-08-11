package yuri_test

import (
	"context"
	"crypto"
	"fmt"
	"log"
	"math/big"
	"time"

	"codeberg.org/lewdest/yuri"
)

// Example walks through the full flow of the library, wires up a
// chain provider, a pricing provider and a storage provider, creates
// an invoice and showcases the payment lifecycle.
//
// the node is a mocked bitcoind (see fakeBitcoinNode)
// in a real situation you would point this towards your actual jsonrpc/bitcoind/etc
func Example() {
	node, pay := fakeBitcoinNode()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	instance, err := yuri.New(yuri.Options{
		Hooks: yuri.Hooks{
			OnError: func(err error) {
				log.Fatalf("yuri hit a problem: %+v", err)
			},
			OnInvoiceUpdated: func(ctx context.Context, inv yuri.Invoice) error {
				if inv.Paid() {
					fmt.Println("invoice settled:", inv.Address)
					cancel()
				}

				return nil
			},
		},
		Chains: []yuri.CryptoProvider{node},
		Pricing: []yuri.PriceProvider{
			// BTC at $100,000, in minor fiat units.
			yuri.NewStaticPriceProvider(10_000_000),
		},
		Storage:         &yuri.InMemoryStorage{},
		PollEvery:       100 * time.Millisecond,
		MaxPollDuration: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to create instance: %+v", err)
	}

	go instance.Run(ctx)

	inv, err := instance.NewInvoice(ctx, yuri.InvoiceCreate{
		Chain:      yuri.Bitcoin,
		AmountFiat: yuri.USD.Of(10.50),
	})
	if err != nil {
		log.Fatalf("failed to create invoice: %+v", err)
	}

	fmt.Printf("invoice created: addr = %s owed = %s\n", inv.Address, inv.AmountOwed.String())

	// the customer pays, the next poll notices and the hook above fires.
	pay()

	select {
	case <-ctx.Done():
	case <-time.After(15 * time.Second):
		log.Fatal("timed out waiting for the payment to be detected")
	}

	// Output:
	// invoice created: addr = bcrt1qfakeprovideraddressforinvoice owed = 10500
	// invoice settled: bcrt1qfakeprovideraddressforinvoice
}

// ExampleNew shows the validation yuri does on construction of an [yuri.Instance]
func ExampleNew() {
	_, err := yuri.New(yuri.Options{})
	fmt.Println("empty:", err)

	_, err = yuri.New(yuri.Options{
		Storage: &yuri.InMemoryStorage{},
		Pricing: []yuri.PriceProvider{yuri.NewStaticPriceProvider(100)},
	})
	fmt.Println("no chains:", err)

	// Output:
	// empty: storage cannot be nil
	// no chains: chains must have atleast 1 CryptoProvider
}

// ExampleNFT shows creating an NFT invoice with [yuri.NFT]. the solana
// provider generates the invoice wallet locally, so no node is needed
// (for the creation of an invoice! you will need a node for actual polling)
func ExampleNFT() {
	instance, err := yuri.New(yuri.Options{
		Chains: []yuri.CryptoProvider{
			yuri.NewSolana(yuri.SolanaOptions{
				Hooks: yuri.ProviderHooks{
					OnNewAddress: func(_ context.Context, pub crypto.PublicKey, _ crypto.PrivateKey) error {
						// you are in charge of storing these keys!
						// they are how the funds get out of the wallet.
						return nil
					},
				},
			}),
		},
		Pricing: []yuri.PriceProvider{
			yuri.NewStaticPriceProvider(1),
		},
		Storage: &yuri.InMemoryStorage{},
	})
	if err != nil {
		log.Fatalf("failed to create instance: %+v", err)
	}

	inv, err := instance.NewNFTInvoice(context.Background(), yuri.NFT(
		yuri.Solana,
		yuri.NftIdentifier{
			Collection: "the collection mint",
			Asset:      "the asset mint",
		},
		map[string]any{"order_id": "1234"},
	))
	if err != nil {
		log.Fatalf("failed to create NFT invoice: %+v", err)
	}

	fmt.Printf("owed = %s symbol = %s address length = %d\n",
		inv.AmountOwed, inv.Token.Symbol, len(inv.Address))

	// Output:
	// owed = 1 symbol = __YURI_NFT__ address length = 44
}

// Example_storage shows what bringing your own storage looks like.
// yuri does not care where invoices live, you are in charge of storing
// your invoices. View [yuri.Invoice] and [yuri.Storage] for what you're required to store.
func Example_storage() {
	store := &exampleMapStorage{}

	instance, err := yuri.New(yuri.Options{
		Chains: []yuri.CryptoProvider{
			// solana generates addresses locally, so we do not
			// need a node just to create an invoice.
			yuri.NewSolana(yuri.SolanaOptions{}),
		},
		Pricing: []yuri.PriceProvider{
			yuri.NewStaticPriceProvider(100_000_000),
		},
		Storage: store,
	})
	if err != nil {
		log.Fatalf("failed to create instance: %+v", err)
	}

	if _, err := instance.NewInvoice(context.Background(), yuri.InvoiceCreate{
		Chain:      yuri.Solana,
		AmountFiat: yuri.USD.Of(10),
	}); err != nil {
		log.Fatalf("failed to create invoice: %+v", err)
	}

	active, err := store.GetActiveInvoices(context.Background(), yuri.Solana)
	if err != nil {
		log.Fatalf("failed to read invoices from storage: %+v", err)
	}

	fmt.Println("active invoices:", len(active))

	// Output:
	// active invoices: 1
}

// ExampleCurrency_Of shows how fiat amounts are expressed. yuri
// deals in minor units, so USD has two decimals (cents) and JPY,
// which has no minor units, just rounds.
func ExampleCurrency_Of() {
	fmt.Println(yuri.USD.Of(10.50).Minor)
	fmt.Println(yuri.JPY.Of(12_345).Minor)

	// Output:
	// 1050
	// 12345
}

// ExampleInvoice_Paid shows what [yuri.Invoice.Paid] considers paid.
// funds that have arrived but are not yet confirmed are marked as pending
func ExampleInvoice_Paid() {
	inv := func(owed, paid int64, pending bool) yuri.Invoice {
		return yuri.Invoice{
			AmountOwed: big.NewInt(owed),
			AmountPaid: big.NewInt(paid),
			Pending:    pending,
		}
	}

	fmt.Println("short:", inv(100, 50, false).Paid())
	fmt.Println("exact:", inv(100, 100, false).Paid())
	fmt.Println("overpaid:", inv(100, 150, false).Paid())
	fmt.Println("pending:", inv(100, 100, true).Paid())

	// Output:
	// short: false
	// exact: true
	// overpaid: true
	// pending: false
}
