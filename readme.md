<div align="center">
    <img src="./assets/logo.jpg" alt="apollo logo" width="480">
</div>

# Apollo: Pure Golang Cardano Building Blocks
## Pure Golang Cardano Serialization

Apollo is a Go library for building Cardano transactions with native ledger
types from Blink Labs packages.

## Quickstart

This example builds, signs, serializes, and submits a simple ADA payment. Replace
the mnemonic, receiver address, and Blockfrost project ID with real values before
running it.

```go
package main

import (
    "context"
    "encoding/hex"
    "fmt"

    "github.com/blinklabs-io/gouroboros/ledger/common"

    apollo "github.com/blinklabs-io/apollo/v2"
    "github.com/blinklabs-io/apollo/v2/backend/blockfrost"
)

func main() {
    ctx := context.Background()

    // ChainContext supplies protocol parameters, UTxOs, evaluation, and submit.
    bfc := blockfrost.NewBlockFrostChainContext(
        "https://cardano-mainnet.blockfrost.io/api/v0",
        1,
        "mainnet0123456789abcdef0123456789abcdef",
    )

    mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
    a := apollo.New(bfc)
    a, err := a.SetWalletFromMnemonic(mnemonic)
    if err != nil {
        panic(err)
    }

    // Query spendable UTxOs for the wallet address.
    utxos, err := bfc.Utxos(ctx, a.GetWallet().Address())
    if err != nil {
        panic(err)
    }

    receiver, err := common.NewAddress("addr1vxp5xez7t3dvr4s8fr3m4yqdhury9m35g3c9d53j8s5tncgphe3d9")
    if err != nil {
        panic(err)
    }

    // AddLoadedUTxOs provides candidate inputs; PayToAddress adds an output.
    // CompleteContext selects inputs, balances change, calculates fees, and builds the tx.
    a, err = a.AddLoadedUTxOs(utxos...).
        PayToAddress(receiver, 1_000_000).
        CompleteContext(ctx)
    if err != nil {
        panic(err)
    }

    // Sign adds the wallet's verification-key witness.
    a, err = a.Sign()
    if err != nil {
        panic(err)
    }

    // CBOR is the binary transaction format submitted to the network.
    txCbor, err := a.GetTxCbor()
    if err != nil {
        panic(err)
    }
    fmt.Println(hex.EncodeToString(txCbor))

    // SubmitContext sends the signed transaction through the ChainContext backend.
    txId, err := a.SubmitContext(ctx)
    if err != nil {
        panic(err)
    }
    fmt.Println(hex.EncodeToString(txId.Bytes()))
}
```

More examples and feature docs are in [docs/README.md](docs/README.md). If you
are migrating from Apollo v1, start with
[docs/v2_migration/MIGRATION.md](docs/v2_migration/MIGRATION.md).

## Coin Selection

Apollo selects transaction inputs with **MACS** (Multi-Asset Coin Selection,
[IEEE Blockchain 2023](https://doi.org/10.1109/Blockchain60715.2023.00029)) by
default. MACS prioritizes UTxOs by value and closeness to the pool's average,
covering each asset in the target directly. Compared to the legacy
largest-first strategy it selects far fewer inputs on multi-asset targets
(15 vs 785 in our 1k-UTxO benchmark), produces much smaller change, and sweeps
dust UTxOs so they don't accumulate in your wallet.

The algorithm is pluggable via the `CoinSelector` interface:

```go
// Default: MACS with dust sweeping (UTxOs under 1 ADA, max 2 per tx)
a := apollo.New(bfc)

// Legacy largest-first behavior
a = a.SetCoinSelector(&apollo.LargestFirstSelector{})

// MACS without dust sweeping, or with custom limits
a = a.SetCoinSelector(&apollo.MACSSelector{})
a = a.SetCoinSelector(&apollo.MACSSelector{DustThreshold: 2_000_000, MaxDustInputs: 4})
```

Benchmarks live in `coinselection_bench_test.go`
(`go test -bench BenchmarkCoinSelection`), and the design notes with full
results are in `docs/design/2026-06-11-macs-coin-selection-design.md`.

If you have any questions or requests feel free to drop into this discord and ask :) https://discord.gg/MH4CmJcg49

By:
    `Edoardo Salvioni - Zhaata` 
