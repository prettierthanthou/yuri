package yuri

import (
	"context"
	"fmt"
	"math/big"
	"strings"
)

const Ethereum Chain = "ethereum"

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
	Decimals: 8,
}

var _ CryptoProvider = ethereum{}
var _ PricingSymbolProvider = ethereum{}

func NewEthereum(rpcConf JsonRpcClientConfig) ethereum {
	return ethereum{jsonRpc: NewJsonRpcClient(rpcConf)}
}

type ethereum struct {
	jsonRpc JsonRpcClient
}

// Chain implements [CryptoProvider].
func (e ethereum) Chain() Chain {
	return Ethereum
}

// PriceSymbol implements [PricingSymbolProvider].
func (e ethereum) PriceSymbol() string {
	return "ETH"
}

// CreateAddress implements [CryptoProvider].
func (e ethereum) CreateAddress(ctx context.Context) (string, error) {
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
func (e ethereum) Decimals() int64 {
	return 18
}

// Poll implements [CryptoProvider].
func (e ethereum) Poll(ctx context.Context, invoices []Invoice) ([]Invoice, error) {
	type balanceBalance struct {
		total     *big.Int
		confirmed *big.Int
	}

	balances := make(map[string]*balanceBalance, len(invoices))

	for i := range invoices {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		inv := invoices[i]
		if inv.Token == (Token{}) {
			latest, pending, err := e.nativeBalance(ctx, inv.Address)
			if err != nil {
				return nil, err
			}

			balances[inv.Address] = &balanceBalance{
				total:     latest,
				confirmed: pending,
			}
			continue
		}

		latest, pending, err := e.erc20Balance(ctx, inv.Address, inv.Token)
		if err != nil {
			return nil, err
		}

		key := tokenBalanceKey(inv.Address, inv.Token)
		balances[key] = &balanceBalance{
			total:     latest,
			confirmed: pending,
		}
	}

	newInvoices := make([]Invoice, 0, len(invoices))
	for i := range invoices {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		inv := invoices[i].Clone()
		key := inv.Address
		if inv.Token != (Token{}) {
			key = tokenBalanceKey(inv.Address, inv.Token)
		}

		bal := balances[key]
		if bal == nil {
			bal = &balanceBalance{total: new(big.Int), confirmed: new(big.Int)}
		}

		inv.AmountPaid = new(big.Int).Set(bal.total)
		inv.Pending = inv.AmountPaid.Cmp(inv.AmountOwed) >= 0 && bal.confirmed.Cmp(inv.AmountOwed) < 0
		newInvoices = append(newInvoices, inv)
	}

	return newInvoices, nil
}

func tokenBalanceKey(addr string, token Token) string {
	return strings.ToLower(addr) + "|" + strings.ToLower(token.Contract)
}

func (e ethereum) nativeBalance(ctx context.Context, addr string) (*big.Int, *big.Int, error) {
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

func (e ethereum) erc20Balance(ctx context.Context, addr string, token Token) (*big.Int, *big.Int, error) {
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

func (e ethereum) rpcHexBalance(ctx context.Context, method, addr, tag string) (*big.Int, error) {
	var raw string
	if err := RPCDo(ctx, e.jsonRpc, JsonRpcRequest{
		Method: method,
		Params: []any{addr, tag},
	}, &raw); err != nil {
		return nil, err
	}

	return parseHexBigInt(raw)
}

func (e ethereum) rpcCallBalance(ctx context.Context, addr, contract, tag string) (*big.Int, error) {
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
