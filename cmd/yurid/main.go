package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"codeberg.org/lewdest/yuri"
	"codeberg.org/lewdest/yuri/cmd/yurid/yurid"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	go instance.Run(ctx)

	slog.Info("created instance", "instance", instance)

	activeChainNames := make([]string, 0, len(conf.Chains))
	for _, chain := range conf.Chains {
		activeChainNames = append(activeChainNames, string(chain.Chain()))
	}

	api := yurid.NewAPI(conf.Addr, database, instance, activeChainNames)

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
