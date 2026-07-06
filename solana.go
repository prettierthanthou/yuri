package yuri

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"log"
	"math/big"

	"codeberg.org/lewdest/yuri/internal/solana"
	"codeberg.org/lewdest/yuri/internal/solana/base58"
)

const Solana Chain = "solana"

type SolanaOptions struct {
	// Hooks directly manage storing the generated wallets, it is on you
	// to store your wallets in a safe place... atleast if you want to get
	// your funds out of the invoice wallets. heh..
	Hooks SolanaHooks
	Rpc   JsonRpcClientConfig
	// isTest is an internal flag to allow for `confirmed` to be used over `finalized`
	// due to the fact solana-test-validator/slopana is just.. shitty and i don't like
	// waiting for my tests.
	isTest bool
}

// TODO: maybe stop people from being dumb and assuming
// that they don't have to pass in the hooks. something like that.
// but until this is v1000.00.05 i don't care! RTFM!
func NewSolana(opts SolanaOptions) solanaProvider {
	return solanaProvider{
		jsonRpc: NewJsonRpcClient(opts.Rpc),
		hooks:   opts.Hooks,
		isTest:  opts.isTest,
	}
}

var _ CryptoProvider = solanaProvider{}
var _ PricingSymbolProvider = solanaProvider{}

// SolanaHooks allows the end user to deal with
// how they want to manage their custodial wallets
type SolanaHooks struct {
	// OnNewAddress is called when NewAddress is called, you are expected
	// to manage your own storage solution for your wallets.
	// Returning an error from this method results in the address not being generated.
	OnNewAddress func(context.Context, ed25519.PublicKey, ed25519.PrivateKey) error
}

type solanaProvider struct {
	jsonRpc JsonRpcClient
	hooks   SolanaHooks
	isTest  bool
}

func (s solanaProvider) Chain() Chain {
	return Solana
}

func (s solanaProvider) PriceSymbol() string {
	return "SOL"
}

func (s solanaProvider) Decimals() int64 {
	return 9
}

func (s solanaProvider) CreateAddress(ctx context.Context) (string, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", err
	}

	if s.hooks.OnNewAddress != nil {
		if err := s.hooks.OnNewAddress(ctx, pub, priv); err != nil {
			return "", err
		}
	}

	return base58.Encode([]byte(pub)), nil
}

func (s solanaProvider) Poll(ctx context.Context, invoices []Invoice) ([]Invoice, error) {
	type balanceBalance struct {
		pending *big.Int
		latest  *big.Int
	}

	balances := make(map[string]*balanceBalance, len(invoices))

	var (
		solAddrs []string
		solKeys  []string

		ataAddrs []string
		ataKeys  []string
	)

	for _, inv := range invoices {
		if inv.Token == (Token{}) {
			solAddrs = append(solAddrs, inv.Address)
			solKeys = append(solKeys, inv.Address)
			continue
		}

		ata, err := solana.AssociatedTokenAddress(inv.Address, inv.Token.Contract)
		if err != nil {
			return nil, err
		}

		ataAddrs = append(ataAddrs, ata)
		ataKeys = append(ataKeys, tokenBalanceKey(inv.Address, inv.Token))
	}

	// lamports
	if len(solAddrs) > 0 {
		latestStage := "finalized"
		if s.isTest {
			latestStage = "confirmed"
		}

		latest, err := s.rpcMultipleAccounts(ctx, solAddrs, latestStage)
		if err != nil {
			return nil, err
		}

		pending, err := s.rpcMultipleAccounts(ctx, solAddrs, "processed")
		if err != nil {
			return nil, err
		}

		for i := range solKeys {
			balances[solKeys[i]] = &balanceBalance{
				latest:  latest[i],
				pending: pending[i],
			}
		}
	}

	// ata/tokens
	if len(ataAddrs) > 0 {
		latest, err := s.rpcMultipleTokenAccounts(ctx, ataAddrs, "finalized")
		if err != nil {
			return nil, err
		}

		pending, err := s.rpcMultipleTokenAccounts(ctx, ataAddrs, "processed")
		if err != nil {
			return nil, err
		}

		for i := range ataKeys {
			balances[ataKeys[i]] = &balanceBalance{
				latest:  latest[i],
				pending: pending[i],
			}
		}
	}

	out := make([]Invoice, 0, len(invoices))
	for _, inv := range invoices {
		cp := inv.Clone()

		key := inv.Address
		if inv.Token != (Token{}) {
			key = tokenBalanceKey(inv.Address, inv.Token)
		}

		bal := balances[key]
		if bal == nil {
			bal = &balanceBalance{
				latest:  new(big.Int),
				pending: new(big.Int),
			}
		}

		cp.AmountPaid = new(big.Int).Set(bal.pending)

		cp.Pending =
			bal.latest.Cmp(cp.AmountOwed) < 0 &&
				bal.pending.Cmp(cp.AmountOwed) >= 0

		out = append(out, cp)
	}

	return out, nil
}

type multipleAccountsResp struct {
	Value []*struct {
		Lamports uint64 `json:"lamports"`
	} `json:"value"`
}

func (s solanaProvider) rpcMultipleAccounts(
	ctx context.Context,
	addrs []string,
	commitment string,
) ([]*big.Int, error) {

	var resp multipleAccountsResp

	err := RPCDo(ctx, s.jsonRpc, JsonRpcRequest{
		Method: "getMultipleAccounts",
		Params: []any{
			addrs,
			map[string]any{
				"encoding":   "base64",
				"commitment": commitment,
			},
		},
	}, &resp)
	if err != nil {
		return nil, err
	}

	out := make([]*big.Int, len(addrs))

	for i, acct := range resp.Value {
		if acct == nil {
			out[i] = new(big.Int)
			continue
		}

		out[i] = new(big.Int).SetUint64(acct.Lamports)
	}

	return out, nil
}

type multipleTokenResp struct {
	Value []*struct {
		Data []string `json:"data"`
	} `json:"value"`
}

func (s solanaProvider) rpcMultipleTokenAccounts(
	ctx context.Context,
	addrs []string,
	commitment string,
) ([]*big.Int, error) {
	var resp multipleTokenResp

	err := RPCDo(ctx, s.jsonRpc, JsonRpcRequest{
		Method: "getMultipleAccounts",
		Params: []any{
			addrs,
			map[string]any{
				"encoding":   "base64",
				"commitment": commitment,
			},
		},
	}, &resp)
	if err != nil {
		return nil, err
	}

	out := make([]*big.Int, len(addrs))

	for i, acct := range resp.Value {
		if acct == nil {
			out[i] = new(big.Int)
			continue
		}

		raw, err := base64.StdEncoding.DecodeString(acct.Data[0])
		if err != nil {
			return nil, err
		}

		if len(raw) < 72 {
			out[i] = new(big.Int)
			continue
		}

		amount := binary.LittleEndian.Uint64(raw[64:72])
		out[i] = new(big.Int).SetUint64(amount)
	}

	return out, nil
}
