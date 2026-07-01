package yuri

import "context"

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

	// Poll gets called with the Invoices with a matching [Chain].
	// It is expected to return the updated values for each invoice.
	//
	// You are expected to return a new [Invoice] with updated balances,
	// or anything else of relevance. You are expected to copy over metadata
	// between the invoices.
	//
	// Please view [Invoice]'s comments if you are implementing this.
	Poll(context.Context, []Invoice) ([]Invoice, error)
}

// PricingSymbolProvider can be implemented by CryptoProvider implementations
// that need to expose a different market symbol than their Chain name.
// This is optional and exists so pricing providers can avoid hardcoded symbol maps.
type PricingSymbolProvider interface {
	PriceSymbol() string
}

// Token represents a smart contract on a respective chain
// for example, USDT erc20
type Token struct {
	Symbol   string
	Contract string
	Decimals int64
}
