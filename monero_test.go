package yuri

import (
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
