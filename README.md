<table>
  <tr>
    <td><h1>yuri</h1></td>
    <td><img src="assets/artwork.jpg" alt="yuri cover art" width="150"></td>
  </tr>
</table>

simple, sweet, and quick library/daemon for handling cryptocurrency payments in go

## what's supported

- [x] ethereum / evm l2s (base, arbitrum, polygon, avalanche, optimism, fantom...) erc20 + erc721
- [x] solana (+ tokens + spl nfts)
- [x] ton (+ nfts)
- [x] bitcoin
- [x] litecoin
- [x] dogecoin
- [x] monero

## quick example

```go
instance, err := yuri.New(yuri.Options{
    Chains: []yuri.CryptoProvider{
        yuri.NewMonero(yuri.JsonRpcClientConfig{Host: "http://localhost:18088/json_rpc"}),
    },
    Pricing: []yuri.PriceProvider{
        yuri.NewCoinGeckoPriceProvider(nil),
    },
    Storage: &yuri.InMemoryStorage{},
    Hooks: yuri.Hooks{
        OnError: func(_ error) {
            // do something
        },
        OnInvoiceUpdated: func(_ context.Context, invoice yuri.Invoice) error {
            if invoice.Paid() {
                // do something!
            }

            return nil
        },
    },
})

inv, _ := instance.NewInvoice(ctx, yuri.InvoiceCreate{
    Chain:      yuri.Monero,
    AmountFiat: yuri.USD.Of(10.50),
})
```

full examples in [example_test.go](./example_test.go) or `go doc`.

the daemon is in [./cmd/yurid](./cmd/yurid).

## notes

all wallets are created on the node/jsonrpc unless you provide a `ProviderHooks` option. use jsonrpcs you trust!


[agplv3](./LICENSE)
artwork by [eh_1315](https://x.com/EH_1315_) ([source](https://danbooru.donmai.us/posts/12195203))
repo on [codeberg](https://codeberg.org/lewdest/yuri) / [github](https://github.com/prettierthanthou/yuri)
