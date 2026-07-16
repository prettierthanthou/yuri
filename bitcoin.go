package yuri

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

const Bitcoin Chain = "bitcoin"
const Litecoin Chain = "litecoin"

var _ CryptoProvider = bitcoinLike{}
var _ PricingSymbolProvider = bitcoinLike{}

func NewBitcoin(rpcConf JsonRpcClientConfig) bitcoinLike {
	return bitcoinLike{
		jsonRpc: NewJsonRpcClient(rpcConf),
		chain:   Bitcoin,
	}
}

func NewLitecoin(rpcConf JsonRpcClientConfig) bitcoinLike {
	return bitcoinLike{
		jsonRpc: NewJsonRpcClient(rpcConf),
		chain:   Litecoin,
	}
}

// bitcoinLike is a generic implementation for anything
// which implements a bitcoin jsonrpc like RPC (ltc)
type bitcoinLike struct {
	jsonRpc JsonRpcClient
	chain   Chain
}

// Chain implements [CryptoProvider].
func (b bitcoinLike) Chain() Chain {
	return b.chain
}

// PriceSymbol implements [PricingSymbolProvider].
func (b bitcoinLike) PriceSymbol() string {
	switch b.chain {
	case Bitcoin:
		return "BTC"
	case Litecoin:
		return "LTC"
	default:
		return strings.ToUpper(string(b.chain))
	}
}

// CreateAddress implements [CryptoProvider].
func (b bitcoinLike) CreateAddress(ctx context.Context) (string, error) {
	var resp string
	if err := RPCDo(ctx, b.jsonRpc, JsonRpcRequest{
		Method: "getnewaddress",
		Params: map[string]any{
			"label": "yuri CreateAddress",
		},
	}, &resp); err != nil {
		return "", err
	}

	return resp, nil
}

// Decimals implements [CryptoProvider].
func (b bitcoinLike) Decimals() int64 {
	return 8
}

// Poll implements [CryptoProvider].
func (b bitcoinLike) Poll(ctx context.Context, invoices []Invoice) ([]Invoice, error) {
	addresses := make([]string, 0, len(invoices))
	invoiceByAddress := make(map[string]int, len(invoices))

	for i := range invoices {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		addresses = append(addresses, invoices[i].Address)
		invoiceByAddress[invoices[i].Address] = i
	}

	type listUnspentResult []struct {
		Address       string      `json:"address"`
		Amount        json.Number `json:"amount"`
		Confirmations uint64      `json:"confirmations"`
	}

	var result listUnspentResult
	err := RPCDo(ctx, b.jsonRpc, JsonRpcRequest{
		Method: "listunspent",
		Params: []any{
			0,
			9999999,
			addresses,
		},
	}, &result)
	if err != nil {
		return nil, err
	}

	type balance struct {
		total     *big.Int
		confirmed *big.Int
	}

	balances := make(map[string]*balance, len(invoices))

	for _, utxo := range result {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		r, ok := new(big.Rat).SetString(utxo.Amount.String())
		if !ok {
			return nil, fmt.Errorf("invalid bitcoin amount %q", utxo.Amount.String())
		}

		r.Mul(r, big.NewRat(100000000, 1))
		if !r.IsInt() {
			return nil, fmt.Errorf("bitcoin amount %q is not an integral number of satoshis", utxo.Amount.String())
		}

		sats := new(big.Int).Set(r.Num())

		bal := balances[utxo.Address]
		if bal == nil {
			bal = &balance{
				total:     new(big.Int),
				confirmed: new(big.Int),
			}
			balances[utxo.Address] = bal
		}

		bal.total.Add(bal.total, sats)

		if utxo.Confirmations > 0 {
			bal.confirmed.Add(bal.confirmed, sats)
		}
	}

	newInvoices := make([]Invoice, 0, len(invoices))

	for addr, idx := range invoiceByAddress {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		bal := balances[addr]
		if bal == nil {
			bal = &balance{
				total:     new(big.Int),
				confirmed: new(big.Int),
			}
		}

		updated := invoices[idx].Clone()
		updated.AmountPaid = new(big.Int).Set(bal.total)
		updated.Pending =
			updated.AmountPaid.Cmp(updated.AmountOwed) >= 0 &&
				bal.confirmed.Cmp(updated.AmountOwed) < 0

		if InvoicePollChanged(invoices[idx], updated) {
			newInvoices = append(newInvoices, updated)
		}
	}

	return newInvoices, nil
}
