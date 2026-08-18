package yuri

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeberg.org/lewdest/yuri/internal/solana/base58"
	"codeberg.org/lewdest/yuri/yuritest"
	"github.com/testcontainers/testcontainers-go/wait"
)

// v1.18.26
const solanaImage = "solanalabs/solana@sha256:098806e64d44bccdbedbf07c2edabd1c850b92c8a4a0f81eba9c789034813db6"

func solanaHelperCreateEnv(t *testing.T) (JsonRpcClient, yuritest.Container) {
	t.Helper()

	env := yuritest.New(t)

	cNode := env.Run(yuritest.Spec{
		Name:       t.Name() + "-solana",
		Image:      solanaImage,
		Entrypoint: []string{"solana-test-validator"},
		Cmd: []string{
			"--reset",
			"--quiet",
			"--rpc-port", "8899",
			"--bind-address", "0.0.0.0",
			"--ledger", "/tmp/solana-ledger",
		},
		Port:   "8899",
		Mounts: nil,
		Wait:   wait.ForListeningPort("8899/tcp"),
	})

	return NewJsonRpcClient(JsonRpcClientConfig{
		Host: cNode.HTTP() + "/",
	}), *cNode
}

// solanaFakeRpc answers every getMultipleAccounts request with the given value.
func solanaFakeRpc(t *testing.T, value any) JsonRpcClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req struct {
			Id     string `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Method != "getMultipleAccounts" {
			http.Error(w, "unexpected method: "+req.Method, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.Id,
			"result": map[string]any{
				"context": map[string]any{"slot": 1},
				"value":   value,
			},
		})
	}))

	t.Cleanup(srv.Close)

	return NewJsonRpcClient(JsonRpcClientConfig{Host: srv.URL})
}

// missing accounts (null or short responses) are treated as zero balance.
func TestSolanaPollShortMultipleAccountsResponse(t *testing.T) {
	ctx := context.Background()

	const lamports = uint64(5_000_000_000)

	addresses := []string{
		"11111111111111111111111111111111",
		"22222222222222222222222222222222",
		"33333333333333333333333333333333",
	}

	invoices := make([]Invoice, len(addresses))
	for i, addr := range addresses {
		invoices[i] = Invoice{
			Chain:      Solana,
			Address:    addr,
			AmountOwed: big.NewInt(100),
			AmountPaid: big.NewInt(123_456),
		}
	}

	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "null value", value: nil},
		{name: "short value", value: []any{map[string]any{"lamports": lamports}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := NewSolana(SolanaOptions{
				isTest: true,
				Rpc:    solanaFakeRpc(t, tc.value).conf,
			})

			changed, err := provider.Poll(ctx, invoices)
			if err != nil {
				t.Fatalf("Poll: %v", err)
			}
			if len(changed) != len(invoices) {
				t.Fatalf("expected %d changed invoices, got %d", len(invoices), len(changed))
			}

			for i, inv := range changed {
				want := big.NewInt(0)
				if tc.value != nil && i == 0 {
					want = new(big.Int).SetUint64(lamports)
				}

				if inv.AmountPaid.Cmp(want) != 0 {
					t.Fatalf("invoice %d AmountPaid = %v, want %v", i, inv.AmountPaid, want)
				}
			}
		})
	}
}

// malformed or missing token accounts are treated as zero balance.
func TestSolanaPollMalformedTokenAccounts(t *testing.T) {
	ctx := context.Background()

	const mint = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"

	invoices := []Invoice{
		{
			Chain:   Solana,
			Address: base58.Encode(bytes.Repeat([]byte{0x11}, 32)),
			Token: Token{
				Symbol:   "TEST",
				Decimals: 9,
				Contract: mint,
			},
			AmountOwed: big.NewInt(100),
			AmountPaid: big.NewInt(123_456),
		},
		{
			Chain:   Solana,
			Address: base58.Encode(bytes.Repeat([]byte{0x22}, 32)),
			Token: Token{
				Symbol:   "TEST",
				Decimals: 9,
				Contract: mint,
			},
			AmountOwed: big.NewInt(100),
			AmountPaid: big.NewInt(123_456),
		},
	}

	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "empty data", value: []any{map[string]any{"data": []any{}}}},
		{name: "null data", value: []any{map[string]any{"data": nil}}},
		{name: "short value", value: []any{map[string]any{"data": []any{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := NewSolana(SolanaOptions{
				isTest: true,
				Rpc:    solanaFakeRpc(t, tc.value).conf,
			})

			changed, err := provider.Poll(ctx, invoices)
			if err != nil {
				t.Fatalf("Poll: %v", err)
			}
			if len(changed) != len(invoices) {
				t.Fatalf("expected %d changed invoices, got %d", len(invoices), len(changed))
			}

			for i, inv := range changed {
				if inv.AmountPaid.Sign() != 0 {
					t.Fatalf("invoice %d AmountPaid = %v, want 0", i, inv.AmountPaid)
				}
			}
		})
	}
}

func TestSolanaChainAndDecimals(t *testing.T) {
	s := NewSolana(SolanaOptions{})

	if s.Chain() != Solana {
		t.Fatalf("expected Solana chain")
	}
	if s.Decimals() != 9 {
		t.Fatalf("expected 9 decimals")
	}
}

func TestSolanaSupportsNFTs(t *testing.T) {
	s := NewSolana(SolanaOptions{})

	if !s.SupportsNFTs() {
		t.Fatalf("expected Solana SupportsNFTs to be truthy")
	}
}

func TestSolanaPriceSymbol(t *testing.T) {
	s := NewSolana(SolanaOptions{})

	if s.PriceSymbol() != "SOL" {
		t.Fatalf("Solana PriceSymbol expected = SOL got = %s", s.PriceSymbol())
	}
}

func pollUntil(t *testing.T, ctx context.Context, provider CryptoProvider, invoice Invoice, cond func(*Invoice) bool) Invoice {
	t.Helper()

	const pollIterations = 50
	const pollInterval = 100 * time.Millisecond

	var invs []Invoice
	var err error

	for range pollIterations {
		invs, err = provider.Poll(ctx, []Invoice{invoice})
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}

		if cond(&invs[0]) {
			return invs[0]
		}

		time.Sleep(pollInterval)
	}

	t.Fatalf("condition not met, last state: %+v", invs[0])
	return Invoice{}
}

func TestSolanaCreateAddressAndPoll(t *testing.T) {
	ctx := context.Background()
	rpc, _ := solanaHelperCreateEnv(t)

	var hookPub string

	provider := NewSolana(SolanaOptions{
		isTest: true,
		Rpc:    rpc.conf,
		Hooks: ProviderHooks{
			OnNewAddress: func(_ context.Context, pub crypto.PublicKey, priv crypto.PrivateKey) error {
				hookPub = base58.Encode([]byte(pub.(ed25519.PublicKey)))

				if len(priv.(ed25519.PrivateKey)) == 0 {
					t.Fatal("expected private key to be populated")
				}
				return nil
			},
		},
	})

	addr, err := provider.CreateAddress(ctx)
	if err != nil {
		t.Fatalf("CreateAddress: %v", err)
	}
	if addr == "" {
		t.Fatal("expected non-empty address")
	}
	if hookPub != addr {
		t.Fatalf("hook pub %q != returned addr %q", hookPub, addr)
	}

	invoice := Invoice{
		Chain:      Solana,
		Address:    addr,
		AmountOwed: big.NewInt(2_500_000_000),
		AmountPaid: big.NewInt(0),
	}

	poll1Invoices, err := provider.Poll(ctx, []Invoice{invoice})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(poll1Invoices) != 0 {
		t.Fatalf("Poll(1) should return 0 invoices recieved: %+v", err)
	}

	const airdrop1 = int64(1_500_000_000)
	const airdrop2 = int64(1_000_000_000)

	// first airdrop
	var sig string
	if err := RPCDo(ctx, rpc, JsonRpcRequest{
		Method: "requestAirdrop",
		Params: []any{addr, airdrop1, map[string]any{"commitment": "finalized"}},
	}, &sig); err != nil {
		t.Fatalf("requestAirdrop: %v", err)
	}

	if err := solanaWaitForFinalizedSignature(ctx, rpc, sig); err != nil {
		t.Fatalf("airdrop not finalized: %v", err)
	}

	partial := pollUntil(t, ctx, provider, invoice, func(inv *Invoice) bool {
		return inv.AmountPaid.Cmp(big.NewInt(airdrop1)) == 0
	})

	if partial.Paid() {
		t.Fatal("invoice should not be fully paid yet")
	}

	if partial.AmountPaid.Cmp(big.NewInt(airdrop1)) < 0 {
		t.Fatalf("expected at least %d, got %s", airdrop1, partial.AmountPaid)
	}

	// second airdrop
	var sig2 string
	if err := RPCDo(ctx, rpc, JsonRpcRequest{
		Method: "requestAirdrop",
		Params: []any{addr, airdrop2, map[string]any{"commitment": "finalized"}},
	}, &sig2); err != nil {
		t.Fatalf("second airdrop: %v", err)
	}

	if err := solanaWaitForFinalizedSignature(ctx, rpc, sig2); err != nil {
		t.Fatalf("airdrop2 not finalized: %v", err)
	}

	final := pollUntil(t, ctx, provider, invoice, func(inv *Invoice) bool {
		return inv.AmountPaid.Cmp(big.NewInt(2_500_000_000)) >= 0
	})

	if !final.Paid() {
		t.Fatal("expected invoice to be fully paid")
	}
}

func solanaWaitForFinalizedSignature(ctx context.Context, rpc JsonRpcClient, sig string) error {
	for range 100 {
		var res struct {
			Value []struct {
				ConfirmationStatus string `json:"confirmationStatus"`
			} `json:"value"`
		}

		if err := RPCDo(ctx, rpc, JsonRpcRequest{
			Method: "getSignatureStatuses",
			Params: []any{
				[]string{sig},
				map[string]any{"searchTransactionHistory": true},
			},
		}, &res); err != nil {
			return err
		}

		if len(res.Value) > 0 {
			switch res.Value[0].ConfirmationStatus {
			case "finalized", "confirmed":
				return nil
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("signature not finalized: %s", sig)
}

func TestSolanaTokenPoll(t *testing.T) {
	ctx := context.Background()

	sol, container := solanaHelperCreateEnv(t)

	out, err := container.Exec(ctx, []string{
		"bash", "-lc",
		fmt.Sprintf(`
set -e

export HOME=/tmp/solana
mkdir -p "$HOME"

KEYPAIR=$HOME/payer.json

solana config set --url %s

solana-keygen new --no-bip39-passphrase -o "$KEYPAIR" --force
solana config set --keypair "$KEYPAIR"

solana airdrop 100

TOKEN_MINT=$(spl-token create-token | awk '/Creating token/{print $3}')
SENDER_ATA=$(spl-token create-account "$TOKEN_MINT" | awk '/Creating account/{print $3}')

spl-token mint "$TOKEN_MINT" 1000 "$SENDER_ATA"

echo ""
echo "TOKEN_MINT=$TOKEN_MINT"
echo "SENDER_ATA=$SENDER_ATA"
`, container.UnmappedHTTP()),
	})
	if err != nil {
		t.Fatal(err)
	}

	var (
		mint      string
		senderATA string
	)

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "TOKEN_MINT="):
			mint = strings.TrimPrefix(line, "TOKEN_MINT=")
		case strings.HasPrefix(line, "SENDER_ATA="):
			senderATA = strings.TrimPrefix(line, "SENDER_ATA=")
		}
	}

	if mint == "" || senderATA == "" {
		t.Fatalf("bad setup:\n%s", out)
	}

	chain := NewSolana(SolanaOptions{
		isTest: true,
		Rpc:    sol.conf,
	})

	invoiceAddr, err := chain.CreateAddress(ctx)
	if err != nil {
		t.Fatal(err)
	}

	inv := Invoice{
		Chain:   Solana,
		Address: invoiceAddr,
		Token: Token{
			Symbol:   "TEST",
			Decimals: 9,
			Contract: mint,
		},
		AmountOwed: big.NewInt(100_000_000_000),
		AmountPaid: big.NewInt(0),
	}

	allInvoices := []Invoice{inv}
	invoicesPoll1, err := chain.Poll(ctx, allInvoices)
	if err != nil {
		t.Fatal(err)
	}

	if len(invoicesPoll1) != 0 {
		t.Fatalf("Poll(1) should return 0 invoices recieved: %+v", err)
	}

	_, err = container.Exec(ctx, []string{
		"bash", "-lc",
		fmt.Sprintf(`
set -e

export HOME=/tmp/solana
KEYPAIR=$HOME/payer.json

solana config set --url %s
solana config set --keypair "$KEYPAIR"

spl-token transfer \
    %s \
    100 \
    %s \
    --fund-recipient \
    --allow-unfunded-recipient
`, container.UnmappedHTTP(), mint, invoiceAddr),
	})
	if err != nil {
		t.Fatal(err)
	}

	invoicesPoll2, err := chain.Poll(ctx, allInvoices)
	if err != nil {
		t.Fatal(err)
	}

	if !invoicesPoll2[0].Paid() {
		t.Fatalf("invoice should be paid. inv %+v", invoicesPoll2[0])
	}

	if invoicesPoll2[0].AmountPaid.Cmp(big.NewInt(100_000_000_000)) != 0 {
		t.Fatalf("AmountPaid=%v", invoicesPoll2[0].AmountPaid)
	}
}

func TestSolanaNFTPoll(t *testing.T) {
	ctx := context.Background()

	sol, container := solanaHelperCreateEnv(t)

	out, err := container.Exec(ctx, []string{
		"bash", "-lc",
		fmt.Sprintf(`
set -e

export HOME=/tmp/solana
mkdir -p "$HOME"

KEYPAIR=$HOME/payer.json

solana config set --url %s

solana-keygen new --no-bip39-passphrase -o "$KEYPAIR" --force
solana config set --keypair "$KEYPAIR"

solana airdrop 100

NFT_MINT=$(spl-token create-token --decimals 0 | awk '/Creating token/{print $3}')
NFT_ATA=$(spl-token create-account "$NFT_MINT" | awk '/Creating account/{print $3}')

spl-token mint "$NFT_MINT" 1 "$NFT_ATA"

echo "NFT_MINT=$NFT_MINT"
`, container.UnmappedHTTP()),
	})
	if err != nil {
		t.Fatal(err)
	}

	var nftMint string

	for _, line := range strings.Split(out, "\n") {
		if idx := strings.Index(line, "NFT_MINT="); idx >= 0 {
			nftMint = strings.TrimSpace(line[idx+len("NFT_MINT="):])
		}
	}

	if nftMint == "" {
		t.Fatalf("failed to create NFT:\n%s", out)
	}

	chain := NewSolana(SolanaOptions{
		isTest: true,
		Rpc:    sol.conf,
	})

	addr, err := chain.CreateAddress(ctx)
	if err != nil {
		t.Fatal(err)
	}

	invoice := Invoice{
		Chain:   Solana,
		Address: addr,
		Token: NftIdentifier{
			Collection: "ignored-for-now",
			Asset:      nftMint,
		}.Token(),
		AmountOwed: big.NewInt(1),
		AmountPaid: big.NewInt(0),
	}

	initial, err := chain.Poll(ctx, []Invoice{invoice})
	if err != nil {
		t.Fatal(err)
	}

	if len(initial) != 0 {
		t.Fatalf("Poll(1) should return no changed invoices")
	}

	_, err = container.Exec(ctx, []string{
		"bash", "-lc",
		fmt.Sprintf(`
set -e

export HOME=/tmp/solana
KEYPAIR=$HOME/payer.json

solana config set --url %s
solana config set --keypair "$KEYPAIR"

spl-token transfer \
    %s \
    1 \
    %s \
    --fund-recipient \
    --allow-unfunded-recipient
`, container.UnmappedHTTP(), nftMint, addr),
	})
	if err != nil {
		t.Fatal(err)
	}

	changed := pollUntil(t, ctx, chain, invoice, func(inv *Invoice) bool {
		return inv.AmountPaid.Cmp(big.NewInt(1)) == 0
	})

	if !changed.Paid() {
		t.Fatal("expected NFT invoice to be paid")
	}

	if changed.AmountPaid.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("AmountPaid=%v", changed.AmountPaid)
	}
}
