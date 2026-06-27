package yuri

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/lewdest/yuri/yuritest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TODO: WAHHHHHHHH PINNING MAKES IT FAIL I DONT CAREEEEE
// GET FUCKED BY THE SUPPLY CHAIN WAHHHHHHHHHHHHHHHHHHHHH
const anvilTestImage = "ghcr.io/foundry-rs/foundry:stable"

func ethereumHelperCreateEnv(t *testing.T) (JsonRpcClient, []string) {
	t.Helper()

	wd, _ := os.Getwd()
	dataDir := filepath.Join(wd, ".eth")

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
		Port: "8545",
		Mounts: []testcontainers.ContainerMount{
			{
				Source: testcontainers.GenericBindMountSource{HostPath: dataDir},
				Target: "/eth",
			},
		},
		Wait: wait.ForListeningPort("8545/tcp"),
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

	updated, err := provider.Poll(ctx, []Invoice{nativeInvoice, tokenInvoice})
	if err != nil {
		t.Fatalf("Poll pending: %v", err)
	}

	if got := updated[0].AmountPaid.String(); got != "20" {
		t.Fatalf("native AmountPaid = %s expected 20", got)
	}
	if updated[0].Pending {
		t.Fatalf("expected native invoice to remain unconfirmed before mining")
	}

	if got := updated[1].AmountPaid.String(); got != "100" {
		t.Fatalf("token AmountPaid = %s expected 100", got)
	}
	if updated[1].Pending {
		t.Fatalf("expected token invoice to be settled")
	}

	if _, err := rpc.Do(ctx, JsonRpcRequest{
		Method: "evm_mine",
	}); err != nil {
		t.Fatalf("evm_mine: %v", err)
	}

	updated, err = provider.Poll(ctx, updated)
	if err != nil {
		t.Fatalf("Poll mined: %v", err)
	}

	if got := updated[0].AmountPaid.String(); got != "25" {
		t.Fatalf("native AmountPaid = %s expected 25 after mining", got)
	}
	if !updated[0].Paid() {
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

func bigInt(t *testing.T, v int64) *big.Int {
	t.Helper()
	return big.NewInt(v)
}
