package yuri

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"codeberg.org/lewdest/yuri/internal/solana/base58"
	"codeberg.org/lewdest/yuri/yuritest"
	"github.com/testcontainers/testcontainers-go/wait"
)

const solanaImage = "solanalabs/solana:v1.18.26"

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

func TestSolanaChainAndDecimals(t *testing.T) {
	s := NewSolana(SolanaOptions{})

	if s.Chain() != Solana {
		t.Fatalf("expected Solana chain")
	}
	if s.Decimals() != 9 {
		t.Fatalf("expected 9 decimals")
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

	// TODO: refactoring to determine a better way.. the funds aren't confirmed
	// yet nor finalized. and i quite frankly hate the hacky shit above.
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
