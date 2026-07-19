package yuri

import (
	"context"
	"math/big"
)

var _ CryptoProvider = monero{}
var _ PricingSymbolProvider = monero{}

const Monero Chain = "monero"

// TODO: maybe allow passing the account_index.
// for now, too bad!

// NewMonero creates a new CryptoProvider for Monero/XMR
//
// This base implementation always uses account index 0 (zero)
func NewMonero(rpcConf JsonRpcClientConfig) monero {
	return monero{jsonRpc: NewJsonRpcClient(JsonRpcClientConfig{
		Host:            rpcConf.Host,
		Username:        rpcConf.Username,
		Password:        rpcConf.Password,
		Client:          rpcConf.Client,
		NonB64BasicAuth: true,
	})}
}

type monero struct {
	jsonRpc JsonRpcClient
}

// SupportsNFTs implements [CryptoProvider].
func (m monero) SupportsNFTs() bool {
	return false
}

// Poll implements [CryptoProvider].
func (m monero) Poll(ctx context.Context, invoices []Invoice) ([]Invoice, error) {
	type subaddr struct {
		major uint64
		minor uint64
	}

	// account -> subaddress indexes
	addressSpaces := make(map[uint64][]uint64, len(invoices))

	// (major, minor) -> invoice index
	invoiceBySubaddr := make(map[subaddr]int, len(invoices))

	for i := range invoices {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		invoice := invoices[i]

		type getAddrIndexResp struct {
			Index struct {
				Major uint64 `json:"major"`
				Minor uint64 `json:"minor"`
			} `json:"index"`
		}

		var resp getAddrIndexResp
		err := RPCDo(ctx, m.jsonRpc, JsonRpcRequest{
			Method: "get_address_index",
			Params: map[string]any{
				"address": invoice.Address,
			},
		}, &resp)
		if err != nil {
			return nil, err
		}

		addressSpaces[resp.Index.Major] = append(addressSpaces[resp.Index.Major], resp.Index.Minor)
		invoiceBySubaddr[subaddr{
			major: resp.Index.Major,
			minor: resp.Index.Minor,
		}] = i
	}

	newInvoices := make([]Invoice, 0, len(invoices))

	for major, minors := range addressSpaces {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		type getBalanceResult struct {
			PerSubaddress []struct {
				AddressIndex    uint64 `json:"address_index"`
				Balance         uint64 `json:"balance"`
				UnlockedBalance uint64 `json:"unlocked_balance"`
			} `json:"per_subaddress"`
		}

		var balanceResult getBalanceResult
		err := RPCDo(ctx, m.jsonRpc, JsonRpcRequest{
			Method: "get_balance",
			Params: map[string]any{
				"account_index":   major,
				"address_indices": minors,
			},
		}, &balanceResult)
		if err != nil {
			return nil, err
		}

		for _, subaddress := range balanceResult.PerSubaddress {
			idx, ok := invoiceBySubaddr[subaddr{
				major: major,
				minor: uint64(subaddress.AddressIndex),
			}]
			if !ok {
				continue
			}

			updated := invoices[idx].Clone()
			updated.AmountPaid = new(big.Int).SetUint64(subaddress.Balance)
			updated.Pending = updated.AmountPaid.Cmp(updated.AmountOwed) >= 0 && new(big.Int).SetUint64(subaddress.UnlockedBalance).Cmp(updated.AmountOwed) < 0
			if InvoicePollChanged(invoices[idx], updated) {
				newInvoices = append(newInvoices, updated)
			}
		}
	}

	return newInvoices, nil
}

// CreateAddress implements [CryptoProvider].
func (m monero) CreateAddress(ctx context.Context) (string, error) {
	type createAddressResp struct {
		Address string `json:"address"`
	}

	var resp createAddressResp
	err := RPCDo(ctx, m.jsonRpc, JsonRpcRequest{
		Method: "create_address",
		Params: map[string]any{
			"account_index": 0,
			"label":         "yuri CreateAddress",
			"count":         1,
		},
	}, &resp)

	if err != nil {
		return "", err
	}

	return resp.Address, nil
}

// Chain implements [CryptoProvider].
func (m monero) Chain() Chain {
	return Monero
}

// PriceSymbol implements [PricingSymbolProvider].
func (m monero) PriceSymbol() string {
	return "XMR"
}

// Decimals implements [CryptoProvider].
func (m monero) Decimals() int64 {
	return 12
}
