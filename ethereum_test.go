package yuri

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codeberg.org/lewdest/yuri/yuritest"
	"github.com/testcontainers/testcontainers-go/wait"
)

const anvilTestImage = "ghcr.io/foundry-rs/foundry@sha256:043752653d5be351c71709091b3db97c4421c907eb40ea294195e7f532aadf46"

func ethereumHelperCreateEnv(t *testing.T) (JsonRpcClient, []string) {
	t.Helper()

	env := yuritest.New(t)
	cNode := env.Run(yuritest.Spec{
		Name:       t.Name() + "-anvil",
		Image:      anvilTestImage,
		Entrypoint: []string{"/usr/local/bin/anvil"},
		Cmd: []string{
			"--host", "0.0.0.0",
			"--port", "8545",
			"--silent",
		},
		Port:   "8545",
		Mounts: nil,
		Wait:   wait.ForListeningPort("8545/tcp"),
	})

	rpc := NewJsonRpcClient(JsonRpcClientConfig{
		Host: cNode.HTTP() + "/",
	})

	var accounts []string
	if err := RPCDo(context.Background(), rpc, JsonRpcRequest{
		Method: "eth_accounts",
	}, &accounts); err != nil {
		t.Fatalf("eth_accounts: %v", err)
	}

	if len(accounts) < 2 {
		t.Fatalf("expected at least 2 unlocked accounts from anvil")
	}

	return rpc, accounts
}

func TestEthereumChainAndDecimals(t *testing.T) {
	e := NewEthereum(JsonRpcClientConfig{})

	if e.Chain() != Ethereum {
		t.Fatalf("expected Ethereum chain")
	}

	if e.Decimals() != 18 {
		t.Fatalf("expected 18 decimals")
	}
}

func TestEthereumBNBChainAndSymbol(t *testing.T) {
	bnb := NewBNB(JsonRpcClientConfig{})

	if bnb.chain != BNB {
		t.Fatalf("BNB expected chain = %s got %s", BNB, bnb.chain)
	}

	if bnb.symbol != "BNB" {
		t.Fatalf("BNB expected chain = %s got %s", "BNB", bnb.chain)
	}
}

func TestEthereumLikeChainAndSymbol(t *testing.T) {
	const chain, symbol = "cool-chain", "chainsymbol"
	ethLike := NewEthereumLike(chain, symbol, JsonRpcClientConfig{})

	if ethLike.chain != chain {
		t.Fatalf("BNB expected chain = %s got %s", chain, ethLike.chain)
	}

	if ethLike.symbol != symbol {
		t.Fatalf("BNB expected chain = %s got %s", symbol, ethLike.symbol)
	}
}

func TestEthereumSupportsNFTs(t *testing.T) {
	eth := NewEthereum(JsonRpcClientConfig{})

	if !eth.SupportsNFTs() {
		t.Fatalf("Ethereum SupportsNFTs expected = true got = %+v", eth.SupportsNFTs())
	}
}

func TestEthereumCreateAddressAndPoll(t *testing.T) {
	rpc, accounts := ethereumHelperCreateEnv(t)

	provider := NewEthereum(JsonRpcClientConfig{
		Host: rpc.conf.Host,
	})

	ctx := context.Background()

	if _, err := provider.CreateAddress(ctx); err == nil {
		t.Fatalf("expected CreateAddress to fail on anvil")
	}

	merchantAddr := accounts[0]
	funderAddr := accounts[1]
	tokenContract := "0x4444444444444444444444444444444444444444"

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "anvil_setBalance",
		Params: []any{merchantAddr, "0x14"},
	}); err != nil {
		t.Fatalf("anvil_setBalance: %v", err)
	}

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "anvil_setCode",
		Params: []any{tokenContract, "0x606460005260206000f3"},
	}); err != nil {
		t.Fatalf("anvil_setCode: %v", err)
	}

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "anvil_setBalance",
		Params: []any{funderAddr, "0xde0b6b3a7640000"},
	}); err != nil {
		t.Fatalf("anvil_setBalance(funder): %v", err)
	}

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "evm_setAutomine",
		Params: []any{false},
	}); err != nil {
		t.Fatalf("evm_setAutomine(false): %v", err)
	}

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "eth_sendTransaction",
		Params: []any{
			map[string]any{
				"from":  funderAddr,
				"to":    merchantAddr,
				"value": "0x5",
			},
		},
	}); err != nil {
		t.Fatalf("eth_sendTransaction: %v", err)
	}

	nativeInvoice := Invoice{
		Chain:      Ethereum,
		Address:    merchantAddr,
		AmountOwed: bigInt(t, 25),
		AmountPaid: big.NewInt(0),
	}

	tokenInvoice := Invoice{
		Chain:      Ethereum,
		Address:    merchantAddr,
		AmountOwed: bigInt(t, 100),
		AmountPaid: big.NewInt(0),
		Token: Token{
			Symbol:   "MOCK",
			Contract: tokenContract,
			Decimals: 18,
		},
	}

	allInvoices := []Invoice{nativeInvoice, tokenInvoice}
	invoicesPoll1, err := provider.Poll(ctx, allInvoices)
	if err != nil {
		t.Fatalf("Poll pending: %v", err)
	}

	var native *Invoice
	var token *Invoice
	for _, inv := range invoicesPoll1 {
		if inv.Token != (Token{}) {
			token = &inv
			continue
		}

		native = &inv
	}

	if native == nil {
		t.Fatalf("poll(1) failed to find updated native")
	}

	if got := native.AmountPaid.String(); got != "25" {
		t.Fatalf("native AmountPaid = %s expected 25 (pending)", got)
	}

	if !native.Pending {
		t.Fatalf("expected native invoice to be pending before mining")
	}

	if token == nil {
		t.Fatalf("poll(1) failed to find token")
	}

	if got := token.AmountPaid.String(); got != "100" {
		t.Fatalf("token AmountPaid = %s expected 100", got)
	}
	if token.Pending {
		t.Fatalf("expected token invoice to be settled")
	}

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "evm_mine",
	}); err != nil {
		t.Fatalf("evm_mine: %v", err)
	}

	poll2Invoices, err := provider.Poll(ctx, allInvoices)
	if err != nil {
		t.Fatalf("Poll mined: %v", err)
	}

	for _, inv := range poll2Invoices {
		if inv.Token != (Token{}) {
			continue
		}

		native = &inv
	}

	if got := native.AmountPaid.String(); got != "25" {
		t.Fatalf("native AmountPaid = %s expected 25 after mining", got)
	}

	if native.Pending {
		t.Fatalf("expected native invoice to no longer be pending after mining")
	}

	if !native.Paid() {
		t.Fatalf("expected native invoice to be paid after mining")
	}

	if !native.Paid() {
		t.Fatalf("expected native invoice to be paid after mining")
	}
}

func TestEthereumERC20BalanceOfData(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{
			name: "checksummed-with-prefix",
			addr: "0xAbCdEf0123456789aBCDef0123456789aBCDef01",
			want: "0x70a08231000000000000000000000000abcdef0123456789abcdef0123456789abcdef01",
		},
		{
			name: "no-prefix",
			addr: "abcdef0123456789abcdef0123456789abcdef01",
			want: "0x70a08231000000000000000000000000abcdef0123456789abcdef0123456789abcdef01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := erc20BalanceOfData(tt.addr); got != tt.want {
				t.Fatalf("erc20BalanceOfData() = %q want %q", got, tt.want)
			}
		})
	}
}

func TestEthereumParseHexBigInt(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "zero", raw: "0x0", want: "0"},
		{name: "trimmed", raw: "  0x2a  ", want: "42"},
		{name: "uppercase", raw: "0X10", want: "16"},
		{name: "empty", raw: "0x", want: "0"},
		{name: "big", raw: "0xffffffffffffffffffffffffffffffff", want: "340282366920938463463374607431768211455"},
		{name: "invalid chars", raw: "0xzz", wantErr: true},
		{name: "negative", raw: "-0x1", wantErr: true},
		{name: "whitespace only", raw: "   ", want: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHexBigInt(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.raw)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseHexBigInt(%q): %v", tt.raw, err)
			}

			if got == nil {
				t.Fatalf("parseHexBigInt(%q) returned nil", tt.raw)
			}

			if got.String() != tt.want {
				t.Fatalf("parseHexBigInt(%q) = %s want %s", tt.raw, got.String(), tt.want)
			}
		})
	}
}

// ethereumFakeRpc serves eth_getBalance with per-address hex balances,
// returning a JSON-RPC error for any address in the fails set.
func ethereumFakeRpc(t *testing.T, balances map[string]string, fails map[string]bool) JsonRpcClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req struct {
			Id     string `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Method != "eth_getBalance" {
			http.Error(w, "unexpected method: "+req.Method, http.StatusBadRequest)
			return
		}

		addr, _ := req.Params[0].(string)

		w.Header().Set("Content-Type", "application/json")
		if fails[addr] {
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.Id,
				"error": map[string]any{
					"code":    -32000,
					"message": "connection refused",
				},
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.Id,
			"result":  balances[addr],
		})
	}))

	return NewJsonRpcClient(JsonRpcClientConfig{Host: srv.URL + "/"})
}

func TestEthereumPollIsolatesInvoiceErrors(t *testing.T) {
	goodAddr := "0x1111111111111111111111111111111111111111"
	badAddr := "0x2222222222222222222222222222222222222222"

	rpc := ethereumFakeRpc(t, map[string]string{
		goodAddr: "0x64",
	}, map[string]bool{
		badAddr: true,
	})

	provider := NewEthereum(JsonRpcClientConfig{
		Host: rpc.conf.Host,
	})

	invoices := []Invoice{
		{
			Chain:      Ethereum,
			Address:    goodAddr,
			AmountOwed: big.NewInt(100),
			AmountPaid: big.NewInt(0),
		},
		{
			Chain:      Ethereum,
			Address:    badAddr,
			AmountOwed: big.NewInt(100),
			AmountPaid: big.NewInt(0),
		},
	}

	changed, err := provider.Poll(context.Background(), invoices)
	if err == nil {
		t.Fatal("expected error for failing invoice")
	}

	if len(changed) != 1 {
		t.Fatalf("expected 1 changed invoice, got %d", len(changed))
	}

	if changed[0].Address != goodAddr {
		t.Fatalf("expected changed invoice for %s, got %s", goodAddr, changed[0].Address)
	}

	if changed[0].AmountPaid.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("expected paid = 100, got %v", changed[0].AmountPaid)
	}
}

func TestEthereumPollPendingBalance(t *testing.T) {
	rpc, accounts := ethereumHelperCreateEnv(t)
	provider := NewEthereum(JsonRpcClientConfig{
		Host: rpc.conf.Host,
	})
	ctx := context.Background()
	merchantAddr := accounts[0]
	funderAddr := accounts[1]

	// 20 wei confirmed
	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "anvil_setBalance",
		Params: []any{merchantAddr, "0x14"},
	}); err != nil {
		t.Fatalf("anvil_setBalance: %v", err)
	}

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "anvil_setBalance",
		Params: []any{funderAddr, "0xde0b6b3a7640000"},
	}); err != nil {
		t.Fatalf("anvil_setBalance(funder): %v", err)
	}

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "evm_setAutomine",
		Params: []any{false},
	}); err != nil {
		t.Fatalf("evm_setAutomine(false): %v", err)
	}

	// pending payment to reach 25
	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "eth_sendTransaction",
		Params: []any{
			map[string]any{
				"from":  funderAddr,
				"to":    merchantAddr,
				"value": "0x5",
			},
		},
	}); err != nil {
		t.Fatalf("eth_sendTransaction: %v", err)
	}
	invoice := Invoice{
		Chain:      Ethereum,
		Address:    merchantAddr,
		AmountOwed: big.NewInt(25),
		AmountPaid: big.NewInt(0),
	}

	allInvoices := []Invoice{invoice}
	got, err := provider.Poll(ctx, allInvoices)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if got[0].AmountPaid.Cmp(big.NewInt(25)) != 0 {
		t.Fatalf("AmountPaid = %v, want 25", got[0].AmountPaid)
	}

	if !got[0].Pending {
		t.Fatal("expected invoice to be pending")
	}

	if got[0].Paid() {
		t.Fatal("pending invoice must not be marked paid")
	}
}

func bigInt(t *testing.T, v int64) *big.Int {
	t.Helper()
	return big.NewInt(v)
}

func TestEthereumCreateAddressAndPollERC721(t *testing.T) {
	rpc, accounts := ethereumHelperCreateEnv(t)

	provider := NewEthereum(JsonRpcClientConfig{
		Host: rpc.conf.Host,
	})

	ctx := context.Background()

	merchantAddr := accounts[0]
	otherOwner := accounts[1]

	collection := "0x5555555555555555555555555555555555555555"

	// Minimal contract runtime implementing:
	//
	// ownerOf(uint256) -> address
	//
	// It ignores calldata and always returns the configured owner.
	makeOwnerOfCode := func(addr string) string {
		addr = strings.TrimPrefix(addr, "0x")

		// PUSH20 <addr>
		// PUSH1 00
		// MSTORE
		// PUSH1 20
		// PUSH1 00
		// RETURN
		return "0x" +
			"73" + addr +
			"60" + "00" +
			"52" +
			"60" + "20" +
			"60" + "00" +
			"f3"
	}

	// NFT is initially owned by someone else.
	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "anvil_setCode",
		Params: []any{
			collection,
			makeOwnerOfCode(otherOwner),
		},
	}); err != nil {
		t.Fatalf("anvil_setCode(initial owner): %v", err)
	}

	nftIdentifier := NftIdentifier{
		Collection: collection,
		Asset:      "1",
	}
	invoice := Invoice{
		Chain:      Ethereum,
		Address:    merchantAddr,
		AmountOwed: big.NewInt(1),
		AmountPaid: big.NewInt(0),
		Token:      nftIdentifier.Token(),
	}

	// before ownership transfer, merchant does not own NFT.
	updates, err := provider.Poll(ctx, []Invoice{invoice})
	if err != nil {
		t.Fatalf("Poll(before transfer): %v", err)
	}

	if len(updates) != 0 {
		t.Fatalf("expected no updates before NFT ownership transfer, got %d", len(updates))
	}

	// simulate transfer by changing ownerOf result.
	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "anvil_setCode",
		Params: []any{
			collection,
			makeOwnerOfCode(merchantAddr),
		},
	}); err != nil {
		t.Fatalf("anvil_setCode(new owner): %v", err)
	}

	updates, err = provider.Poll(ctx, []Invoice{invoice})
	if err != nil {
		t.Fatalf("Poll(after transfer): %v", err)
	}

	if len(updates) != 1 {
		t.Fatalf("expected one updated invoice, got %d", len(updates))
	}

	updated := updates[0]

	if got := updated.AmountPaid.String(); got != "1" {
		t.Fatalf("AmountPaid = %s expected 1", got)
	}

	if updated.Pending {
		t.Fatalf("expected NFT invoice not pending")
	}

	if !updated.Paid() {
		t.Fatalf("expected NFT invoice paid")
	}
}
