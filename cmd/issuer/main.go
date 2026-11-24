package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mihavo/gct/internal/config"
	"github.com/mihavo/gct/internal/gate"
	"github.com/mihavo/gct/internal/issuer"
	"github.com/mihavo/gct/internal/keystore"
	"github.com/mihavo/gct/internal/serve"
)

const (
	shutdownTimeout = 8 * time.Second
	defaultKeysDir  = "./keys"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "serve":
		runServe()
	case "keygen":
		runKeygen(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: issuer <serve|keygen> [flags]")
	os.Exit(2)
}

func keysDirFromEnv() string {
	keysDir, ok := os.LookupEnv("ISSUER_KEYS_DIR")
	if !ok {
		return defaultKeysDir
	}
	return keysDir
}

func runKeygen(args []string) {
	flags := flag.NewFlagSet("keygen", flag.ExitOnError)
	dir := flags.String("dir", keysDirFromEnv(), "directory to write the new private key into")
	flags.Parse(args)

	kid, path, err := keystore.Generate(*dir)
	if err != nil {
		log.Fatalf("generating signing key: %v", err)
	}
	fmt.Printf("kid=%s\npath=%s\n", kid, path)
}

func runServe() {
	ctx := context.Background()
	configPath, ok := os.LookupEnv("ISSUER_CONFIG_PATH")
	if !ok {
		configPath = "./configs/issuer.yml"
	}
	config, err := config.LoadYAML[config.IssuerConfig](configPath)
	if err != nil {
		log.Fatalf("loading issuer config: %v", err)
	}
	slog.Debug("issuer config loaded: %+v", "config", config)

	keysDir := keysDirFromEnv()
	created, err := keystore.EnsureKey(keysDir)
	if err != nil {
		log.Fatalf("preparing signing keys: %v", err)
	}
	if created {
		slog.Info("no signing key found, generated the first one", "dir", keysDir)
	}

	redisClient, err := gate.NewRedisClient()
	if err != nil {
		log.Fatalf("creating redis client: %v", err)
	}
	defer redisClient.Close()

	issuer, err := issuer.NewIssuer(ctx, redisClient, *config)

	if err != nil {
		log.Fatalf("could not initialize issuer %v", err)
	}
	notifyCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serve.Run(notifyCtx, issuer.Server, shutdownTimeout); err != nil {
		log.Fatalf("issuer server: %v", err)
	}
	slog.Info("issuer server is down")
}
