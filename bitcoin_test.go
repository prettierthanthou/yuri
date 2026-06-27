package yuri

import (
	"context"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/lewdest/yuri/yuritest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const bitcoinTestImage = "bitcoin/bitcoin:31.0"

func bitcoinHelperCreateEnv(t *testing.T) JsonRpcClient {
	t.Helper()

	wd, _ := os.Getwd()
	bitcoinDir := path.Join(wd, ".bitcoin")
	dataDir, _ := filepath.Abs(path.Join(bitcoinDir, "data"))

	const rpcPassword = "qDDZdeQ5vw9XXFeVnXT4PZ--tGN2xNjjR4nrtyszZx0="
	const rpcAuth = "foo:7d9ba5ae63c3d4dc30583ff4fe65a67e$9e3634e81c11659e3de036d0bf88f89cd169c1039e6e09607562d54765c649cc"

	env := yuritest.New(t)
	cNode := env.Run(yuritest.Spec{
		Name:       t.Name() + "-bitcoind",
		Image:      bitcoinTestImage,
		Entrypoint: []string{"bitcoind"},
		Cmd: []string{
			"-regtest=1",
			"-printtoconsole",
			"-rpcport=18443",
			"-rpcbind=0.0.0.0",
			"-rpcallowip=0.0.0.0/0",
			"-rpcauth=" + rpcAuth,
			"-fallbackfee=0.0002",
			"-datadir=/bitcoin",
		},
		Port: "18443",
		Mounts: []testcontainers.ContainerMount{
			{
				Source: testcontainers.GenericBindMountSource{HostPath: dataDir},
				Target: "/bitcoin",
			},
		},
		Wait: wait.ForListeningPort("18443/tcp"),
	})

	rpc := NewJsonRpcClient(JsonRpcClientConfig{
		Host:     cNode.HTTP() + "/",
		Username: "foo",
		Password: rpcPassword,
	})

	if _, err := rpc.Do(context.Background(), JsonRpcRequest{
		Method: "createwallet",
		Params: map[string]any{
			"wallet_name": "test",
		},
	}); err != nil && !strings.Contains(err.Error(), "already exists") {
		panic(err)
	}

	rpc.Do(context.Background(), JsonRpcRequest{
		Method: "loadwallet",
		Params: []any{
			"test",
		},
	})

	return rpc
}

func bitcoinHelperCreateWallet(t *testing.T, base JsonRpcClient, walletName string) JsonRpcClient {
	t.Helper()

	// create wallet
	_, err := base.Do(context.Background(), JsonRpcRequest{
		Method: "createwallet",
		Params: map[string]any{
			"wallet_name": walletName,
		},
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("createwallet(%s): %v", walletName, err)
	}

	base.Do(context.Background(), JsonRpcRequest{
		Method: "loadwallet",
		Params: []any{
			walletName,
		},
	})

	// return scoped wallet RPC client
	return NewJsonRpcClient(JsonRpcClientConfig{
		Host:     base.conf.Host + "wallet/" + walletName,
		Username: base.conf.Username,
		Password: base.conf.Password,
	})
}

func bitcoinMineBlocks(t *testing.T, rpc JsonRpcClient, addr string, n int) {
	t.Helper()

	_, err := rpc.Do(context.Background(), JsonRpcRequest{
		Method: "generatetoaddress",
		Params: []any{n, addr},
	})
	if err != nil {
		t.Fatalf("mine block: %v", err)
	}
}

func TestBitcoinChain(t *testing.T) {
	b := NewBitcoin(JsonRpcClientConfig{})
	if b.Chain() != Bitcoin {
		t.Fatalf("expected Bitcoin chain")
	}
}

func TestLitecoinChain(t *testing.T) {
	l := NewLitecoin(JsonRpcClientConfig{})
	if l.Chain() != Litecoin {
		t.Fatalf("expected Litecoin chain")
	}
}

func TestBitcoinDecimals(t *testing.T) {
	b := NewBitcoin(JsonRpcClientConfig{})
	if b.Decimals() != 8 {
		t.Fatalf("expected 8 decimals")
	}
}

func TestBitcoinCreateAddress(t *testing.T) {
	rpc := bitcoinHelperCreateEnv(t)

	p := NewBitcoin(JsonRpcClientConfig{
		Host:     rpc.conf.Host,
		Username: rpc.conf.Username,
		Password: rpc.conf.Password,
	})

	addr, err := p.CreateAddress(context.Background())
	if err != nil {
		t.Fatalf("CreateAddress: %v", err)
	}

	if addr == "" {
		t.Fatalf("expected non-empty address")
	}
}

func TestBitcoinPoll(t *testing.T) {
	node := bitcoinHelperCreateEnv(t)

	// wallets
	miner := bitcoinHelperCreateWallet(t, node, "miner")
	merchant := bitcoinHelperCreateWallet(t, node, "merchant")
	customer := bitcoinHelperCreateWallet(t, node, "customer")

	ctx := context.Background()

	// fund miner by mining
	var minerAddr string
	_ = RPCDo(ctx, miner, JsonRpcRequest{
		Method: "getnewaddress",
	}, &minerAddr)

	bitcoinMineBlocks(t, miner, minerAddr, 101)

	// create CUSTOMER address correctly (FIXED)
	var customerAddr string
	_ = RPCDo(ctx, customer, JsonRpcRequest{
		Method: "getnewaddress",
	}, &customerAddr)

	// fund customer from miner
	_, err := miner.Do(ctx, JsonRpcRequest{
		Method: "sendtoaddress",
		Params: []any{
			customerAddr,
			10.0,
		},
	})
	if err != nil {
		t.Fatalf("fund customer: %v", err)
	}

	bitcoinMineBlocks(t, miner, minerAddr, 1)

	// merchant invoice provider
	provider := NewBitcoin(JsonRpcClientConfig{
		Host:     merchant.conf.Host,
		Username: merchant.conf.Username,
		Password: merchant.conf.Password,
	})

	merchantAddr, err := provider.CreateAddress(ctx)
	if err != nil {
		t.Fatalf("CreateAddress: %v", err)
	}

	// invoice created BEFORE payment
	inv := Invoice{
		Chain:      Bitcoin,
		Address:    merchantAddr,
		AmountOwed: big.NewInt(50_000_000),
		AmountPaid: big.NewInt(0),
	}

	// FIRST POLL should be unpaid
	invoices, err := provider.Poll(ctx, []Invoice{inv})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if invoices[0].AmountPaid.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("expected no payment yet")
	}

	// CUSTOMER pays merchant
	_, err = customer.Do(ctx, JsonRpcRequest{
		Method: "sendtoaddress",
		Params: []any{
			merchantAddr,
			0.2,
		},
	})
	if err != nil {
		t.Fatalf("customer payment: %v", err)
	}

	bitcoinMineBlocks(t, miner, minerAddr, 1)

	// POLL should detect partial payment
	invoices, err = provider.Poll(ctx, invoices)
	if err != nil {
		t.Fatalf("Poll2: %v", err)
	}

	if invoices[0].AmountPaid.Cmp(big.NewInt(0)) <= 0 {
		t.Fatalf("expected partial payment detected")
	}

	// final payment
	_, err = customer.Do(ctx, JsonRpcRequest{
		Method: "sendtoaddress",
		Params: []any{
			merchantAddr,
			0.3,
		},
	})
	if err != nil {
		t.Fatalf("final payment: %v", err)
	}

	bitcoinMineBlocks(t, miner, minerAddr, 2)

	invoices, err = provider.Poll(ctx, invoices)
	if err != nil {
		t.Fatalf("Poll final: %v", err)
	}

	if invoices[0].Pending {
		t.Fatalf("expected invoice settled")
	}

	if !invoices[0].Paid() {
		t.Fatalf("expected invoice paid")
	}
}
