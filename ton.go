package yuri

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

const Ton Chain = "ton"

var _ CryptoProvider = tonProvider{}
var _ PricingSymbolProvider = tonProvider{}

type tonOptions struct {
	configUrl  string
	httpClient *http.Client
	api        *ton.APIClient
}

const tonMainnetPublic = "https://ton-blockchain.github.io/global.config.json"
const tonTestnetPublic = "https://ton-blockchain.github.io/testnet-global.config.json"

func TonWithMainnet() tonOptions                       { return tonOptions{configUrl: tonMainnetPublic} }
func TonWithTestnet() tonOptions                       { return tonOptions{configUrl: tonTestnetPublic} }
func TonWithHttpClient(client *http.Client) tonOptions { return tonOptions{httpClient: client} }
func TonWithApi(api *ton.APIClient) tonOptions         { return tonOptions{api: api} }

func NewTon(opts ...tonOptions) tonProvider {
	o := &tonOptions{
		configUrl:  tonMainnetPublic,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		if opt.configUrl != "" {
			o.configUrl = opt.configUrl
		}

		if opt.httpClient != nil {
			o.httpClient = opt.httpClient
		}
	}

	if o.api == nil {
		client := liteclient.NewConnectionPool()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.AddConnectionsFromConfigUrl(ctx, o.configUrl); err != nil {
			panic(fmt.Sprintf("Ton AddConnectionsFromConfigUrl failed: %+v", err))
		}

		api := ton.NewAPIClient(client)
		o.api = api
	}

	return tonProvider{api: o.api}
}

type tonProvider struct {
	api *ton.APIClient
}

// PriceSymbol implements [PricingSymbolProvider].
func (t tonProvider) PriceSymbol() string {
	return "TON"
}

// Chain implements [CryptoProvider].
func (t tonProvider) Chain() Chain {
	return Ton
}

// Decimals implements [CryptoProvider].
func (t tonProvider) Decimals() int64 {
	return 9
}

// CreateAddress implements [CryptoProvider].
func (t tonProvider) CreateAddress(context.Context) (string, error) {
	seed := wallet.NewSeed()
	w, err := wallet.FromSeedWithOptions(t.api, seed, wallet.V5R1Final)
	if err != nil {
		return "", err
	}

	// TODO: hooks save

	return w.WalletAddress().String(), nil
}

// Poll implements [CryptoProvider].
func (t tonProvider) Poll(ctx context.Context, invoices []Invoice) ([]Invoice, error) {
	block, err := t.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Invoice, 0, len(invoices))
	for _, inv := range invoices {
		inv := inv.Clone()

		var amount *big.Int
		if inv.Token == (Token{}) {
			amount, err = t.nativeBalance(ctx, block, inv.Address)
		} else {
			amount, err = t.jettonBalance(ctx, block, inv.Address, inv.Token)
		}

		if err != nil {
			return nil, err
		}

		inv.AmountPaid = amount
		inv.Pending = false
		out = append(out, inv)
	}

	return out, nil
}

func (t tonProvider) nativeBalance(ctx context.Context, block *ton.BlockIDExt, addr string) (*big.Int, error) {
	address, err := address.ParseAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ton address (%s): %+v", addr, err)
	}

	account, err := t.api.GetAccount(ctx, block, address)
	if err != nil {
		return nil, fmt.Errorf("failed to get account (%s): %+v", addr, err)
	}

	return new(big.Int).Set(account.State.Balance.Nano()), nil
}

func (t tonProvider) jettonBalance(ctx context.Context, block *ton.BlockIDExt, addr string, token Token) (*big.Int, error) {
	ownerAddr, err := address.ParseAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ton jetton wallet address (%s): %+v", addr, err)
	}

	masterAddr, err := address.ParseAddr(token.Contract)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ton contract address (%s): %+v", token.Contract, err)
	}

	master := jetton.NewJettonMasterClient(t.api, masterAddr)
	wallet, err := master.GetJettonWalletAtBlock(ctx, ownerAddr, block)
	if err != nil {
		return nil, fmt.Errorf("failed to get jetton wallet (%s): %+v", addr, block)
	}

	balance, err := wallet.GetBalanceAtBlock(ctx, block)
	if err != nil {
		return nil, fmt.Errorf("failed to get jetton balance (%s): %+v", addr, err)
	}

	return new(big.Int).Set(balance), nil
}
