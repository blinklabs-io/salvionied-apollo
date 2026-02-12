# Plutus V3 Data Structures

This document details how Plutus V3 scripts are represented in the transaction witness set in Apollo v2. All types come from gouroboros (`github.com/blinklabs-io/gouroboros`).

## Transaction Witness Set

The `ConwayTransactionWitnessSet` (from `gouroboros/ledger/conway`) contains fields for all Plutus script versions:

```go
// From gouroboros/ledger/conway
type ConwayTransactionWitnessSet struct {
    cbor.DecodeStoreCbor
    VkeyWitnesses      cbor.SetType[common.VkeyWitness]
    WsNativeScripts    cbor.SetType[common.NativeScript]
    BootstrapWitnesses cbor.SetType[common.BootstrapWitness]
    WsPlutusV1Scripts  cbor.SetType[common.PlutusV1Script]
    WsPlutusData       cbor.SetType[common.Datum]
    WsRedeemers        ConwayRedeemers
    WsPlutusV2Scripts  cbor.SetType[common.PlutusV2Script]
    WsPlutusV3Scripts  cbor.SetType[common.PlutusV3Script]
}
```

- `WsPlutusV3Scripts`: Holds Plutus V3 scripts attached to the transaction via `AttachScript`.

## How Apollo Populates the Witness Set

When `Complete()` is called, Apollo builds the witness set from its internal state:

```go
// Internal to Apollo - shown for illustration
func (a *Apollo) buildWitnessSet() conway.ConwayTransactionWitnessSet {
    ws := conway.ConwayTransactionWitnessSet{}
    if len(a.v3scripts) > 0 {
        ws.WsPlutusV3Scripts = cbor.NewSetType(a.v3scripts, true)
    }
    // ... plus V1/V2 scripts, datums, redeemers, native scripts
    return ws
}
```

Scripts are stored internally by version but attached through the unified `AttachScript` API, which detects the version automatically.

## Script Types

All script types implement `common.Script`:

| Type | Go Type | ScriptRef Type |
|------|---------|---------------|
| Native Script | `common.NativeScript` | 0 |
| Plutus V1 | `common.PlutusV1Script` | 1 |
| Plutus V2 | `common.PlutusV2Script` | 2 |
| Plutus V3 | `common.PlutusV3Script` | 3 |

## Redeemers

Redeemers in v2 use the gouroboros key-value format:

```go
// From gouroboros/ledger/common
type RedeemerKey struct {
    Tag   RedeemerTag
    Index uint32
}

type RedeemerValue struct {
    Data    Datum
    ExUnits ExUnits
}
```

Redeemer tags: `RedeemerTagSpend`, `RedeemerTagMint`, `RedeemerTagCert`, `RedeemerTagReward`.

## Datum Types

Datums use `common.Datum` (alias for `common.PlutusData`). They can be attached inline to outputs or as hashes:

```go
// Inline datum on output
a.PayToContract(scriptAddr, &datum, 5_000_000)

// Datum hash on output (datum added to witness set)
a.PayToContractWithDatumHash(scriptAddr, &datum, 5_000_000)

// Add datum directly to witness set
a.AddDatum(&datum)
```
