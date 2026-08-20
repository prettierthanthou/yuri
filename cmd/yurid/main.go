package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"codeberg.org/lewdest/yuri"
	"codeberg.org/lewdest/yuri/cmd/yurid/yurid"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func main() {
	debug := slices.Contains(os.Args, "+debug")
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

	wrappedPricingProviders := make([]yuri.PriceProvider, 0, len(conf.PricingProviders))
	for _, provider := range conf.PricingProviders {
		wrappedPricingProviders = append(wrappedPricingProviders, yuri.NewCachedPriceProviderWithTTL(provider, 5*time.Minute))
	}

	database, err := yurid.NewDatabase(conf.DatabaseConfig)
	if err != nil {
		slog.Error("failed to open database", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := yurid.NewEventServer()

	instance, err := yuri.New(yuri.Options{
		Hooks: yuri.Hooks{
			OnError: func(err error) {
				slog.Error("Something went wrong during cycle!", "err", err)
			},
			OnInvoiceUpdated: func(ctx context.Context, i yuri.Invoice) error {
				slog.Debug("invoice updated", "invoice", i)
				events.PublishInvoice(&i)
				return nil
			},
		},
		Chains:  conf.Chains,
		Pricing: wrappedPricingProviders,
		Storage: database,
	})
	if err != nil {
		slog.Error("failed to create instance", "err", err)
		return
	}

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		instance.Run(ctx)
	}()

	slog.Info("created instance", "instance", instance)

	activeChainNames := make([]string, 0, len(conf.Chains))
	for _, chain := range conf.Chains {
		activeChainNames = append(activeChainNames, string(chain.Chain()))
	}

	api := yurid.NewAPI(database, instance, activeChainNames, conf.APIToken, events)

	srv := &http.Server{
		Addr:              conf.Addr,
		Handler:           api.Handler(),
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("starting REST API at", "url", conf.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api ListenAndServe failed", "err", err)
			os.Exit(1)
		}
	}()

	sig := <-sigCh
	slog.Info("received signal, shutting down", "signal", sig)

	cancel()

	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		slog.Warn("poller did not stop in time, continuing shutdown")
	}

	events.Close()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown failed", "err", err)
	}

	if err := database.Close(); err != nil {
		slog.Error("database close failed", "err", err)
	}

	slog.Info("shutdown complete")
}
