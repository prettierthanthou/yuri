package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"strings"

	"codeberg.org/lewdest/yuri"
	"codeberg.org/lewdest/yuri/internal/yurid"
)

func main() {
	debug := strings.Contains(strings.Join(os.Args, " "), "+debug")
	if debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
		slog.Info("log level set to debug")
	}

	conf, err := yurid.ParseConfig()
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}

		slog.Error("failed to parse configuration", "err", err)
		return
	}

	slog.Debug("parsed configuration", "conf", conf)

	database, err := yurid.NewDatabase(conf.DatabaseConfig)
	if err != nil {
		slog.Error("failed to open database", "err", err)
		return
	}

	instance, err := yuri.New(yuri.Options{
		Hooks: yuri.Hooks{
			OnError: func(err error) {
				slog.Error("Something went wrong during cycle!", "err", err)
			},
			OnInvoiceUpdated: func(ctx context.Context, i yuri.Invoice) error {
				// TODO: maybe send down events via SSE or something? idk
				slog.Debug("invoice updated", "invoice", i)
				return nil
			},
		},
		Chains:  conf.Chains,
		Pricing: conf.PricingProviders,
		Storage: database,
	})
	if err != nil {
		slog.Error("failed to create instance", "err", err)
		return
	}

	go instance.Run(context.Background())

	slog.Info("created instance", "instance", instance)

	activeChainNames := make([]string, 0, len(conf.Chains))
	for _, chain := range conf.Chains {
		activeChainNames = append(activeChainNames, string(chain.Chain()))
	}

	slog.Info("starting REST API at", "url", conf.Addr)
	api := yurid.NewAPI(conf.Addr, database, instance, activeChainNames)
	if err := api.ListenAndServe(); err != nil {
		slog.Error("api ListenAndServe failed", "err", err)
	}
}
