package yuri

import (
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

// TODO: pin
const moneroTestImage = "melotools/monero:latest"

// moneroHelperCreateFullEnv starts monerod and monero-wallet-rpc, and creates
// a testing rpc wallet automatically
func moneroHelperCreateFullEnv(t *testing.T) (cModerod *yuritest.Container, cWalletd *yuritest.Container, cCustomerWalletD *yuritest.Container, walletJsonRpc, customerJsonRpc JsonRpcClient, daemonRPC JsonRpcClient) {
	t.Helper()

	wd, _ := os.Getwd()
	moneroDir := path.Join(wd, ".monero")
	dataDir, _ := filepath.Abs(path.Join(moneroDir, "data"))
	walletDir, _ := filepath.Abs(path.Join(moneroDir, "wallet"))

	env := yuritest.New(t)

	cModerod = env.Run(yuritest.Spec{
		Name:       t.Name() + "-monerod",
		Image:      moneroTestImage,
		Entrypoint: []string{"monerod"},
		Cmd: []string{
			"--regtest",
			"--data-dir", "/monero",
			"--offline",
			"--keep-fakechain",
			"--rpc-bind-ip", "0.0.0.0",
			"--rpc-bind-port", "28081",
			"--confirm-external-bind",
			"--fixed-difficulty", "1",
			"--non-interactive",
		},
		Port: "28081",
		Mounts: []testcontainers.ContainerMount{
			{
				Source: testcontainers.GenericBindMountSource{HostPath: dataDir},
				Target: "/monero",
			},
		},
		Wait: wait.ForListeningPort("28081/tcp"),
	})

	daemonRPC = NewJsonRpcClient(JsonRpcClientConfig{
		Host: cModerod.HTTP() + "/json_rpc",
	})

	makeAndOpenWallet := func(wallet string) (*yuritest.Container, JsonRpcClient) {
		cWalletd = env.Run(yuritest.Spec{
			Name:       t.Name() + "-walletd" + "-" + wallet,
			Image:      moneroTestImage,
			Entrypoint: []string{"monero-wallet-rpc"},
			Cmd: []string{
				"--wallet-dir", "/wallets",
				"--rpc-bind-ip", "0.0.0.0",
				"--rpc-bind-port", "28083",
				"--daemon-host", cModerod.Name(),
				"--daemon-port", "28081",
				"--disable-rpc-login",
				"--confirm-external-bind",
				"--non-interactive",
				"--log-level", "2",
			},
			Port: "28083",
			Mounts: []testcontainers.ContainerMount{
				{
					Source: testcontainers.GenericBindMountSource{HostPath: walletDir},
					Target: "/wallets",
				},
			},
			Wait: wait.ForListeningPort("28083/tcp"),
		})

		walletJsonRpc = NewJsonRpcClient(JsonRpcClientConfig{Host: cWalletd.HTTP() + "/json_rpc"})
		_, err := walletJsonRpc.Do(JsonRpcRequest{
			Method: "create_wallet",
			Params: map[string]any{
				"filename": wallet,
				"language": "English",
			},
		})

		if err != nil && !strings.Contains(err.Error(), "Already exists.") {
			t.Fatalf("create_wallet err = %q", err)
		}

		_, err = walletJsonRpc.Do(JsonRpcRequest{
			Method: "open_wallet",
			Params: map[string]any{
				"filename": wallet,
			},
		})

		if err != nil {
			t.Fatalf("open_wallet err = %q", err)
		}

		return cWalletd, walletJsonRpc
	}

	cWalletd, walletJsonRpc = makeAndOpenWallet("yuri-monero-test-wallet")
	cCustomerWalletD, customerJsonRpc = makeAndOpenWallet("yuri-monero-test-customer-wallet")
	return
}

func TestMoneroChain(t *testing.T) {
	if got := NewMonero(JsonRpcClientConfig{}).Chain(); got != Monero {
		t.Fatalf("Name() = %q, want %q", got, Monero)
	}
}

func TestMoneroDecimals(t *testing.T) {
	if got := NewMonero(JsonRpcClientConfig{}).Decimals(); got != 12 {
		t.Fatalf("Decimals() = %q, want %q", got, 12)
	}
}

// TestMoneroCreateAddress tests that [monero.CreateAddress] creates a new address
func TestMoneroCreateAddress(t *testing.T) {
	_, _, _, walletJsonRpc, _, _ := moneroHelperCreateFullEnv(t)
	moneroProvider := NewMonero(walletJsonRpc.conf)

	addr, err := moneroProvider.CreateAddress()
	if err != nil {
		t.Fatalf("CreateAddress err = %q", err)
	}

	res, err := walletJsonRpc.Do(JsonRpcRequest{
		Method: "get_address_index",
		Params: map[string]any{
			"address": addr,
		},
	})

	if err != nil {
		t.Fatalf("get_address_index err = %q", err)
	}

	result, ok := res.Result["index"]
	if !ok {
		t.Fatalf("result['index'] does not exist")
	}

	t.Logf("address = %q", addr)
	t.Logf("account index = %q", result)
}

func moneroGenerateBlocks(t *testing.T, daemonRpc JsonRpcClient, addr string, blocks uint64) {
	t.Helper()

	_, err := daemonRpc.Do(JsonRpcRequest{
		Method: "generateblocks",
		Params: map[string]any{
			"amount_of_blocks": blocks,
			"wallet_address":   addr,
		},
	})

	if err != nil {
		t.Fatalf("generateblocks = %q", err)
	}
}

func TestMoneroPoll(t *testing.T) {
	_, _, _, merchantJsonRpc, customerJsonRpc, daemonJsonRpc := moneroHelperCreateFullEnv(t)

	resp, err := customerJsonRpc.Do(JsonRpcRequest{
		Method: "get_address",
		Params: map[string]any{
			"account_index": 0,
			"address_index": 0,
		},
	})
	if err != nil {
		t.Fatalf("get_address(customer) = %q", err)
	}

	customerAddr, ok := resp.Result["address"].(string)
	if !ok {
		t.Fatalf("get_address(customer) = failed to get address from result")
	}

	moneroGenerateBlocks(t, daemonJsonRpc, customerAddr, 30)

	monero := NewMonero(merchantJsonRpc.conf)
	merchantAddr, err := monero.CreateAddress()
	if err != nil {
		t.Fatalf("CreateAddress() = %q", err)
	}

	inv := Invoice{
		Chain:      Monero,
		Address:    merchantAddr,
		AmountOwed: big.NewInt(10_000_000_000),
		AmountPaid: big.NewInt(0),
		Token:      Token{},
		Pending:    false,
		Metadata:   map[string]any{},
	}

	invoices, err := monero.Poll(t.Context(), []Invoice{inv})
	if err != nil {
		t.Fatalf("Poll() = %q", err)
	}

	if len(invoices) != 1 {
		t.Fatalf("Poll() should have returned 1 invoice back")
	}

	_, err = customerJsonRpc.Do(JsonRpcRequest{
		Method: "transfer",
		Params: map[string]any{
			"destinations": []map[string]any{
				{
					"address": merchantAddr,
					"amount":  10_000_000_000,
				},
			},
		},
	})

	if err != nil {
		t.Fatalf("transfer(customer) = %q", err)
	}

	moneroGenerateBlocks(t, daemonJsonRpc, customerAddr, 1)
	merchantJsonRpc.Do(JsonRpcRequest{
		Method: "refresh",
	})

	invoices, err = monero.Poll(t.Context(), invoices)
	if err != nil {
		t.Fatalf("Poll2() = %q", err)
	}

	if !invoices[0].Pending {
		t.Fatalf("Invoice should be pending after 1 conf")
	}

	moneroGenerateBlocks(t, daemonJsonRpc, customerAddr, 9)
	merchantJsonRpc.Do(JsonRpcRequest{
		Method: "refresh",
	})

	invoices, err = monero.Poll(t.Context(), invoices)
	if err != nil {
		t.Fatalf("Poll3() = %q", err)
	}

	if invoices[0].Pending {
		t.Fatalf("Invoice should no longer be pending after 9 more blocks")
	}

	if !invoices[0].Paid() {
		t.Fatalf("Invoice should be paid")
	}
}
