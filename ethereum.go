package yuri

import (
	"context"
	"fmt"
	"math/big"
	"strings"
)

const Ethereum Chain = "ethereum"
const BNB Chain = "bnb"

// EthereumUSDT is USD(Tether) on Eth(eth) not Eth(base)
var EthereumUSDT Token = Token{
	Symbol:   "USDT",
	Contract: "0xdAC17F958D2ee523a2206206994597C13D831ec7",
	Decimals: 6,
}

// EthereumUSDC is USD(Circle) on Eth(eth) not Eth(base)
var EthereumUSDC Token = Token{
	Symbol:   "USDC",
	Contract: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
	Decimals: 6,
}

var _ CryptoProvider = ethereumLike{}
var _ PricingSymbolProvider = ethereumLike{}

// NewEthereum constructs a new ethereumLike CryptoProvider preconfigured for the standard Eth chain.
//
// All JsonRPCs for Ethereum must support `personal_newAccount` and you MUST trust
// the node/JsonRPC as the wallet is made on the node.
func NewEthereum(rpcConf JsonRpcClientConfig) ethereumLike {
	return ethereumLike{
		jsonRpc: NewJsonRpcClient(rpcConf),
		chain:   Ethereum,
		symbol:  "ETH",
	}
}

// NewBNB constructs a new ethereumLike CryptoProvider preconfigured for the BNB chain.
//
// All JsonRPCs for BNB must support `personal_newAccount` and you MUST trust
// the node/JsonRPC as the wallet is made on the node.
func NewBNB(rpcConf JsonRpcClientConfig) ethereumLike {
	return ethereumLike{
		jsonRpc: NewJsonRpcClient(rpcConf),
		chain:   BNB,
		symbol:  "BNB",
	}
}

// NewEthereumLike constructs a new ethereumLike CryptoProvider for generic EVM compatible
// chains. For example Eth(base), BNB, Eth(eth).
//
// EthereumLike is dependent on the Ethereum JSON RPC, any EVM compatible chain which
// still implements the ETH JsonRPC will work.
//
// All JsonRPCs for EthereumLike must support `personal_newAccount` and you MUST trust
// the node/JsonRPC as the wallet is made on the node.
//
// Chain is the name of the chain, this must be unique.
// Symbol is the pricing symbol, for example "ETH" for Eth(eth).
func NewEthereumLike(chain, symbol string, rpcConf JsonRpcClientConfig) ethereumLike {
	return ethereumLike{
		jsonRpc: NewJsonRpcClient(rpcConf),
		chain:   Chain(chain),
		symbol:  symbol,
	}
}

type ethereumLike struct {
	jsonRpc JsonRpcClient
	chain   Chain
	symbol  string
}

// SupportsNFTs implements [CryptoProvider].
func (e ethereumLike) SupportsNFTs() bool {
	return true
}

// Chain implements [CryptoProvider].
func (e ethereumLike) Chain() Chain {
	return e.chain
}

// PriceSymbol implements [PricingSymbolProvider].
func (e ethereumLike) PriceSymbol() string {
	return e.symbol
}

// CreateAddress implements [CryptoProvider].
func (e ethereumLike) CreateAddress(ctx context.Context) (string, error) {
	var resp string
	if err := RPCDo(ctx, e.jsonRpc, JsonRpcRequest{
		Method: "personal_newAccount",
		Params: []any{""},
	}, &resp); err != nil {
		return "", err
	}

	return resp, nil
}

// Decimals implements [CryptoProvider].
func (e ethereumLike) Decimals() int64 {
	return 18
}

// Poll implements [CryptoProvider].
func (e ethereumLike) Poll(ctx context.Context, invoices []Invoice) ([]Invoice, error) {
	type balanceBalance struct {
		// thank you jintana

		pending *big.Int
		// confirmed bal
		latest *big.Int
	}

	balances := make(map[string]*balanceBalance, len(invoices))

	for i := range invoices {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		inv := invoices[i]
		if inv.Token != (Token{}) && inv.Token.Symbol == NftSymbol {
			latest, pending, err := e.erc721Ownership(ctx, inv.Address, inv.Token)
			if err != nil {
				return nil, err
			}

			key := tokenBalanceKey(inv.Address, inv.Token)
			balances[key] = &balanceBalance{
				latest:  latest,
				pending: pending,
			}
			continue
		}

		if inv.Token == (Token{}) {
			latest, pending, err := e.nativeBalance(ctx, inv.Address)
			if err != nil {
				return nil, err
			}

			balances[inv.Address] = &balanceBalance{
				pending: pending,
				latest:  latest,
			}
			continue
		}

		latest, pending, err := e.erc20Balance(ctx, inv.Address, inv.Token)
		if err != nil {
			return nil, err
		}

		key := tokenBalanceKey(inv.Address, inv.Token)
		balances[key] = &balanceBalance{
			pending: pending,
			latest:  latest,
		}
	}

	newInvoices := make([]Invoice, 0, len(invoices))
	for i := range invoices {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		updatedInvoice := invoices[i].Clone()
		key := updatedInvoice.Address
		if updatedInvoice.Token != (Token{}) {
			key = tokenBalanceKey(updatedInvoice.Address, updatedInvoice.Token)
		}

		bal := balances[key]
		if bal == nil {
			bal = &balanceBalance{pending: new(big.Int), latest: new(big.Int)}
		}

		updatedInvoice.AmountPaid = new(big.Int).Set(bal.pending)
		updatedInvoice.Pending =
			bal.latest.Cmp(updatedInvoice.AmountOwed) < 0 &&
				bal.pending.Cmp(updatedInvoice.AmountOwed) >= 0

		if InvoicePollChanged(invoices[i], updatedInvoice) {
			newInvoices = append(newInvoices, updatedInvoice)
		}
	}

	return newInvoices, nil
}

func tokenBalanceKey(addr string, token Token) string {
	return strings.ToLower(addr) + "|" + strings.ToLower(token.Contract)
}

func (e ethereumLike) erc721Ownership(ctx context.Context, addr string, token Token) (*big.Int, *big.Int, error) {
	nftIdentifier, ok := NftIdentifierFromString(token.Contract)
	if !ok {
		return nil, nil, fmt.Errorf("failed to parse contract (%s) into NftIdentifier", token.Contract)
	}

	latest, err := e.rpcERC721Ownership(ctx, addr, nftIdentifier, "latest")
	if err != nil {
		return nil, nil, err
	}

	pending, err := e.rpcERC721Ownership(ctx, addr, nftIdentifier, "pending")
	if err != nil {
		return nil, nil, err
	}

	return latest, pending, nil
}

func (e ethereumLike) rpcERC721Ownership(ctx context.Context, addr string, nftIdentifer NftIdentifier, tag string) (*big.Int, error) {
	tokenID, ok := new(big.Int).SetString(nftIdentifer.Asset, 10)
	if !ok {
		return nil, fmt.Errorf("invalid token ID %q", nftIdentifer.Asset)
	}

	call := map[string]any{
		"to":   nftIdentifer.Collection,
		"data": erc721OwnerOfData(tokenID),
	}

	var raw string
	if err := RPCDo(ctx, e.jsonRpc, JsonRpcRequest{
		Method: "eth_call",
		Params: []any{call, tag},
	}, &raw); err != nil {
		return big.NewInt(0), fmt.Errorf("failed eth_call: err %+v", err)
	}

	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if len(raw) != 64 {
		return nil, fmt.Errorf("invalid ownerOf response %q", raw)
	}

	owner := "0x" + raw[24:]
	if strings.EqualFold(owner, addr) {
		return big.NewInt(1), nil
	}

	return big.NewInt(0), nil
}

func erc721OwnerOfData(tokenID *big.Int) string {
	id := fmt.Sprintf("%064x", tokenID)
	return "0x6352211e" + id
}

func (e ethereumLike) nativeBalance(ctx context.Context, addr string) (*big.Int, *big.Int, error) {
	latest, err := e.rpcHexBalance(ctx, "eth_getBalance", addr, "latest")
	if err != nil {
		return nil, nil, err
	}

	pending, err := e.rpcHexBalance(ctx, "eth_getBalance", addr, "pending")
	if err != nil {
		return nil, nil, err
	}

	return latest, pending, nil
}

func (e ethereumLike) erc20Balance(ctx context.Context, addr string, token Token) (*big.Int, *big.Int, error) {
	latest, err := e.rpcCallBalance(ctx, addr, token.Contract, "latest")
	if err != nil {
		return nil, nil, err
	}

	pending, err := e.rpcCallBalance(ctx, addr, token.Contract, "pending")
	if err != nil {
		return nil, nil, err
	}

	return latest, pending, nil
}

func (e ethereumLike) rpcHexBalance(ctx context.Context, method, addr, tag string) (*big.Int, error) {
	var raw string
	if err := RPCDo(ctx, e.jsonRpc, JsonRpcRequest{
		Method: method,
		Params: []any{addr, tag},
	}, &raw); err != nil {
		return nil, err
	}

	return parseHexBigInt(raw)
}

func (e ethereumLike) rpcCallBalance(ctx context.Context, addr, contract, tag string) (*big.Int, error) {
	call := map[string]any{
		"to":   contract,
		"data": erc20BalanceOfData(addr),
	}

	var raw string
	if err := RPCDo(ctx, e.jsonRpc, JsonRpcRequest{
		Method: "eth_call",
		Params: []any{call, tag},
	}, &raw); err != nil {
		return nil, err
	}

	return parseHexBigInt(raw)
}

// erc20BalanceOfData builds the ERC20 `balanceOf(address)` calldata payload.
//
// The selector is `0x70a08231` and the address is left-padded to 32 bytes as
// required by the ABI encoder. We normalize the address to lowercase and strip
// any leading `0x` prefix so callers can pass common Ethereum address formats
// without affecting the encoded calldata.
func erc20BalanceOfData(addr string) string {
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	return "0x70a08231" + strings.Repeat("0", 24) + addr
}

func parseHexBigInt(raw string) (*big.Int, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "0x")
	raw = strings.TrimPrefix(raw, "0X")
	if raw == "" {
		return new(big.Int), nil
	}

	v, ok := new(big.Int).SetString(raw, 16)
	if !ok {
		return nil, fmt.Errorf("invalid hex balance %q", raw)
	}

	return v, nil
}
