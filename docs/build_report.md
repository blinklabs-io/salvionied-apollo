# Build Reports

`Complete` and `CompleteContext` store a diagnostic `BuildReport` for the
transaction they build. Call `Explain` after a successful build to inspect the
builder's decisions without decoding CBOR or reading private builder fields.

```go
a, err := apollo.New(chainContext).
    SetWallet(wallet).
    AddPayment(payment).
    CompleteContext(ctx)
if err != nil {
    return err
}

report, err := a.Explain()
if err != nil {
    return err
}

fmt.Println(report.Fees.Final)
fmt.Println(report.Inputs.Selected)
fmt.Println(report.Outputs.Change)
```

The report includes:

- preselected, coin-selected, and final ordered inputs;
- requested outputs, min-UTxO-adjusted base outputs, final outputs, and the
  change output;
- each output's computed min lovelace and any min-UTxO increase applied by the
  builder;
- final fee, size fee component, fee padding, reference-script byte/fee details,
  and execution-unit totals/fee;
- withdrawals, mints, burns, certificate deposits/refunds, and governance costs;
- collateral inputs, required collateral, total collateral, collateral return,
  auto-selection, and overlap details;
- redeemer tag/index mappings after canonical ordering;
- reference input refs, attached script hashes, and required signer key hashes.

`Explain` returns a deep copy of the stored report. Mutating the returned value
does not alter the builder. Transactions loaded with `LoadTxCbor` do not have a
build report because the original build decisions are not recoverable from CBOR.

`Explain` does not query the backend. Backend-dependent values such as protocol
parameters, reference-script sizes, collateral requirements, and min-UTxO values
are captured during `Complete` or `CompleteContext` while the transaction is
being built.
