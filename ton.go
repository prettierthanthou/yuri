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

type chainBlock struct {
	inner *ton.BlockIDExt
}

// chainClient exists to purely do mock tests
type chainClient interface {
	CreateAddress(ctx context.Context) (string, error)
	CurrentBlock(ctx context.Context) (*chainBlock, error)
	NativeBalance(ctx context.Context, block *chainBlock, addr string) (*big.Int, error)
	JettonBalance(ctx context.Context, block *chainBlock, addr string, contract string) (*big.Int, error)
}

type tonChainClient struct {
	api *ton.APIClient
}

func (c *tonChainClient) CreateAddress(ctx context.Context) (string, error) {
	seed := wallet.NewSeed()

	w, err := wallet.FromSeedWithOptions(c.api, seed, wallet.V5R1Final)
	if err != nil {
		return "", err
	}

	// TODO: persist seed/hooks

	return w.WalletAddress().String(), nil
}

func (c *tonChainClient) CurrentBlock(ctx context.Context) (*chainBlock, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, err
	}

	return &chainBlock{inner: b}, nil
}

func (c *tonChainClient) NativeBalance(
	ctx context.Context,
	block *chainBlock,
	addr string,
) (*big.Int, error) {
	address, err := address.ParseAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ton address (%s): %w", addr, err)
	}

	account, err := c.api.GetAccount(ctx, block.inner, address)
	if err != nil {
		return nil, fmt.Errorf("failed to get account (%s): %w", addr, err)
	}

	return new(big.Int).Set(account.State.Balance.Nano()), nil
}

func (c *tonChainClient) JettonBalance(
	ctx context.Context,
	block *chainBlock,
	addr string,
	contract string,
) (*big.Int, error) {
	ownerAddr, err := address.ParseAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ton address (%s): %w", addr, err)
	}

	masterAddr, err := address.ParseAddr(contract)
	if err != nil {
		return nil, fmt.Errorf("failed to parse contract (%s): %w", contract, err)
	}

	master := jetton.NewJettonMasterClient(c.api, masterAddr)
	wallet, err := master.GetJettonWalletAtBlock(ctx, ownerAddr, block.inner)
	if err != nil {
		return nil, fmt.Errorf("failed to get jetton wallet (%s): %w", addr, err)
	}

	balance, err := wallet.GetBalanceAtBlock(ctx, block.inner)
	if err != nil {
		return nil, fmt.Errorf("failed to get jetton balance (%s): %w", addr, err)
	}

	return new(big.Int).Set(balance), nil
}

type tonOptions struct {
	configUrl  string
	httpClient *http.Client
	client     chainClient
}

const tonMainnetPublic = "https://ton-blockchain.github.io/global.config.json"
const tonTestnetPublic = "https://ton-blockchain.github.io/testnet-global.config.json"

func TonWithMainnet() tonOptions                       { return tonOptions{configUrl: tonMainnetPublic} }
func TonWithTestnet() tonOptions                       { return tonOptions{configUrl: tonTestnetPublic} }
func TonWithHttpClient(client *http.Client) tonOptions { return tonOptions{httpClient: client} }
func TonWithApi(api *ton.APIClient) tonOptions         { return tonOptions{client: &tonChainClient{api: api}} }

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

	if o.client == nil {
		client := liteclient.NewConnectionPool()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.AddConnectionsFromConfigUrl(ctx, o.configUrl); err != nil {
			panic(fmt.Sprintf("Ton AddConnectionsFromConfigUrl failed: %+v", err))
		}

		api := ton.NewAPIClient(client)
		o.client = &tonChainClient{api: api}
	}

	return tonProvider{api: o.client}
}

type tonProvider struct {
	api chainClient
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
func (t tonProvider) CreateAddress(ctx context.Context) (string, error) {
	return t.api.CreateAddress(ctx)
}

// Poll implements [CryptoProvider].
func (t tonProvider) Poll(ctx context.Context, invoices []Invoice) ([]Invoice, error) {
	block, err := t.api.CurrentBlock(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Invoice, 0, len(invoices))

	for _, inv := range invoices {
		updated := inv.Clone()

		if updated.Token == (Token{}) {
			updated.AmountPaid, err = t.api.NativeBalance(ctx, block, updated.Address)
		} else {
			updated.AmountPaid, err = t.api.JettonBalance(
				ctx,
				block,
				updated.Address,
				updated.Token.Contract,
			)
		}
		if err != nil {
			return nil, err
		}

		updated.Pending = false

		if InvoicePollChanged(inv, updated) {
			out = append(out, updated)
		}
	}

	return out, nil
}
