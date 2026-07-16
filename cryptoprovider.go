package yuri

import (
	"context"
	"crypto"
	"fmt"
	"strings"
)

// Chain is the full name of the chain in lowercase. (e.g. monero)
type Chain string

// CryptoProvider standardizes how cryptocurrency providers (e.g. monero)
// should handle core functionality.
//
// Every method implemented by a [CryptoProvider] must support being called
// concurrently.
type CryptoProvider interface {
	// Chain is the full name of the chain in lowercase. (e.g. monero)
	Chain() Chain

	// CreateAddress creates a new subaddress/derived address from the root address
	// provided at init time. Each provider should handle if they want to manually
	// derive it, or request it from something like a JSON-RPC server.
	CreateAddress(context.Context) (string, error)

	// Decimals gets the amount of decimals for the Native currency of this provider
	Decimals() int64

	// Poll checks the supplied invoice for a Chain against the blockchain,
	// and returns only the [Invoice]s which have updated.
	//
	// Providers MUST NEVER mutate the provided invoices. See [Invoice.Clone]
	// Providers should preserve all fields as required by [Invoice], and only
	// mutate the business state of the Invoice. (Balances, Pending)
	//
	// An empty slice should be returned if no invoices changed.
	//
	// Please see [Invoice]'s documentation for the expcted semantics of [Invoice.AmountPaid] and [Invoice.Pending]
	Poll(context.Context, []Invoice) ([]Invoice, error)

	// SupportsNFTs indicates if this specific [CryptoProvider] supports [NFT]
	SupportsNFTs() bool
}

// InvoicePollChanged is a small utility to determine if two Invoices are equal for polling.
func InvoicePollChanged(old, updated Invoice) bool {
	return old.Pending != updated.Pending ||
		old.AmountPaid.Cmp(updated.AmountPaid) != 0
}

// PricingSymbolProvider can be implemented by CryptoProvider implementations
// that need to expose a different market symbol than their Chain name.
// This is optional and exists so pricing providers can avoid hardcoded symbol maps.
type PricingSymbolProvider interface {
	PriceSymbol() string
}

// NftSymbol is used as a [Token.Symbol] to indicate
// that a specific [Token] is an NFT and should be
// handled differently than a typical Token.
const NftSymbol = "__YURI_NFT__"

type NftIdentifier struct {
	// Chain-specific collection identifier.
	// Ethereum: contract address
	// Solana: collection mint
	// Bitcoin Ordinals: inscription collection, etc.
	Collection string

	// Chain-specific asset identifier.
	// Ethereum ERC-721: token ID
	// Solana: mint address
	// Bitcoin Ordinals: inscription ID
	Asset string
}

func NftIdentifierFromString(contract string) (NftIdentifier, bool) {
	split := strings.Split(contract, "|||")
	if len(split) != 2 {
		return NftIdentifier{}, false
	}

	return NftIdentifier{
		Collection: split[0],
		Asset:      split[1],
	}, true
}

func (n NftIdentifier) String() string {
	return fmt.Sprintf("%s|||%s", n.Collection, n.Asset)
}

// NFT is a wrapper around [Invoice] and [Token] to indicate that
// a specified Token is in fact an NFT on a specific chain.
//
// [nftIdentifier] is either the smart contract, or other form
// of identifying an NFT on the wanted chain.
//
// NFTs are always the following: AmountOwed = 1, AmountPaid = 0.
// Where >=1 Paid means the NFT was recieved.
func NFT(chain Chain, nftIdentifier NftIdentifier, metadata map[string]any) InvoiceCreate {
	return InvoiceCreate{
		Chain: chain,
		Token: Token{
			Symbol:   NftSymbol,
			Contract: nftIdentifier.String(),
			Decimals: 1,
		},
		Metadata: metadata,
	}
}

// Token represents a smart contract on a respective chain
// for example, USDT erc20
type Token struct {
	Symbol   string `json:"symbol"`
	Contract string `json:"contract"`
	Decimals int64  `json:"decimals"`
}

// ProviderHooks allows the end user to deal with
// how they want to manage their custodial wallets
// for providers whos native JsonRPC/communication
// does not bundle a wallet.
type ProviderHooks struct {
	// OnNewAddress is called when NewAddress is called, you are expected
	// to manage your own storage solution for your wallets.
	// Returning an error from this method results in the address not being generated.
	OnNewAddress func(context.Context, crypto.PublicKey, crypto.PrivateKey) error
}
