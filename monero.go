package yuri

import (
	"context"
	"errors"
	"math/big"
)

var _ CryptoProvider = monero{}

const Monero Chain = "monero"

// TODO: maybe allow passing the account_index.
// for now, too bad!

// NewMonero creates a new CryptoProvider for Monero/XMR
//
// This base implementation always uses account index 0 (zero)
func NewMonero(rpcConf JsonRpcClientConfig) monero {
	return monero{jsonRpc: NewJsonRpcClient(rpcConf)}
}

type monero struct {
	jsonRpc JsonRpcClient
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

		resp, err := m.jsonRpc.Do(JsonRpcRequest{
			Method: "get_address_index",
			Params: map[string]any{
				"address": invoice.Address,
			},
		})
		if err != nil {
			return nil, err
		}

		indexRaw, ok := resp.Result["index"]
		if !ok {
			return nil, errors.New("jsonrpc did not return index on get_address_index request")
		}

		index, ok := indexRaw.(map[string]any)
		if !ok {
			return nil, errors.New("failed to cast index from get_address_index")
		}

		majorRaw, ok := index["major"]
		if !ok {
			return nil, errors.New("missing major from get_address_index")
		}

		minorRaw, ok := index["minor"]
		if !ok {
			return nil, errors.New("missing minor from get_address_index")
		}

		majorFloat, ok := majorRaw.(float64)
		if !ok {
			return nil, errors.New("failed to cast major from get_address_index")
		}

		minorFloat, ok := minorRaw.(float64)
		if !ok {
			return nil, errors.New("failed to cast minor from get_address_index")
		}

		major := uint64(majorFloat)
		minor := uint64(minorFloat)

		addressSpaces[major] = append(addressSpaces[major], minor)

		invoiceBySubaddr[subaddr{
			major: major,
			minor: minor,
		}] = i
	}

	newInvoices := make([]Invoice, 0, len(invoices))

	for major, minors := range addressSpaces {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := m.jsonRpc.Do(JsonRpcRequest{
			Method: "get_balance",
			Params: map[string]any{
				"account_index":   major,
				"address_indices": minors,
			},
		})
		if err != nil {
			return nil, err
		}

		perSubaddressRaw, ok := resp.Result["per_subaddress"]
		if !ok {
			return nil, errors.New("jsonrpc did not return per_subaddress on get_balance")
		}

		perSubaddress, ok := perSubaddressRaw.([]any)
		if !ok {
			return nil, errors.New("failed to cast per_subaddress from get_balance")
		}

		for _, raw := range perSubaddress {
			sub, ok := raw.(map[string]any)
			if !ok {
				continue
			}

			addressIndexRaw, ok := sub["address_index"]
			if !ok {
				continue
			}

			addressIndexFloat, ok := addressIndexRaw.(float64)
			if !ok {
				continue
			}

			idx, ok := invoiceBySubaddr[subaddr{
				major: major,
				minor: uint64(addressIndexFloat),
			}]
			if !ok {
				continue
			}

			updated := invoices[idx].Clone()

			balanceRaw, ok := sub["balance"]
			if !ok {
				continue
			}

			balanceFloat, ok := balanceRaw.(float64)
			if !ok {
				continue
			}

			unlockedRaw, ok := sub["unlocked_balance"]
			if !ok {
				continue
			}

			unlockedFloat, ok := unlockedRaw.(float64)
			if !ok {
				continue
			}

			balance := uint64(balanceFloat)
			unlocked := uint64(unlockedFloat)
			updated.AmountPaid = new(big.Int).SetUint64(balance)
			updated.Pending = (updated.AmountPaid.Cmp(updated.AmountOwed) >= 0) && !(new(big.Int).SetUint64(unlocked).Cmp(updated.AmountOwed) >= 0)

			newInvoices = append(newInvoices, updated)
		}
	}

	return newInvoices, nil
}

// CreateAddress implements [CryptoProvider].
func (m monero) CreateAddress() (string, error) {
	resp, err := m.jsonRpc.Do(JsonRpcRequest{
		Method: "create_address",
		Params: map[string]any{
			"account_index": 0,
			"label":         "yuri CreateAddress",
			"count":         1,
		},
	})

	if err != nil {
		return "", err
	}

	addr, ok := resp.Result["address"]
	if !ok {
		return "", errors.New("missing address in jsonrpc result")
	}

	return addr.(string), nil
}

// Chain implements [CryptoProvider].
func (m monero) Chain() Chain {
	return Monero
}

// Decimals implements [CryptoProvider].
func (m monero) Decimals() int64 {
	return 12
}
