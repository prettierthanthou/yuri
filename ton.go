package yuri

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

const Ton Chain = "ton"

var (
	_ CryptoProvider        = tonProvider{}
	_ PricingSymbolProvider = tonProvider{}
)

type chainBlock struct {
	inner *ton.BlockIDExt
}

// chainClient exists to purely do mock tests
type chainClient interface {
	CreateAddress(ctx context.Context, hooks ProviderHooks) (string, error)
	CurrentBlock(ctx context.Context) (*chainBlock, error)
	NativeBalance(ctx context.Context, block *chainBlock, addr string) (*big.Int, error)
	JettonBalance(ctx context.Context, block *chainBlock, addr string, contract string) (*big.Int, error)
	NFTOwner(ctx context.Context, block *chainBlock, collection string, asset string) (string, error)
}

var _ chainClient = &tonChainClient{}

type tonChainClient struct {
	api *ton.APIClient
	// 0 means mainnet, see TonOptions.NetworkGlobalID
	networkGlobalID int32
}

func (c *tonChainClient) CreateAddress(ctx context.Context, hooks ProviderHooks) (string, error) {
	seed := wallet.NewSeed()

	globalID := c.networkGlobalID
	if globalID == 0 {
		globalID = wallet.MainnetGlobalID
	}

	w, err := wallet.FromSeedWithOptions(c.api, seed, wallet.ConfigV5R1Final{
		NetworkGlobalID: globalID,
	})
	if err != nil {
		return "", err
	}

	if hooks.OnNewAddress != nil {
		if err := hooks.OnNewAddress(ctx, w.PrivateKey().Public(), w.PrivateKey()); err != nil {
			return "", err
		}
	}

	return w.WalletAddress().String(), nil
}

func (c *tonChainClient) CurrentBlock(ctx context.Context) (*chainBlock, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, err
	}

	if b == nil {
		return nil, errors.New("no masterchain block returned by the ton api")
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

	if account == nil || account.State == nil {
		// the address has never been funded, so it has no state at all and
		// its balance is 0. dereferencing State here panics the poll cycle.
		return big.NewInt(0), nil
	}

	return account.State.Balance.Nano(), nil
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

	if balance == nil {
		// a jetton wallet which does not exist yet holds nothing
		return big.NewInt(0), nil
	}

	return new(big.Int).Set(balance), nil
}

func (c *tonChainClient) NFTOwner(
	ctx context.Context,
	block *chainBlock,
	collection string,
	asset string,
) (string, error) {
	collectionAddr, err := address.ParseAddr(collection)
	if err != nil {
		return "", err
	}

	index, ok := new(big.Int).SetString(asset, 10)
	if !ok {
		return "", fmt.Errorf("invalid TON NFT index %s", asset)
	}

	collectionClient := nft.NewCollectionClient(c.api, collectionAddr)

	itemAddr, err := collectionClient.GetNFTAddressByIndexAtBlock(
		ctx,
		index,
		block.inner,
	)
	if err != nil {
		return "", err
	}

	itemClient := nft.NewItemClient(c.api, itemAddr)

	data, err := itemClient.GetNFTDataAtBlock(
		ctx,
		block.inner,
	)
	if err != nil {
		return "", err
	}

	return data.OwnerAddress.String(), nil
}

type TonOptions struct {
	Client chainClient
	Hooks  ProviderHooks

	// NetworkGlobalID is the TON network id that wallets are derived for,
	// see [wallet.MainnetGlobalID] and [wallet.TestnetGlobalID].
	// 0 means mainnet, or testnet when the testnet config URL is used.
	// Only applies when Client is nil.
	NetworkGlobalID int32
}

const (
	TonMainnetPublic = "https://ton-blockchain.github.io/global.config.json"
	TonTestnetPublic = "https://ton-blockchain.github.io/testnet-global.config.json"
)

// MustTon creates a new [tonProvider]. If there is not a [chainClient] provided
// a [liteclient.ConnectionPool] with [TonMainnetPublic] connections is created.
//
// MustTon will panic if ConfigURL to add Connections from fails!
//
// This is equivilant to calling
//
//	MustTonWithConfigURL(opts, TonMainnetPublic)
func MustTon(opts TonOptions) tonProvider {
	return MustTonWithConfigURL(opts, TonMainnetPublic)
}

// MustTonWithConfigURL creates a new [tonProvider] and configures a [liteclient.ConnectionPool]
// to the provided [configUrl]
// MustTonWithConfigURL will panic if ConfigURL to add Connections from fails!
func MustTonWithConfigURL(opts TonOptions, configUrl string) tonProvider {
	p, err := NewTonWithConfigURL(opts, configUrl)
	if err != nil {
		panic(fmt.Sprintf("MustTonWithConfigUrl: %s", err.Error()))
	}

	return p
}

func NewTon(opts TonOptions) (tonProvider, error) {
	return NewTonWithConfigURL(opts, TonMainnetPublic)
}

func NewTonWithConfigURL(opts TonOptions, configUrl string) (tonProvider, error) {
	if opts.Client == nil {
		client := liteclient.NewConnectionPool()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.AddConnectionsFromConfigUrl(ctx, configUrl); err != nil {
			return tonProvider{}, fmt.Errorf("ton AddConnectionsFromConfigUrl failed: %w", err)
		}

		api := ton.NewAPIClient(client)

		// a wallet derived with the wrong network id cannot send on that
		// network, so follow the config URL unless told otherwise
		globalID := opts.NetworkGlobalID
		if globalID == 0 && configUrl == TonTestnetPublic {
			globalID = wallet.TestnetGlobalID
		}

		opts.Client = &tonChainClient{api: api, networkGlobalID: globalID}
	}

	if opts.Hooks.OnNewAddress == nil {
		return tonProvider{}, errors.New("ton requires hooks")
	}

	return tonProvider{api: opts.Client, hooks: opts.Hooks}, nil
}

type tonProvider struct {
	api   chainClient
	hooks ProviderHooks
}

// SupportsNFTs implements [CryptoProvider].
func (t tonProvider) SupportsNFTs() bool {
	return true
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
	return t.api.CreateAddress(ctx, t.hooks)
}

// Poll implements [CryptoProvider].
func (t tonProvider) Poll(ctx context.Context, invoices []Invoice) ([]Invoice, error) {
	block, err := t.api.CurrentBlock(ctx)
	if err != nil {
		return nil, err
	}

	if block == nil {
		return nil, errors.New("no ton block to poll against")
	}

	out := make([]Invoice, 0, len(invoices))
	var errs []error

	for _, inv := range invoices {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		updated := inv.Clone()

		switch {
		case updated.Token == (Token{}):
			updated.AmountPaid, err = t.api.NativeBalance(
				ctx,
				block,
				updated.Address,
			)

		case updated.Token.Symbol == NftSymbol:
			nft, ok := NftIdentifierFromString(updated.Token.Contract)

			if !ok {
				errs = append(errs, fmt.Errorf("invalid nft identifier %s", updated.Token.Contract))
				continue
			}

			var owner string
			owner, err = t.api.NFTOwner(
				ctx,
				block,
				nft.Collection,
				nft.Asset,
			)

			if err == nil && tonAddressesEqual(owner, updated.Address) {
				updated.AmountPaid = big.NewInt(1)
			} else {
				updated.AmountPaid = big.NewInt(0)
			}

		default:
			updated.AmountPaid, err = t.api.JettonBalance(
				ctx,
				block,
				updated.Address,
				updated.Token.Contract,
			)
		}

		if err != nil {
			errs = append(errs, fmt.Errorf("invoice %s: %w", inv.Address, err))
			continue
		}

		updated.Pending = false

		if InvoicePollChanged(inv, updated) {
			out = append(out, updated)
		}
	}

	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}

// tonAddressesEqual compares two TON addresses by their (workchain, hash)
// bytes, ignoring bounceable/non-bounceable and other textual variants.
func tonAddressesEqual(a, b string) bool {
	aa, err1 := address.ParseAddr(a)
	bb, err2 := address.ParseAddr(b)
	if err1 != nil || err2 != nil {
		// avoid false negatives from buggy tests by direct comparison
		return a == b
	}
	if aa.Workchain() != bb.Workchain() {
		return false
	}
	return string(aa.Data()) == string(bb.Data())
}
