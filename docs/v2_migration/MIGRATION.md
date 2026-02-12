# Apollo v1 to v2 Migration Guide

Apollo v2 replaces all custom serialization code with [gouroboros](https://github.com/blinklabs-io/gouroboros) types, reducing the codebase from ~13,000+ lines to ~3,500. The API has been simplified from ~90 public methods to ~55.

## Import Path

```go
// v1
import "github.com/Salvionied/apollo"

// v2
import "github.com/Salvionied/apollo/v2"
```

## Quick Reference: Removed Methods and Replacements

| v1 Method | v2 Replacement |
|-----------|---------------|
| `AttachV1Script(script)` | `AttachScript(script)` |
| `AttachV2Script(script)` | `AttachScript(script)` |
| `AttachV3Script(script)` | `AttachScript(script)` |
| `AttachDatum(datum)` | `AddDatum(datum)` |
| `AddPlutusV1Script(script)` | `AttachScript(script)` |
| `AddPlutusV2Script(script)` | `AttachScript(script)` |
| `AddPlutusV3Script(script)` | `AttachScript(script)` |
| `AddNativeScript(script)` | `AttachScript(script)` |
| `AddMint(units...)` | `Mint(unit, nil, nil)` |
| `MintAssetsWithRedeemer(unit, redeemer, exUnits)` | `Mint(unit, &redeemer, &exUnits)` |
| `PayToAddressBech32(addr, ...)` | Parse address first, then `PayToAddress(addr, ...)` |
| `AddInputAddressFromBech32(addr)` | Parse address first, then `AddInputAddress(addr)` |
| `SetWalletFromBech32(addr)` | Parse address first, then `SetWallet(NewExternalWallet(addr))` |
| `SetWalletFromKeypair(vkey, skey, net)` | Build address manually, then `SetWallet(NewExternalWallet(addr))` |
| `SetChangeAddressBech32(addr)` | Parse address first, then `SetChangeAddress(addr)` |
| `SetWalletAsChangeAddress()` | Not needed — wallet address is the default change address |
| `AddRequiredSignerFromBech32(addr)` | Parse address, then use `AddRequiredSignerPaymentKey(addr)` or `AddRequiredSignerStakeKey(addr)` |
| `AddRequiredSignerFromAddress(addr, payment, staking)` | Use `AddRequiredSignerPaymentKey(addr)` and/or `AddRequiredSignerStakeKey(addr)` |
| `AddReferenceInputV3(hash, idx)` | `AddReferenceInput(hash, idx)` |
| `PayToAddressWithV1ReferenceScript(...)` | `PayToAddressWithReferenceScript(addr, lovelace, script, units...)` |
| `PayToAddressWithV2ReferenceScript(...)` | `PayToAddressWithReferenceScript(addr, lovelace, script, units...)` |
| `PayToAddressWithV3ReferenceScript(...)` | `PayToAddressWithReferenceScript(addr, lovelace, script, units...)` |
| `PayToContractWithV1ReferenceScript(...)` | Combine `PayToContract` + `ScriptRef` on Payment |
| `PayToContractWithV2ReferenceScript(...)` | Combine `PayToContract` + `ScriptRef` on Payment |
| `PayToContractWithV3ReferenceScript(...)` | Combine `PayToContract` + `ScriptRef` on Payment |
| `CompleteExact(fee)` | `SetFee(fee)` then `Complete()` |
| `SetEstimateRequired()` | Automatic — set by `CollectFrom` and `Mint` with redeemers |
| `ConsumeAssetsFromUtxo(utxo, payments...)` | `AddInput(utxo)` then `AddPayment(payments...)` |
| `GetPaymentsLength()` | Removed — internal detail |
| `GetRedeemers()` | Removed — leaked private type |
| `UpdateRedeemers(r)` | Removed — leaked private type |
| `GetSortedInputs()` | Removed — internal detail |
| `RegisterStakeFromAddress(addr)` | `RegisterStake(addr)` |
| `RegisterStakeFromBech32(bech32)` | `RegisterStake(bech32)` |
| `DeregisterStakeFromAddress(addr)` | `DeregisterStake(addr)` |
| `DeregisterStakeFromBech32(bech32)` | `DeregisterStake(bech32)` |
| `DelegateStakeFromAddress(addr, pool)` | `DelegateStake(addr, pool)` |
| `DelegateStakeFromBech32(bech32, pool)` | `DelegateStake(bech32, pool)` |
| `RegisterAndDelegateStakeFromAddress(...)` | `RegisterAndDelegateStake(addr, pool, coin)` |
| `RegisterAndDelegateStakeFromBech32(...)` | `RegisterAndDelegateStake(bech32, pool, coin)` |
| `DelegateVoteFromAddress(addr, drep)` | `DelegateVote(addr, drep)` |
| `DelegateVoteFromBech32(bech32, drep)` | `DelegateVote(bech32, drep)` |
| `DelegateStakeAndVoteFromAddress(...)` | `DelegateStakeAndVote(addr, pool, drep)` |
| `DelegateStakeAndVoteFromBech32(...)` | `DelegateStakeAndVote(bech32, pool, drep)` |
| `RegisterAndDelegateVoteFromAddress(...)` | `RegisterAndDelegateVote(addr, drep, coin)` |
| `RegisterAndDelegateVoteFromBech32(...)` | `RegisterAndDelegateVote(bech32, drep, coin)` |
| `RegisterAndDelegateStakeAndVoteFromAddress(...)` | `RegisterAndDelegateStakeAndVote(addr, pool, drep, coin)` |
| `RegisterAndDelegateStakeAndVoteFromBech32(...)` | `RegisterAndDelegateStakeAndVote(bech32, pool, drep, coin)` |
| `NewV1ScriptRef(script)` | `NewScriptRef(script)` |
| `NewV2ScriptRef(script)` | `NewScriptRef(script)` |
| `NewV3ScriptRef(script)` | `NewScriptRef(script)` |

## Dependencies

Apollo v2 uses [bursa](https://github.com/blinklabs-io/bursa) v0.15.0 for HD wallet key derivation and transaction signing. Bursa provides:

- **BIP32-Ed25519 key derivation** following CIP-1852 (payment, stake, DRep, committee, and calidus keys)
- **Native `XPrv.Sign()`** for correct CIP-1852 extended Ed25519 signatures
- **Mnemonic generation** via `GenerateMnemonic()`

Wallet creation uses bursa internally — see `SetWalletFromMnemonic` and `NewBursaWallet`.

## Detailed Changes

### 1. Script Attachment (12+ methods → 1)

v2 uses a single `AttachScript` method that handles all script types — PlutusV1, PlutusV2, PlutusV3, and NativeScript. Duplicate scripts are ignored automatically.

```go
// v1
a.AttachV1Script(v1Script)
a.AttachV2Script(v2Script)
a.AttachV3Script(v3Script)
a.AddNativeScript(nativeScript)

// v2 - single method, auto-detects type
a.AttachScript(v1Script)
a.AttachScript(v2Script)
a.AttachScript(v3Script)
a.AttachScript(nativeScript)
```

`AttachDatum` is removed. Use `AddDatum` instead:

```go
// v1
a.AttachDatum(datum)

// v2
a.AddDatum(datum)
```

### 2. Script References (6 constructors → 1)

```go
// v1
ref := apollo.NewV1ScriptRef(v1Script)
ref := apollo.NewV2ScriptRef(v2Script)
ref := apollo.NewV3ScriptRef(v3Script)

// v2 - auto-detects type
ref := apollo.NewScriptRef(script)
```

### 3. Minting (2 methods → 1)

`AddMint` and `MintAssetsWithRedeemer` are replaced by a single `Mint` method. Pass nil for redeemer/exUnits for native minting:

```go
// v1 - native mint
a.AddMint(unit1, unit2)

// v2 - native mint (one unit at a time, nil redeemer)
a.Mint(unit1, nil, nil)
a.Mint(unit2, nil, nil)

// v1 - script mint
a.MintAssetsWithRedeemer(unit, redeemer, exUnits)

// v2 - script mint (pass pointers)
a.Mint(unit, &redeemer, &exUnits)
```

### 4. PayToContract Signature Change

The `isInline` boolean parameter is removed. Use the method name to choose behavior:

```go
// v1 - inline datum
a.PayToContract(addr, datum, lovelace, true, units...)

// v2 - inline datum (default)
a.PayToContract(addr, datum, lovelace, units...)

// v1 - datum hash
a.PayToContract(addr, datum, lovelace, false, units...)

// v2 - datum hash
a.PayToContractWithDatumHash(addr, datum, lovelace, units...)
```

### 5. Reference Script Payments (6 methods → 1)

```go
// v1
a.PayToAddressWithV1ReferenceScript(addr, lovelace, v1Script, units...)
a.PayToAddressWithV2ReferenceScript(addr, lovelace, v2Script, units...)
a.PayToAddressWithV3ReferenceScript(addr, lovelace, v3Script, units...)

// v2 - auto-detects type
a.PayToAddressWithReferenceScript(addr, lovelace, script, units...)
```

### 6. Bech32 Convenience Methods (Removed)

All `*Bech32` convenience methods are removed. Parse the address yourself:

```go
// v1
a.PayToAddressBech32("addr1...", 2_000_000, nil)
a.AddInputAddressFromBech32("addr1...")
a.SetWalletFromBech32("addr1...")
a.SetChangeAddressBech32("addr1...")

// v2
addr, err := common.NewAddress("addr1...")
a.PayToAddress(addr, 2_000_000)
a.AddInputAddress(addr)
a.SetWallet(apollo.NewExternalWallet(addr))
a.SetChangeAddress(addr)
```

### 7. Required Signers (Boolean flags → Named methods)

```go
// v1 - boolean flags for payment/staking
a.AddRequiredSignerFromAddress(addr, true, false)   // payment only
a.AddRequiredSignerFromAddress(addr, false, true)    // staking only
a.AddRequiredSignerFromAddress(addr, true, true)     // both

// v2 - explicit named methods
a.AddRequiredSignerPaymentKey(addr)  // payment only
a.AddRequiredSignerStakeKey(addr)    // staking only
a.AddRequiredSignerPaymentKey(addr).AddRequiredSignerStakeKey(addr) // both
```

### 8. Reference Inputs (Unified)

`AddReferenceInputV3` is removed. All reference inputs use the same method:

```go
// v1
a.AddReferenceInput(txHash, idx)    // for V1/V2
a.AddReferenceInputV3(txHash, idx)  // for V3

// v2
a, err = a.AddReferenceInput(txHash, idx)    // for all script versions
```

### 9. Staking & Delegation (24 methods → 8)

All `FromAddress` and `FromBech32` variants are eliminated. The base methods now accept flexible input types via `any`:

- `*common.Credential` — direct credential
- `common.Credential` — direct credential (value)
- `common.Address` — extracts staking credential from address
- `string` — parses as bech32, then extracts staking credential
- `nil` — uses wallet address

```go
// v1 - three variants per operation
a.RegisterStake(&cred)
a.RegisterStakeFromAddress(addr)
a.RegisterStakeFromBech32("addr1...")

// v2 - single method, multiple input types
a.RegisterStake(&cred)       // credential pointer
a.RegisterStake(addr)        // common.Address
a.RegisterStake("addr1...")  // bech32 string
a.RegisterStake(nil)         // wallet fallback
```

This pattern applies to all 8 staking/delegation methods:
- `RegisterStake(credOrAddr)`
- `DeregisterStake(credOrAddr)`
- `DelegateStake(credOrAddr, poolHash)`
- `RegisterAndDelegateStake(credOrAddr, poolHash, coin)`
- `DelegateVote(credOrAddr, drep)`
- `DelegateStakeAndVote(credOrAddr, poolHash, drep)`
- `RegisterAndDelegateVote(credOrAddr, drep, coin)`
- `RegisterAndDelegateStakeAndVote(credOrAddr, poolHash, drep, coin)`

**Note**: All staking/delegation methods now return `(*Apollo, error)` instead of `*Apollo`.

### 10. Removed Methods (No Direct Replacement Needed)

| Method | Reason |
|--------|--------|
| `CompleteExact(fee)` | Use `SetFee(fee)` then `Complete()` |
| `SetWalletAsChangeAddress()` | Default behavior — wallet is always the change address |
| `SetWalletFromKeypair(...)` | Incomplete implementation — build address manually |
| `SetEstimateRequired()` | Internal — automatically set by `CollectFrom`/`Mint` |
| `ConsumeAssetsFromUtxo(...)` | Use `AddInput(utxo)` then `AddPayment(...)` |
| `GetPaymentsLength()` | Internal detail |
| `GetRedeemers()` | Leaked private type |
| `UpdateRedeemers(r)` | Leaked private type |
| `GetSortedInputs()` | Internal detail |

### 11. Type Changes

| v1 Type | v2 Type |
|---------|---------|
| `serialization.TransactionBody` | `conway.ConwayTransactionBody` |
| `serialization.Transaction` | `conway.ConwayTransaction` |
| `serialization.TransactionOutput` | `babbage.BabbageTransactionOutput` |
| `serialization.MultiAsset` | `common.MultiAsset[T]` |
| `serialization.PlutusData` | `common.Datum` |
| `serialization.Redeemer` | `common.RedeemerKey` + `common.RedeemerValue` |
| `serialization.NativeScript` | `common.NativeScript` |
| `PlutusData` (custom) | `common.PlutusData` / `common.Datum` |

### 12. Backend / Chain Context

The `ChainContext` interface is now in `backend` package with gouroboros types:

```go
// v1
import "github.com/Salvionied/apollo/txBuilding/Backend/Base"

// v2
import "github.com/Salvionied/apollo/v2/backend"
```

Supported backends: `blockfrost`, `ogmios`, `maestro`, `utxorpc`, `fixed` (testing).

### 13. Value Type

v2 introduces an explicit `Value` type replacing various ad-hoc representations:

```go
v := apollo.NewSimpleValue(2_000_000)           // ADA only
v := apollo.NewValue(2_000_000, multiAsset)     // ADA + assets
result, err := v.Add(other)                     // returns error on overflow
v, err = v.Sub(other)
ok := v.GreaterOrEqual(other)
```

**Note**: `Value.Add` returns `(Value, error)` to detect uint64 overflow.

### 14. Type Safety Improvements

**`int64` for monetary amounts**: `Unit.Quantity` and `Payment.Lovelace` are now `int64` (previously `int`) to ensure consistent 64-bit precision across all platforms.

**Error-returning interfaces**: `PaymentI.ToValue()` and `PaymentI.ToTxOut()` now return errors instead of silently swallowing them:

```go
// v1
type PaymentI interface {
    ToValue() Value
    ToTxOut() *babbage.BabbageTransactionOutput
}

// v2
type PaymentI interface {
    ToValue() (Value, error)
    ToTxOut() (*babbage.BabbageTransactionOutput, error)
}
```

**`ParseFraction` returns errors**: `backend.ParseFraction` now returns `(float32, error)` instead of silently returning 0 on invalid input.

### 15. Wallet Passphrase Support

`NewBursaWallet` keeps the simple signature for common use. Use `NewBursaWalletWithPassphrase` for BIP39 passphrase:

```go
// No passphrase (most common)
w, err := apollo.NewBursaWallet(mnemonic)

// With passphrase
w, err := apollo.NewBursaWalletWithPassphrase(mnemonic, "my-secret")

// Via Apollo builder
a, err = a.SetWalletFromMnemonic(mnemonic)                        // no passphrase
a, err = a.SetWalletFromMnemonicWithPassphrase(mnemonic, "pass")  // with passphrase
```

## Complete v2 Public API

### Construction & Wallet
- `New(cc) *Apollo`
- `SetWallet(w) *Apollo`
- `SetWalletFromMnemonic(mnemonic) (*Apollo, error)`
- `SetWalletFromMnemonicWithPassphrase(mnemonic, passphrase) (*Apollo, error)`
- `GetWallet() Wallet`

### Payments
- `AddPayment(payment) *Apollo`
- `PayToAddress(addr, lovelace, units...) *Apollo`
- `PayToContract(addr, datum, lovelace, units...) *Apollo`
- `PayToContractWithDatumHash(addr, datum, lovelace, units...) *Apollo`
- `PayToAddressWithReferenceScript(addr, lovelace, script, units...) *Apollo`

### Inputs & UTxOs
- `AddInput(utxo) *Apollo`
- `AddLoadedUTxOs(utxos...) *Apollo`
- `AddInputAddress(addr) *Apollo`
- `CollectFrom(utxo, redeemer, exUnits) *Apollo`
- `ConsumeUTxO(utxo, payments...) (*Apollo, error)`
- `UtxoFromRef(hash, index) (*Utxo, error)`
- `GetUsedUTxOs() []string`

### Scripts & Minting
- `AttachScript(script) *Apollo`
- `AddDatum(datum) *Apollo`
- `Mint(unit, redeemer, exUnits) *Apollo`
- `GetBurns() (Value, error)`

### Reference Inputs
- `AddReferenceInput(txHash, index) (*Apollo, error)`

### Required Signers
- `AddRequiredSigner(pkh) *Apollo`
- `AddRequiredSignerPaymentKey(addr) *Apollo`
- `AddRequiredSignerStakeKey(addr) *Apollo`

### Transaction Parameters
- `SetTtl(ttl) *Apollo`
- `SetValidityStart(start) *Apollo`
- `SetFee(fee) *Apollo`
- `SetFeePadding(padding) *Apollo`
- `SetChangeAddress(addr) *Apollo`
- `SetCollateralAmount(amount) *Apollo`
- `AddCollateral(utxo) *Apollo`
- `DisableExecutionUnitsEstimation() *Apollo`

### Staking & Delegation
- `RegisterStake(credOrAddr) (*Apollo, error)`
- `DeregisterStake(credOrAddr) (*Apollo, error)`
- `DelegateStake(credOrAddr, poolHash) (*Apollo, error)`
- `RegisterAndDelegateStake(credOrAddr, poolHash, coin) (*Apollo, error)`
- `DelegateVote(credOrAddr, drep) (*Apollo, error)`
- `DelegateStakeAndVote(credOrAddr, poolHash, drep) (*Apollo, error)`
- `RegisterAndDelegateVote(credOrAddr, drep, coin) (*Apollo, error)`
- `RegisterAndDelegateStakeAndVote(credOrAddr, poolHash, drep, coin) (*Apollo, error)`
- `RegisterPool(params) *Apollo`
- `DeregisterPool(poolHash, epoch) *Apollo`
- `SetCertificates(certs) *Apollo`
- `GetStakeCredentialFromWallet() (Credential, error)`

### Withdrawals & Metadata
- `AddWithdrawal(address, amount, redeemer, exUnits) *Apollo`
- `SetShelleyMetadata(metadata) *Apollo`

### Building & Signing
- `Complete() (*Apollo, error)`
- `Sign() (*Apollo, error)`
- `SignWithSkey(vkey, skey) (*Apollo, error)`
- `AddVerificationKeyWitness(witness) (*Apollo, error)`
- `Submit() (Blake2b256, error)`
- `GetTx() *ConwayTransaction`
- `GetTxCbor() ([]byte, error)`
- `LoadTxCbor(hex) (*Apollo, error)`
- `Clone() *Apollo`
