package yurid

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"codeberg.org/lewdest/yuri"
	"golang.org/x/net/proxy"
)

const exampleUsage = `yurid \
	-price ALL \
	-monero-host localhost:28081/json_rpc \
 	-monero-username foo \
	-monero-password bar \
	-database postgresql
	-database-dsn postgresql://root:toor@localhost/yurid
`

var supportedChains = map[yuri.Chain]func(rpc yuri.JsonRpcClientConfig) yuri.CryptoProvider{
	yuri.Bitcoin:  func(rpc yuri.JsonRpcClientConfig) yuri.CryptoProvider { return yuri.NewBitcoin(rpc) },
	yuri.Litecoin: func(rpc yuri.JsonRpcClientConfig) yuri.CryptoProvider { return yuri.NewBitcoin(rpc) },
	yuri.Ethereum: func(rpc yuri.JsonRpcClientConfig) yuri.CryptoProvider { return yuri.NewEthereum(rpc) },
	yuri.BNB:      func(rpc yuri.JsonRpcClientConfig) yuri.CryptoProvider { return yuri.NewBNB(rpc) },
	yuri.Monero:   func(rpc yuri.JsonRpcClientConfig) yuri.CryptoProvider { return yuri.NewMonero(rpc) },
	// yuri.Solana:   func(rpc yuri.JsonRpcClientConfig) yuri.CryptoProvider { return yuri.NewSolana(rpc) },
}

var supportedPricingProviders map[string]func(c *http.Client) yuri.PriceProvider = map[string]func(c *http.Client) yuri.PriceProvider{
	"coingecko":         func(c *http.Client) yuri.PriceProvider { return yuri.NewCoinGeckoPriceProvider(c) },
	"btcturk":           func(c *http.Client) yuri.PriceProvider { return yuri.NewBtcTurkPriceProvider(c) },
	"barebitcoin":       func(c *http.Client) yuri.PriceProvider { return yuri.NewBareBitcoinPriceProvider(c) },
	"bitbank":           func(c *http.Client) yuri.PriceProvider { return yuri.NewBitbankPriceProvider(c) },
	"bitcoinkenya":      func(c *http.Client) yuri.PriceProvider { return yuri.NewBitcoinKenyaPriceProvider(c) },
	"bitflyer":          func(c *http.Client) yuri.PriceProvider { return yuri.NewBitflyerPriceProvider(c) },
	"bitmynt":           func(c *http.Client) yuri.PriceProvider { return yuri.NewBitmyntPriceProvider(c) },
	"bitnob":            func(c *http.Client) yuri.PriceProvider { return yuri.NewBitnobPriceProvider(c) },
	"bitpay":            func(c *http.Client) yuri.PriceProvider { return yuri.NewBitpayPriceProvider(c) },
	"buda":              func(c *http.Client) yuri.PriceProvider { return yuri.NewBudaPriceProvider(c) },
	"bylls":             func(c *http.Client) yuri.PriceProvider { return yuri.NewByllsPriceProvider(c) },
	"coindcx":           func(c *http.Client) yuri.PriceProvider { return yuri.NewCoinDCXPriceProvider(c) },
	"coinmate":          func(c *http.Client) yuri.PriceProvider { return yuri.NewCoinmatePriceProvider(c) },
	"cryptomarket":      func(c *http.Client) yuri.PriceProvider { return yuri.NewCryptoMarketPriceProvider(c) },
	"desiboard":         func(c *http.Client) yuri.PriceProvider { return yuri.NewDesiboardPriceProvider(c) },
	"freecurrencyrates": func(c *http.Client) yuri.PriceProvider { return yuri.NewFreeCurrencyRatesPriceProvider(c) },
	"hitbtc":            func(c *http.Client) yuri.PriceProvider { return yuri.NewHitBTCPriceProvider(c) },
	"kraken":            func(c *http.Client) yuri.PriceProvider { return yuri.NewKrakenPriceProvider(c) },
	"luno":              func(c *http.Client) yuri.PriceProvider { return yuri.NewLunoPriceProvider(c) },
	"ripio":             func(c *http.Client) yuri.PriceProvider { return yuri.NewRipioPriceProvider(c) },
	"yadio":             func(c *http.Client) yuri.PriceProvider { return yuri.NewYadioPriceProvider(c) },
	"null":              func(_ *http.Client) yuri.PriceProvider { return yuri.NewNullPriceProvider() },
}

type CryptoConfiguration struct {
	Host         string
	Username     string
	Password     string
	Proxy        string
	walletOutDir string
}

type Configuration struct {
	Addr             string
	Chains           []yuri.CryptoProvider
	PricingProviders []yuri.PriceProvider
	DatabaseConfig   DatabaseConfig
}

type pricingProviderSliceFlag []string

func (s *pricingProviderSliceFlag) String() string {
	names := make([]string, 0, len(supportedPricingProviders))
	for k := range supportedPricingProviders {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (s *pricingProviderSliceFlag) Set(v string) error {
	parts := strings.SplitSeq(v, ",")

	for p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if p == "ALL" {
			slog.Debug("found 'ALL' price provider, registering all...")
			built := make([]string, 0, len(supportedPricingProviders))
			for k := range supportedPricingProviders {
				if k == "null" {
					continue
				}

				built = append(built, k)
			}

			slog.Debug("added price providers...", "built", built)

			*s = built
			return nil
		}

		if _, ok := supportedPricingProviders[p]; !ok {
			return fmt.Errorf("unknown pricing provider: %s", p)
		}

		*s = append(*s, p)
	}

	return nil
}

func (s pricingProviderSliceFlag) Contains(v string) bool {
	str := s.String()
	return strings.Contains(str, v)
}

func ParseConfig() (Configuration, error) {
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	var addr string
	fs.StringVar(&addr, "addr", ":6761", "address to bind to")

	var globalProxy string
	fs.StringVar(
		&globalProxy,
		"proxy",
		"",
		"Global SOCKS5 proxy used unless overridden by a chain.",
	)

	var databaseType, databaseDsn string
	fs.StringVar(&databaseType, "database-type", "", "database type (sqlite|mysql|postgresql)")
	fs.StringVar(&databaseDsn, "database-dsn", "", "database DSN")

	chainConfigs := make(map[yuri.Chain]*CryptoConfiguration)

	for chain := range supportedChains {
		cfg := &CryptoConfiguration{}
		chainConfigs[chain] = cfg

		prefix := string(chain)

		fs.StringVar(&cfg.Host, prefix+"-host", "", "JSON-RPC host")
		fs.StringVar(&cfg.Username, prefix+"-username", "", "JSON-RPC username")
		fs.StringVar(&cfg.Password, prefix+"-password", "", "JSON-RPC password")
		fs.StringVar(&cfg.Proxy, prefix+"-proxy", "", "SOCKS5 proxy")

		if chain == yuri.Solana {
			fs.StringVar(&cfg.walletOutDir, prefix+"-wallet-dir", "", "output directory for wallets")
		}
	}

	supportedProviders := make([]string, 0, len(supportedPricingProviders))
	for priceProviderKey := range supportedPricingProviders {
		supportedProviders = append(supportedProviders, priceProviderKey)
	}

	var pricingProviderNames pricingProviderSliceFlag
	fs.Var(&pricingProviderNames, "price", fmt.Sprintf("List of pricing providers (or ALL) either as new flags or comma seperated: %s", strings.Join(supportedProviders, ", ")))

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Printf("Example usage:\n%s", exampleUsage)
			return Configuration{}, err
		}
		return Configuration{}, err
	}

	// post process
	client, err := defaultHTTPClient(globalProxy)
	if err != nil {
		return Configuration{}, fmt.Errorf("global proxy: %w", err)
	}

	out := Configuration{
		Chains:           make([]yuri.CryptoProvider, 0, len(supportedChains)),
		PricingProviders: make([]yuri.PriceProvider, 0, len(supportedPricingProviders)),
	}

	hasSeenAtleast1ValidChainConf := false
	for chain, cfg := range chainConfigs {
		if !cfg.Enabled() {
			continue
		}

		if err := cfg.Validate(chain); err != nil {
			return Configuration{}, err
		}

		rpc, err := cfg.RPCConfig(chain, client)
		if err != nil {
			return Configuration{}, err
		}

		hasSeenAtleast1ValidChainConf = true
		// if chain != yuri.Solana {
		// 	continue
		// }

		getHooks := func() (yuri.ProviderHooks, error) {
			if cfg.walletOutDir == "" {
				return yuri.ProviderHooks{}, fmt.Errorf("expected solana to have a wallet-out-dir but instead recieved '%s'", cfg.walletOutDir)
			}

			return yuri.ProviderHooks{
				OnNewAddress: func(ctx context.Context, pk1 ed25519.PublicKey, pk2 ed25519.PrivateKey) error {
					return os.WriteFile(path.Join(cfg.walletOutDir, base64.RawStdEncoding.EncodeToString(pk1)), pk2, 0666)
				},
			}, nil
		}

		switch chain {
		case yuri.Solana:
			hooks, err := getHooks()
			if err != nil {
				return Configuration{}, err
			}

			out.Chains = append(out.Chains, yuri.NewSolana(yuri.SolanaOptions{
				Hooks: hooks,
				Rpc:   rpc,
			}))
		default:
			out.Chains = append(out.Chains, supportedChains[chain](rpc))
		}
	}

	if !hasSeenAtleast1ValidChainConf {
		return Configuration{}, errors.New("atleast 1 CryptoProvider must be specified")
	}

	if len(pricingProviderNames) == 0 {
		return Configuration{}, errors.New("atleast 1 pricing provider must be specified")
	}

	for _, priceProviderName := range pricingProviderNames {
		newFunc, ok := supportedPricingProviders[priceProviderName]
		if !ok {
			// NOTE: this is redudant as we can never get here but i'd rather.. it just exist
			slog.Error("unsupported pricing provider before runtime", "name", priceProviderName)
			return Configuration{}, fmt.Errorf("REALLY BAD STATE!! unsupported pricing provider '%s', please use -help", priceProviderName)
		}

		if priceProviderName == "null" {
			slog.Warn("null pricing provider is enabled, you most likely do not want this!")
		}

		provider := newFunc(client)
		out.PricingProviders = append(out.PricingProviders, provider)
	}

	out.Addr = addr

	dbTypLower := strings.ToLower(databaseType)
	if dbTypLower != string(DatabaseTypeMysql) && dbTypLower != string(DatabaseTypePostgres) && dbTypLower != string(DatabaseTypeSqlite) {
		return Configuration{}, fmt.Errorf("database type '%s' is not valid", dbTypLower)
	}

	out.DatabaseConfig = DatabaseConfig{
		Type: DatabaseType(dbTypLower),
		DSN:  databaseDsn,
	}

	return out, nil
}

func (c CryptoConfiguration) Enabled() bool {
	return c.Host != "" ||
		c.Username != "" ||
		c.Password != "" ||
		c.Proxy != ""
}

func (c CryptoConfiguration) Validate(chain yuri.Chain) error {
	switch {
	case !c.Enabled():
		return nil
	case c.Host == "":
		return fmt.Errorf("%s: host is required", chain)
	case c.Username == "" && c.Password != "":
		return fmt.Errorf("%s: username is required", chain)
	case c.Password == "" && c.Username != "":
		return fmt.Errorf("%s: password is required", chain)
	default:
		return nil
	}
}

func (c CryptoConfiguration) RPCConfig(chain yuri.Chain, client *http.Client) (yuri.JsonRpcClientConfig, error) {
	if c.Proxy != "" {
		var err error
		client, err = newHTTPClientFromSOCKS5(c.Proxy)
		if err != nil {
			return yuri.JsonRpcClientConfig{}, fmt.Errorf("%s proxy: %w", chain, err)
		}
	}

	return yuri.JsonRpcClientConfig{
		Host:     c.Host,
		Username: c.Username,
		Password: c.Password,
		Client:   client,
	}, nil
}

func defaultHTTPClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return http.DefaultClient, nil
	}

	return newHTTPClientFromSOCKS5(proxyURL)
}

func newHTTPClientFromSOCKS5(rawURL string) (*http.Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	var auth *proxy.Auth
	if u.User != nil {
		password, _ := u.User.Password()
		auth = &proxy.Auth{
			User:     u.User.Username(),
			Password: password,
		}
	}

	dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
	}, nil
}
