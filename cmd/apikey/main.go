package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mihavo/gct/internal/gate"
)

func main() {
	flag.Parse()
	if flag.NArg() != 1 || flag.Arg(0) == "" {
		fmt.Fprintln(os.Stderr, "usage: apikey <identity-name>")
		os.Exit(2)
	}
	name := flag.Arg(0)

	ctx := context.Background()
	redisClient, err := gate.NewRedisClient()
	if err != nil {
		log.Fatalf("creating redis client: %v", err)
	}
	defer redisClient.Close()

	seq := make([]byte, 32)
	_, err = rand.Read(seq)
	if err != nil {
		log.Fatalf("could not generate key: %v", err)
	}
	key := hex.EncodeToString(seq)
	hash := gate.GenerateKeyHash(key)
	err = redisClient.Set(ctx, gate.AuthKeyPrefix+hash, name, 0).Err()

	if err != nil {
		log.Fatalf("could not store key for %q: %v", name, err)
	}

	fmt.Fprintf(os.Stderr, "\nAPI key created for %q\n", name)
	fmt.Fprintf(os.Stderr, "Send it in the %s header. It is shown only once.\n\n", gate.AuthKeyHeader)
	fmt.Println(key)
}
