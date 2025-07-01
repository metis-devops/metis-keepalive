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

		StartHeight uint64
	)

	flag.StringVar(&rpc, "rpc", "https://andromeda.metis.io", "rpc endpoint")
	flag.StringVar(&key, "key", "", "raw private key")
	flag.DurationVar(&interval, "interval", 0, "interval to send a next tx")
	flag.Uint64Var(&StartHeight, "start-height", 0, "start block number to send txs from")
	flag.Parse()

	basectx, cancel := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	wallet, err := NewWallet(basectx, key, rpc, StartHeight)
	if err != nil {
		panic(err)
	}

	wallet.Start(basectx, interval)
}
