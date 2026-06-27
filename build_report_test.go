package apollo

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
	"github.com/blinklabs-io/gouroboros/ledger/mary"
	"github.com/blinklabs-io/gouroboros/ledger/shelley"
	plutigoData "github.com/blinklabs-io/plutigo/data"
)

func TestExplainRequiresBuildReport(t *testing.T) {
	cc := setupFixedContext()
	if _, err := New(cc).Explain(); err == nil {
		t.Fatal("expected Explain to fail before Complete")
	}

	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cc).
		SetWallet(NewExternalWallet(addr)).
		AddPayment(p).
		SetTtl(50000000).
		Complete()
	if err != nil {
		t.Fatal(err)
	}
	txCbor, err := a.GetTxCbor()
	if err != nil {
		t.Fatal(err)
	}

	loaded := New(cc)
	if _, err := loaded.LoadTxCbor(hex.EncodeToString(txCbor)); err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.Explain(); err == nil {
		t.Fatal("expected Explain to fail for a transaction loaded from CBOR")
	}
}

func TestBuildReportSimpleTransfer(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cc).
		SetWallet(NewExternalWallet(addr)).
		AddPayment(p).
		SetFeePadding(5_000).
		SetTtl(50000000).
		Complete()
	if err != nil {
		t.Fatal(err)
	}

	report, err := a.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if report.TxHash == "" {
		t.Fatal("expected transaction hash")
	}
	if report.TxSize == 0 {
		t.Fatal("expected transaction size")
	}
	if report.CoinSelector != defaultCoinSelector.Name() {
		t.Fatalf("expected default coin selector %q, got %q", defaultCoinSelector.Name(), report.CoinSelector)
	}
	if report.Fees.Final != int64(a.GetTx().Body.TxFee) { //nolint:gosec // test fee is small and produced by Complete
		t.Fatalf("expected final fee %d, got %d", a.GetTx().Body.TxFee, report.Fees.Final)
	}
	if report.Fees.Padding != 5_000 {
		t.Fatalf("expected fee padding 5000, got %d", report.Fees.Padding)
	}
	if report.Inputs.AvailableUtxoCount != 1 {
		t.Fatalf("expected 1 available UTxO, got %d", report.Inputs.AvailableUtxoCount)
	}
	if len(report.Inputs.Preselected) != 0 {
		t.Fatalf("expected no preselected inputs, got %d", len(report.Inputs.Preselected))
	}
	if len(report.Inputs.Selected) != 1 {
		t.Fatalf("expected 1 selected input, got %d", len(report.Inputs.Selected))
	}
	if len(report.Inputs.Final) != 1 {
		t.Fatalf("expected 1 final input, got %d", len(report.Inputs.Final))
	}
	if report.Balance.TotalInput.Coin != 10_000_000 {
		t.Fatalf("expected total input 10000000, got %d", report.Balance.TotalInput.Coin)
	}
	if len(report.Outputs.Requested) != 1 {
		t.Fatalf("expected 1 requested output, got %d", len(report.Outputs.Requested))
	}
	if len(report.Outputs.Final) < 2 {
		t.Fatalf("expected payment and change outputs, got %d", len(report.Outputs.Final))
	}
	if report.Outputs.ChangeIndex < 0 {
		t.Fatal("expected a change output")
	}
	if !report.Outputs.Final[report.Outputs.ChangeIndex].IsChange {
		t.Fatal("expected change output to be marked")
	}
	if report.Outputs.Change.Coin == 0 {
		t.Fatal("expected non-zero change")
	}
	if report.Balance.Change.Coin != report.Outputs.Change.Coin {
		t.Fatalf("expected balance change %d, got %d", report.Outputs.Change.Coin, report.Balance.Change.Coin)
	}

	report.Inputs.Final[0].Value.Coin = 1
	report.Outputs.Final[0].Value.Coin = 1
	reportAgain, err := a.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if reportAgain.Inputs.Final[0].Value.Coin == 1 {
		t.Fatal("Explain returned a mutable input report")
	}
	if reportAgain.Outputs.Final[0].Value.Coin == 1 {
		t.Fatal("Explain returned a mutable output report")
	}

	clone := a.Clone()
	cloneReport, err := clone.Explain()
	if err != nil {
		t.Fatal(err)
	}
	cloneReport.Outputs.Change.Coin = 1
	if cloneReport.Outputs.Change.Coin == report.Outputs.Change.Coin {
		t.Fatal("test mutation did not change clone report copy")
	}
	clone.buildReport.Outputs.Change.Coin = 1
	originalReport, err := a.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if originalReport.Outputs.Change.Coin == 1 {
		t.Fatal("Clone shared build report state with original")
	}
}

func TestBuildReportPreselectedAndSelectedInputs(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	var preselectedHash common.Blake2b256
	preselectedHash[0] = 0x01
	var selectedHash common.Blake2b256
	selectedHash[0] = 0x02
	preselected := makeTestUtxo(t, preselectedHash, 0, 3_000_000)
	selected := makeTestUtxo(t, selectedHash, 0, 10_000_000)

	p, err := NewPayment(validTestAddrBech32, 8_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cc).
		SetWallet(NewExternalWallet(addr)).
		AddInput(preselected).
		AddLoadedUTxOs(selected).
		AddPayment(p).
		SetTtl(50000000).
		Complete()
	if err != nil {
		t.Fatal(err)
	}

	report, err := a.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Inputs.Preselected) != 1 {
		t.Fatalf("expected 1 preselected input, got %d", len(report.Inputs.Preselected))
	}
	if report.Inputs.Preselected[0].Ref != utxoRef(preselected) {
		t.Fatalf("expected preselected ref %s, got %s", utxoRef(preselected), report.Inputs.Preselected[0].Ref)
	}
	if len(report.Inputs.Selected) != 1 {
		t.Fatalf("expected 1 selected input, got %d", len(report.Inputs.Selected))
	}
	if report.Inputs.Selected[0].Ref != utxoRef(selected) {
		t.Fatalf("expected selected ref %s, got %s", utxoRef(selected), report.Inputs.Selected[0].Ref)
	}
	if len(report.Inputs.Final) != 2 {
		t.Fatalf("expected 2 final inputs, got %d", len(report.Inputs.Final))
	}
	if report.Inputs.SelectionTarget.Coin == 0 {
		t.Fatal("expected non-zero selection target")
	}
}

func TestBuildReportMinUtxoIncrease(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	p, err := NewPayment(validTestAddrBech32, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cc).
		SetWallet(NewExternalWallet(addr)).
		AddPayment(p).
		SetTtl(50000000).
		Complete()
	if err != nil {
		t.Fatal(err)
	}

	report, err := a.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Outputs.Requested) != 1 || len(report.Outputs.Base) != 1 {
		t.Fatalf("expected one requested/base output, got %d/%d", len(report.Outputs.Requested), len(report.Outputs.Base))
	}
	if report.Outputs.Requested[0].Value.Coin != 1 {
		t.Fatalf("expected requested output to preserve original lovelace, got %d", report.Outputs.Requested[0].Value.Coin)
	}
	if report.Outputs.Requested[0].MinUtxoIncrease == 0 {
		t.Fatal("expected requested output to report a min-UTxO shortfall")
	}
	if report.Outputs.Base[0].Value.Coin <= report.Outputs.Requested[0].Value.Coin {
		t.Fatalf("expected base output lovelace to increase, requested=%d base=%d", report.Outputs.Requested[0].Value.Coin, report.Outputs.Base[0].Value.Coin)
	}
	if report.Outputs.Base[0].MinUtxoIncrease == 0 {
		t.Fatal("expected base output to report the applied min-UTxO increase")
	}
}

func TestBuildReportScriptCollateralAndRedeemer(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 30_000_000, 0x01, 0)
	addTestUtxo(cc, addr, 20_000_000, 0x02, 0)

	policy := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
	datum := common.Datum{Data: plutigoData.NewInteger(big.NewInt(1))}
	exUnits := common.ExUnits{Memory: 1_000, Steps: 2_000}
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cc).
		SetWallet(NewExternalWallet(addr)).
		AttachScript(common.PlutusV2Script([]byte{0x01, 0x02})).
		DisableExecutionUnitsEstimation().
		Mint(NewUnit(policy, "746f6b656e", 1), &datum, &exUnits).
		AddPayment(p).
		SetTtl(50000000).
		Complete()
	if err != nil {
		t.Fatal(err)
	}

	report, err := a.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if report.Scripts.PlutusV2 != 1 || len(report.Scripts.ScriptHashes) != 1 {
		t.Fatalf("expected one attached PlutusV2 script, got count=%d hashes=%d", report.Scripts.PlutusV2, len(report.Scripts.ScriptHashes))
	}
	if len(report.Collateral.Inputs) != 1 {
		t.Fatalf("expected one collateral input, got %d", len(report.Collateral.Inputs))
	}
	if !report.Collateral.AutoSelected {
		t.Fatal("expected auto-selected collateral")
	}
	if report.Collateral.Required == 0 || report.Collateral.Total < report.Collateral.Required {
		t.Fatalf("expected collateral total %d to cover required %d", report.Collateral.Total, report.Collateral.Required)
	}
	if report.Collateral.Return == nil {
		t.Fatal("expected collateral return for oversized collateral")
	}
	if len(report.Redeemers) != 1 {
		t.Fatalf("expected one redeemer, got %d", len(report.Redeemers))
	}
	if report.Redeemers[0].Tag != common.RedeemerTagMint || report.Redeemers[0].Source != policy {
		t.Fatalf("unexpected redeemer mapping: %+v", report.Redeemers[0])
	}
	if report.Redeemers[0].ExUnits != exUnits {
		t.Fatalf("expected redeemer ExUnits %+v, got %+v", exUnits, report.Redeemers[0].ExUnits)
	}
	if report.Fees.ExecutionUnits != exUnits || report.Fees.ExecutionUnitFee == 0 {
		t.Fatalf("expected execution-unit fee details, got units=%+v fee=%d", report.Fees.ExecutionUnits, report.Fees.ExecutionUnitFee)
	}
	if !report.Balance.Mint.HasAssets() {
		t.Fatal("expected mint balance component to include assets")
	}
}

func TestBuildReportReferenceScriptFeeDetails(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 5_000_000, 0x01, 0)
	addTestUtxo(cc, addr, 5_000_000, 0x02, 0)

	hashHex := "eeff000000000000000000000000000000000000000000000000000000000000"
	hashBytes, err := hex.DecodeString(hashHex)
	if err != nil {
		t.Fatal(err)
	}
	var refTxHash common.Blake2b256
	copy(refTxHash[:], hashBytes)
	cc.AddUtxoByRef(common.Utxo{
		Id: shelley.ShelleyTransactionInput{TxId: refTxHash, OutputIndex: 0},
		Output: &babbage.BabbageTransactionOutput{
			OutputAddress: addr,
			OutputAmount:  mary.MaryTransactionOutputValue{Amount: 2_000_000},
			TxOutScriptRef: &common.ScriptRef{
				Type:   common.ScriptRefTypePlutusV2,
				Script: common.PlutusV2Script(bytes.Repeat([]byte{0x42}, 60_000)),
			},
		},
	})

	p, err := NewPayment(validTestAddrBech32, 4_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(NewExternalWallet(addr)).
		AddPayment(p)
	a, err = a.AddReferenceInput(hashHex, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Complete(); err != nil {
		t.Fatal(err)
	}

	report, err := a.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if report.Fees.ReferenceScriptBytes != 60_000 {
		t.Fatalf("expected 60000 reference-script bytes, got %d", report.Fees.ReferenceScriptBytes)
	}
	if report.Fees.ReferenceScriptFee == 0 || report.Fees.ReferenceScriptReserve == 0 {
		t.Fatalf("expected reference-script fee details, got fee=%d reserve=%d", report.Fees.ReferenceScriptFee, report.Fees.ReferenceScriptReserve)
	}
	if report.Fees.Size == 0 {
		t.Fatal("expected size fee component")
	}
	if len(report.Scripts.ReferenceInputRefs) != 1 || report.Scripts.ReferenceInputRefs[0] != hashHex+"#0" {
		t.Fatalf("unexpected reference input refs: %v", report.Scripts.ReferenceInputRefs)
	}
}
