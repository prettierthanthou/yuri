package main

import (
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

	storage := &yuri.InMemoryStorage{}
	instance, err := yuri.New(yuri.Options{
		Hooks: yuri.Hooks{
			OnError: func(err error) {
				slog.Error("Something went wrong during cycle!", "err", err)
			},
		},
		Chains:  conf.Chains,
		Pricing: conf.PricingProviders,
		Storage: storage,
	})
	if err != nil {
		slog.Error("failed to create instance", "err", err)
		return
	}

	slog.Info("created instance", "instance", instance)

	slog.Info("starting REST API at", "url", conf.Addr)
	api := yurid.NewAPI(conf.Addr, storage, instance)
	if err := api.ListenAndServe(); err != nil {
		slog.Error("api ListenAndServe failed", "err", err)
	}
}
