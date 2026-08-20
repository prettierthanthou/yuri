package yurid

import (
	"context"
	"crypto"
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
	"slices"
	"sort"
	"strings"
	"time"

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

var supportedChainNames = []yuri.Chain{
	yuri.Bitcoin,
	yuri.Litecoin,
	yuri.Dogecoin,
	yuri.Ethereum,
	yuri.BNB,
	yuri.Polygon,
	yuri.Avalanche,
	yuri.Arbitrum,
	yuri.Base,
	yuri.Optimism,
	yuri.Fantom,
	yuri.Monero,
	// NOTE: TON and SOL are handled explicitly in ParseConfig as they
	// require a wallet output directory
	yuri.Ton,
	yuri.Solana,
}

// chainBuilder adapts a concrete provider constructor to one returning
// the CryptoProvider interface.
func chainBuilder[P yuri.CryptoProvider](new func(yuri.JsonRpcClientConfig) P) func(yuri.JsonRpcClientConfig) yuri.CryptoProvider {
	return func(rpc yuri.JsonRpcClientConfig) yuri.CryptoProvider { return new(rpc) }
}

var supportedChains = map[yuri.Chain]func(yuri.JsonRpcClientConfig) yuri.CryptoProvider{
	yuri.Bitcoin:   chainBuilder(yuri.NewBitcoin),
	yuri.Litecoin:  chainBuilder(yuri.NewLitecoin),
	yuri.Dogecoin:  chainBuilder(yuri.NewDogecoin),
	yuri.Ethereum:  chainBuilder(yuri.NewEthereum),
	yuri.BNB:       chainBuilder(yuri.NewBNB),
	yuri.Polygon:   chainBuilder(yuri.NewPolygon),
	yuri.Avalanche: chainBuilder(yuri.NewAvalanche),
	yuri.Arbitrum:  chainBuilder(yuri.NewArbitrum),
	yuri.Base:      chainBuilder(yuri.NewBase),
	yuri.Optimism:  chainBuilder(yuri.NewOptimism),
	yuri.Fantom:    chainBuilder(yuri.NewFantom),
	yuri.Monero:    chainBuilder(yuri.NewMonero),
}

var supportedPricingProviders = map[string]func(*http.Client) yuri.PriceProvider{
	"coingecko":         yuri.NewCoinGeckoPriceProvider,
	"btcturk":           yuri.NewBtcTurkPriceProvider,
	"barebitcoin":       yuri.NewBareBitcoinPriceProvider,
	"bitbank":           yuri.NewBitbankPriceProvider,
	"bitcoinkenya":      yuri.NewBitcoinKenyaPriceProvider,
	"bitflyer":          yuri.NewBitflyerPriceProvider,
	"bitmynt":           yuri.NewBitmyntPriceProvider,
	"bitnob":            yuri.NewBitnobPriceProvider,
	"bitpay":            yuri.NewBitpayPriceProvider,
	"buda":              yuri.NewBudaPriceProvider,
	"bylls":             yuri.NewByllsPriceProvider,
	"coindcx":           yuri.NewCoinDCXPriceProvider,
	"coinmate":          yuri.NewCoinmatePriceProvider,
	"cryptomarket":      yuri.NewCryptoMarketPriceProvider,
	"desiboard":         yuri.NewDesiboardPriceProvider,
	"freecurrencyrates": yuri.NewFreeCurrencyRatesPriceProvider,
	"hitbtc":            yuri.NewHitBTCPriceProvider,
	"kraken":            yuri.NewKrakenPriceProvider,
	"luno":              yuri.NewLunoPriceProvider,
	"ripio":             yuri.NewRipioPriceProvider,
	"yadio":             yuri.NewYadioPriceProvider,
	"null":              func(*http.Client) yuri.PriceProvider { return yuri.NewNullPriceProvider() },
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
	APIToken         string
}

func pricingProviderNames() []string {
	names := make([]string, 0, len(supportedPricingProviders))
	for name := range supportedPricingProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type pricingProviderSliceFlag []string

func (s *pricingProviderSliceFlag) String() string {
	return strings.Join(pricingProviderNames(), ", ")
}

func (s *pricingProviderSliceFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if p == "ALL" {
			for _, name := range pricingProviderNames() {
				if name != "null" && !slices.Contains(*s, name) {
					*s = append(*s, name)
				}
			}
			continue
		}

		if _, ok := supportedPricingProviders[p]; !ok {
			return fmt.Errorf("unknown pricing provider: %q", p)
		}

		*s = append(*s, p)
	}

	return nil
}

func ParseConfig() (Configuration, error) {
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	var addr string
	fs.StringVar(&addr, "addr", ":6761", "address to bind to")

	var apiToken string
	fs.StringVar(&apiToken, "api-token", "", "optional bearer token required by the REST API")

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

	for _, chain := range supportedChainNames {
		cfg := &CryptoConfiguration{}
		chainConfigs[chain] = cfg

		prefix := string(chain)

		fs.StringVar(&cfg.Host, prefix+"-host", "", "JSON-RPC host")
		fs.StringVar(&cfg.Username, prefix+"-username", "", "JSON-RPC username")
		fs.StringVar(&cfg.Password, prefix+"-password", "", "JSON-RPC password")
		fs.StringVar(&cfg.Proxy, prefix+"-proxy", "", "SOCKS5 proxy")

		if chain == yuri.Solana || chain == yuri.Ton {
			fs.StringVar(&cfg.walletOutDir, prefix+"-wallet-dir", "", "output directory for wallets")
		}
	}

	var pricingProviderNamesFlag pricingProviderSliceFlag
	fs.Var(
		&pricingProviderNamesFlag,
		"price",
		fmt.Sprintf("List of pricing providers (or ALL), either as repeated flags or comma separated: %s", strings.Join(pricingProviderNames(), ", ")),
	)

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Printf("Example usage:\n%s", exampleUsage)
		}
		return Configuration{}, err
	}

	client, err := defaultHTTPClient(globalProxy)
	if err != nil {
		return Configuration{}, fmt.Errorf("global proxy: %w", err)
	}

	chains, err := buildChainProviders(chainConfigs, client)
	if err != nil {
		return Configuration{}, err
	}

	priceProviders, err := buildPricingProviders(client, pricingProviderNamesFlag)
	if err != nil {
		return Configuration{}, err
	}

	dbType := DatabaseType(strings.ToLower(databaseType))
	switch dbType {
	case DatabaseTypeSqlite, DatabaseTypePostgres, DatabaseTypeMysql:
	default:
		return Configuration{}, fmt.Errorf("database type '%s' is not valid (sqlite|mysql|postgresql)", databaseType)
	}

	return Configuration{
		Addr:             addr,
		APIToken:         apiToken,
		Chains:           chains,
		PricingProviders: priceProviders,
		DatabaseConfig:   DatabaseConfig{Type: dbType, DSN: databaseDsn},
	}, nil
}

// buildChainProviders validates each enabled chain config and constructs
// its provider. TON and Solana additionally require a wallet output
// directory to persist freshly generated keypairs.
func buildChainProviders(chainConfigs map[yuri.Chain]*CryptoConfiguration, client *http.Client) ([]yuri.CryptoProvider, error) {
	providers := make([]yuri.CryptoProvider, 0, len(chainConfigs))

	for chain, cfg := range chainConfigs {
		if !cfg.Enabled() {
			continue
		}

		if err := cfg.Validate(chain); err != nil {
			return nil, err
		}

		switch chain {
		case yuri.Ton:
			hooks, err := walletHooks(chain, cfg.walletOutDir)
			if err != nil {
				return nil, err
			}

			host := cfg.Host
			if host == "" {
				host = yuri.TonMainnetPublic
			}

			ton, err := yuri.NewTonWithConfigUrl(yuri.TonOptions{Hooks: hooks}, host)
			if err != nil {
				return nil, fmt.Errorf("failed to create TON provider: %w", err)
			}

			providers = append(providers, ton)
		case yuri.Solana:
			hooks, err := walletHooks(chain, cfg.walletOutDir)
			if err != nil {
				return nil, err
			}

			rpc, err := cfg.RPCConfig(chain, client)
			if err != nil {
				return nil, err
			}

			providers = append(providers, yuri.NewSolana(yuri.SolanaOptions{
				Hooks: hooks,
				Rpc:   rpc,
			}))
		default:
			rpc, err := cfg.RPCConfig(chain, client)
			if err != nil {
				return nil, err
			}

			providers = append(providers, supportedChains[chain](rpc))
		}
	}

	if len(providers) == 0 {
		return nil, errors.New("at least 1 CryptoProvider must be specified")
	}

	return providers, nil
}

func buildPricingProviders(client *http.Client, names []string) ([]yuri.PriceProvider, error) {
	if len(names) == 0 {
		return nil, errors.New("at least 1 pricing provider must be specified")
	}

	providers := make([]yuri.PriceProvider, 0, len(names))
	for _, name := range names {
		build, ok := supportedPricingProviders[name]
		if !ok {
			return nil, fmt.Errorf("unsupported pricing provider: %q", name)
		}

		if name == "null" {
			slog.Warn("null pricing provider enabled; prices will always be zero")
		}

		providers = append(providers, build(client))
	}

	return providers, nil
}

// walletHooks persists freshly generated keypairs for chains that need a
// wallet output directory (TON, Solana).
func walletHooks(chain yuri.Chain, walletOutDir string) (yuri.ProviderHooks, error) {
	if walletOutDir == "" {
		return yuri.ProviderHooks{}, fmt.Errorf("%s: wallet-dir is required", chain)
	}

	return yuri.ProviderHooks{
		OnNewAddress: func(_ context.Context, public crypto.PublicKey, private crypto.PrivateKey) error {
			edPub, ok := public.(ed25519.PublicKey)
			if !ok {
				return fmt.Errorf("unexpected public key type %T", public)
			}

			edPriv, ok := private.(ed25519.PrivateKey)
			if !ok {
				return fmt.Errorf("unexpected private key type %T", private)
			}

			return os.WriteFile(
				path.Join(walletOutDir, base64.RawStdEncoding.EncodeToString(edPub)),
				edPriv,
				0600,
			)
		},
	}, nil
}

func (c CryptoConfiguration) Enabled() bool {
	return c.Host != "" ||
		c.Username != "" ||
		c.Password != "" ||
		c.Proxy != "" ||
		c.walletOutDir != ""
}

func (c CryptoConfiguration) Validate(chain yuri.Chain) error {
	switch {
	case !c.Enabled():
		return nil
	case chain == yuri.Ton:
		// TON defaults to the public mainnet config, host is optional
		if c.walletOutDir == "" {
			return fmt.Errorf("%s: wallet-dir is required", chain)
		}
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

var defaultHTTPTimeout = 15 * time.Second

func defaultHTTPClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return &http.Client{Timeout: defaultHTTPTimeout}, nil
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
		Timeout: defaultHTTPTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if cd, ok := dialer.(proxy.ContextDialer); ok {
					return cd.DialContext(ctx, network, addr)
				}
				return dialer.Dial(network, addr)
			},
		},
	}, nil
}
