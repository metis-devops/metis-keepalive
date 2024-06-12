package main

import (
	"context"
	"flag"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var (
		rpc      string
		key      string
		interval time.Duration
	)

	flag.StringVar(&rpc, "rpc", "https://andromeda.metis.io", "rpc endpoint")
	flag.StringVar(&key, "key", "", "raw private key")
	flag.DurationVar(&interval, "interval", time.Second*30, "interval to send a next tx")
	flag.Parse()

	basectx, cancel := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	wallet, err := NewWallet(basectx, key, rpc)
	if err != nil {
		panic(err)
	}

	wallet.Start(basectx, interval)
}
