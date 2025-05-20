package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Wallet struct {
	client *ethclient.Client

	eip155  types.Signer
	prvkey  *ecdsa.PrivateKey
	address common.Address
	nonce   uint64
}

func NewWallet(basectx context.Context, prvkey, rpc string) (*Wallet, error) {
	newctx, cancel := context.WithTimeout(basectx, time.Second*5)
	defer cancel()

	slog.Info("connecting", "rpc", rpc)
	client, err := ethclient.DialContext(newctx, rpc)
	if err != nil {
		return nil, err
	}

	chainId, err := client.ChainID(newctx)
	if err != nil {
		return nil, err
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(prvkey, "0x"))
	if err != nil {
		return nil, err
	}
	publicKey := privateKey.Public()

	wallet := &Wallet{
		client:  client,
		eip155:  types.NewEIP155Signer(chainId),
		address: crypto.PubkeyToAddress(*publicKey.(*ecdsa.PublicKey)),
		prvkey:  privateKey,
	}

	slog.Info("chain info", "address", wallet.address, "chainId", chainId)
	return wallet, nil
}

func (w *Wallet) Start(basectx context.Context, interval time.Duration) {
	ticker := time.NewTimer(0)
	defer ticker.Stop()

	for {
		select {
		case <-basectx.Done():
			return
		case <-ticker.C:
			if err := w.start(basectx, interval); err != nil {
				slog.Error("ticker", "err", err)
			}
			ticker.Reset(interval / 2)
		}
	}
}

func (w *Wallet) start(basectx context.Context, interval time.Duration) error {
	newctx, cancel := context.WithTimeout(basectx, time.Second*10)
	defer cancel()

	if interval != 0 {
		header, err := w.client.HeaderByNumber(newctx, nil)
		if err != nil {
			return err
		}

		blockTime := time.Unix(int64(header.Time), 0).UTC()
		if time.Since(blockTime) < interval {
			slog.Info("No need to send a tx", "block", header.Number, "time", blockTime)
			return nil
		}
	}

	nonce, err := w.client.NonceAt(newctx, w.address, nil)
	if err != nil {
		return err
	}

	gasPrice, err := w.client.SuggestGasPrice(newctx)
	if err != nil {
		return err
	}

	if gasPrice.BitLen() == 0 {
		return fmt.Errorf("gas price is 0")
	}

	if nonce == w.nonce && w.nonce > 0 {
		gasPrice.Add(gasPrice, big.NewInt(1e9))
	}

	var gasLimit uint64 = 200_000 + rand.Uint64()%1000

	balance, err := w.client.BalanceAt(newctx, w.address, nil)
	if err != nil {
		return err
	}

	if balance.Cmp(new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(gasLimit))) < 0 {
		return fmt.Errorf("No enough balance to send the tx")
	}

	w.send(
		basectx,
		types.MustSignNewTx(
			w.prvkey,
			w.eip155,
			&types.LegacyTx{
				Nonce:    nonce,
				GasPrice: gasPrice,
				Gas:      gasLimit,
				To:       &w.address,
				Value:    big.NewInt(0),
				Data:     nil,
			},
		),
	)

	w.nonce = nonce
	return nil
}

func (w *Wallet) send(basectx context.Context, tx *types.Transaction) {
	sendTx := func() {
		newctx, cancel := context.WithTimeout(basectx, time.Second*3)
		defer cancel()

		slog.Info("Resending")

		err := w.client.SendTransaction(newctx, tx)
		if err != nil {
			slog.Error("Failed to send Tx", "err", err)
		}
	}

	waitFor := func() *types.Receipt {
		newctx, cancel := context.WithTimeout(basectx, time.Second*3)
		defer cancel()

		slog.Info("Checking status")
		receipt, err := w.client.TransactionReceipt(newctx, tx.Hash())
		if err != nil && err != ethereum.NotFound {
			slog.Error("Failed to check tx", "err", err)
			return nil
		}
		return receipt
	}

	sendTicker := time.NewTimer(0)
	defer sendTicker.Stop()

	recTicker := time.NewTicker(time.Second * 3)
	defer recTicker.Stop()

	var start = time.Now()

	slog.Info("Sending", "tx", tx.Hash())
	for {
		select {
		case <-basectx.Done():
			return
		case <-sendTicker.C:
			sendTx()
			sendTicker.Reset(time.Minute)
		case <-recTicker.C:
			duration := time.Since(start)
			if duration > time.Minute*2 {
				slog.Warn("Discard due to timeout")
				return
			}
			if receipt := waitFor(); receipt != nil {
				slog.Info("Confirmed", "duration", duration.String(), "height", receipt.BlockNumber)
				return
			}
		}
	}
}
