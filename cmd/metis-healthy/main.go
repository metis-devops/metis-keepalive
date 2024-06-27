package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
)

/*
	{
	  "healthy": true,
	  "latest_block_number": "0xe90cc9",
	  "latest_block_timestamp": "2024-03-16T02:12:05"
	}
*/

type HealthyResponse struct {
	Healthy   bool        `json:"healthy"`
	Height    hexutil.Big `json:"latest_block_number"`
	Timestamp time.Time   `json:"latest_block_timestamp"`
}

func main() {
	var Endpoint string
	var Ticker time.Duration
	flag.StringVar(&Endpoint, "endpoint", "http://l2geth.metis.svc.cluster.local:8545", "endpoint to sequencer url")
	flag.DurationVar(&Ticker, "ticker", time.Second*5, "ticker")
	flag.Parse()

	basectx, cancel := signal.NotifyContext(
		context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	client, err := ethclient.Dial(Endpoint)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	var mutex sync.RWMutex
	var result *HealthyResponse

	refresh := func() {
		newctx, cancel := context.WithTimeout(basectx, time.Second*3)
		defer cancel()
		header, err := client.HeaderByNumber(newctx, nil)
		if err != nil {
			slog.Error("HeaderByNumber", "err", err)
			return
		}

		if header.Time == 0 {
			slog.Error("Ignore 0 timestamp block", "block", header.Number)
			return
		}

		mutex.Lock()
		defer mutex.Unlock()

		timestamp := time.Unix(int64(header.Time), 0).UTC()
		if result != nil && result.Height.ToInt().Cmp(header.Number) >= 0 {
			if time.Since(timestamp) > 5*time.Minute {
				result.Healthy = false
			}
			return
		}

		slog.Info("refreshing", "block", header.Number, "time", timestamp)
		result = &HealthyResponse{
			Healthy:   time.Since(timestamp) < 5*time.Minute,
			Height:    hexutil.Big(*header.Number),
			Timestamp: timestamp,
		}
	}

	refresh()

	go func() {
		ticker := time.NewTicker(Ticker)
		defer ticker.Stop()

		for {
			select {
			case <-basectx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		mutex.RLock()
		defer mutex.RUnlock()
		if result == nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "The API is not available now")
			return
		}

		w.Header().Set("content-type", "application/json")
		w.Header().Set("access-control-allow-origin", "*")
		_ = json.NewEncoder(w).Encode(result)
	})

	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{Addr: ":8080"}
	go func() {
		defer cancel()
		slog.Info("Start and serving...")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Failed to start server", "err", err)
			return
		}
	}()

	<-basectx.Done()
	slog.Info("stopping")
	_ = server.Shutdown(context.Background())
	slog.Info("stopped")
}
