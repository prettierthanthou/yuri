package yuri

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	"codeberg.org/lewdest/yuri/yuritest"
	"github.com/testcontainers/testcontainers-go/wait"
)

const moneroTestImage = "melotools/monero@sha256:7fc3357602845fa2e35e1ac25fd0f035f4c72da188a7c9ef76a6cf3203d0bb57"

// moneroHelperCreateFullEnv starts monerod and monero-wallet-rpc, and creates
// a testing rpc wallet automatically
func moneroHelperCreateFullEnv(t *testing.T) (cModerod *yuritest.Container, cWalletd *yuritest.Container, cCustomerWalletD *yuritest.Container, walletJsonRpc, customerJsonRpc JsonRpcClient, daemonRPC JsonRpcClient) {
	t.Helper()

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
		Port:   "28081",
		Mounts: nil,
		Wait:   wait.ForListeningPort("28081/tcp"),
	})

	daemonRPC = NewJsonRpcClient(JsonRpcClientConfig{
		Host:            cModerod.HTTP() + "/json_rpc",
		NonB64BasicAuth: true,
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
			Port:   "28083",
			Mounts: nil,
			Wait:   wait.ForListeningPort("28083/tcp"),
		})

		walletJsonRpc = NewJsonRpcClient(JsonRpcClientConfig{Host: cWalletd.HTTP() + "/json_rpc", NonB64BasicAuth: true})
		_, err := walletJsonRpc.Do(context.Background(), JsonRpcRequest{
			Method: "create_wallet",
			Params: map[string]any{
				"filename": wallet,
				"language": "English",
			},
		})

		if err != nil && !strings.Contains(err.Error(), "Already exists.") {
			t.Fatalf("create_wallet err = %q", err)
		}

		_, err = walletJsonRpc.Do(context.Background(), JsonRpcRequest{
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

	addr, err := moneroProvider.CreateAddress(context.Background())
	if err != nil {
		t.Fatalf("CreateAddress err = %q", err)
	}

	type getAddrIndexResp struct {
		Index struct {
			Major uint64 `json:"major"`
			Minor uint64 `json:"minor"`
		} `json:"index"`
	}

	var resp getAddrIndexResp
	err = RPCDo(context.Background(), walletJsonRpc, JsonRpcRequest{
		Method: "get_address_index",
		Params: map[string]any{
			"address": addr,
		},
	}, &resp)
	if err != nil {
		t.Fatalf("get_address_index = %q", err)
	}
}

func moneroGenerateBlocks(t *testing.T, daemonRpc JsonRpcClient, addr string, blocks uint64) {
	t.Helper()

	_, err := daemonRpc.Do(context.Background(), JsonRpcRequest{
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

// this test is explicitly named different so that
// running it when attempting to run the Monero tests
// is less likely (via --run)
//
// TODO: fix the fact this takes ~20 seconds to run.
// this aids. not my main issue right now though.

func TestPollMonero(t *testing.T) {
	_, _, _, merchantJsonRpc, customerJsonRpc, daemonJsonRpc := moneroHelperCreateFullEnv(t)

	type getAddress struct {
		Address string `json:"address"`
	}

	var getCustomerAddressResp getAddress
	err := RPCDo(context.Background(), customerJsonRpc, JsonRpcRequest{
		Method: "get_address",
		Params: map[string]any{
			"account_index": 0,
			"address_index": 0,
		},
	}, &getCustomerAddressResp)
	if err != nil {
		t.Fatalf("get_address(customer) = %q", err)
	}

	start := time.Now()
	moneroGenerateBlocks(t, daemonJsonRpc, getCustomerAddressResp.Address, 30)
	t.Log("30 blocks:", time.Since(start))

	monero := NewMonero(merchantJsonRpc.conf)
	merchantAddr, err := monero.CreateAddress(context.Background())
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

	allInvoices := []Invoice{inv}
	poll1ResultInvoices, err := monero.Poll(t.Context(), allInvoices)
	if err != nil {
		t.Fatalf("Poll() = %q", err)
	}

	if len(poll1ResultInvoices) != 0 {
		t.Fatalf("Poll() should have returned 0 invoices back")
	}

	start = time.Now()
	_, err = customerJsonRpc.Do(context.Background(), JsonRpcRequest{
		Method: "transfer",
		Params: map[string]any{
			"destinations": []map[string]any{
				{
					"address": merchantAddr,
					"amount":  int64(10_000_000_000),
				},
			},
		},
	})

	customerJsonRpc.Do(context.Background(), JsonRpcRequest{
		Method: "refresh",
	})
	t.Log("transfer + refresh:", time.Since(start))

	if err != nil {
		t.Fatalf("transfer(customer) = %q", err)
	}

	start = time.Now()
	moneroGenerateBlocks(t, daemonJsonRpc, getCustomerAddressResp.Address, 1)
	t.Log("1 block:", time.Since(start))

	start = time.Now()
	merchantJsonRpc.Do(context.Background(), JsonRpcRequest{
		Method: "refresh",
	})
	t.Log("merchantJsonRpc refresh:", time.Since(start))

	poll2ResultInvoices, err := monero.Poll(t.Context(), allInvoices)
	if err != nil {
		t.Fatalf("Poll2() = %q", err)
	}

	if !poll2ResultInvoices[0].Pending {
		t.Fatalf("Invoice should be pending after 1 conf")
	}

	start = time.Now()
	moneroGenerateBlocks(t, daemonJsonRpc, getCustomerAddressResp.Address, 9)
	t.Log("1 block:", time.Since(start))

	start = time.Now()
	merchantJsonRpc.Do(context.Background(), JsonRpcRequest{
		Method: "refresh",
	})
	t.Log("merchantJsonRpc refresh:", time.Since(start))
	poll3ResultValues, err := monero.Poll(t.Context(), allInvoices)
	if err != nil {
		t.Fatalf("Poll3() = %q", err)
	}

	if poll3ResultValues[0].Pending {
		t.Fatalf("Invoice should no longer be pending after 9 more blocks")
	}

	if !poll3ResultValues[0].Paid() {
		t.Fatalf("Invoice should be paid")
	}
}
