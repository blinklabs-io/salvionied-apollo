package apollo

import (
	"testing"

	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
	"github.com/blinklabs-io/gouroboros/ledger/mary"
	"github.com/blinklabs-io/gouroboros/ledger/shelley"

	"github.com/Salvionied/apollo/v2/backend/fixed"
)

// validTestAddrBech32 is a valid bech32 test address with both payment and staking parts.
var validTestAddrBech32 = func() string {
	// Build a proper base address (type 0) with payment + staking key hashes
	var raw [57]byte
	raw[0] = 0x00 // type 0 = base address, network 0 = testnet
	raw[1] = 0xAA // payment key hash
	raw[29] = 0xBB // stake key hash
	addr, err := common.NewAddressFromBytes(raw[:])
	if err != nil {
		// Return empty string; testAddress(t) will fail with a clear message.
		return ""
	}
	return addr.String()
}()

func testAddress(t *testing.T) common.Address {
	t.Helper()
	addr, err := common.NewAddress(validTestAddrBech32)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func setupFixedContext() *fixed.FixedChainContext {
	return fixed.NewEmptyFixedChainContext()
}

func addTestUtxo(fc *fixed.FixedChainContext, addr common.Address, lovelace uint64, txHashByte byte, index uint32) {
	var txHash common.Blake2b256
	txHash[0] = txHashByte

	input := shelley.ShelleyTransactionInput{
		TxId:        txHash,
		OutputIndex: index,
	}
	output := babbage.BabbageTransactionOutput{
		OutputAddress: addr,
		OutputAmount: mary.MaryTransactionOutputValue{
			Amount: lovelace,
		},
	}
	utxo := common.Utxo{
		Id:     input,
		Output: &output,
	}
	fc.AddUtxo(addr, utxo)
}

func TestNewApollo(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	if a == nil {
		t.Fatal("expected non-nil Apollo")
	}
	if a.Context == nil {
		t.Fatal("expected non-nil context")
	}
}

func TestApolloChaining(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc).
		SetTtl(1000).
		SetValidityStart(500).
		SetFeePadding(10000)

	if a.Ttl != 1000 {
		t.Errorf("expected TTL 1000, got %d", a.Ttl)
	}
	if a.ValidityStart != 500 {
		t.Errorf("expected validity start 500, got %d", a.ValidityStart)
	}
	if a.FeePadding != 10000 {
		t.Errorf("expected fee padding 10000, got %d", a.FeePadding)
	}
}

func TestCompleteRequiresWallet(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	_, err := a.Complete()
	if err == nil {
		t.Error("expected error when wallet is not set")
	}
}

func TestSignRequiresTransaction(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	w := NewExternalWallet(testAddress(t))
	a = a.SetWallet(w)
	_, err := a.Sign()
	if err == nil {
		t.Error("expected error when no transaction built")
	}
}

func TestGetTxCborRequiresTransaction(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	_, err := a.GetTxCbor()
	if err == nil {
		t.Error("expected error when no transaction built")
	}
}

func TestAddPayment(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	p, err := NewPayment(validTestAddrBech32, 2000000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.AddPayment(p)
	if len(a.payments) != 1 {
		t.Errorf("expected 1 payment, got %d", len(a.payments))
	}
}

func TestAddLoadedUTxOs(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	var hash common.Blake2b256
	hash[0] = 1
	utxo := makeTestUtxo(t, hash, 0, 5_000_000)
	a.AddLoadedUTxOs(utxo)
	if len(a.utxos) != 1 {
		t.Errorf("expected 1 utxo, got %d", len(a.utxos))
	}
}

func TestAddInput(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	var hash common.Blake2b256
	hash[0] = 1
	utxo := makeTestUtxo(t, hash, 0, 5_000_000)
	a.AddInput(utxo)
	if len(a.preselectedUtxos) != 1 {
		t.Errorf("expected 1 preselected utxo, got %d", len(a.preselectedUtxos))
	}
}

func TestAddRequiredSigner(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	var pkh common.Blake2b224
	pkh[0] = 0xaa
	a.AddRequiredSigner(pkh)
	if len(a.requiredSigners) != 1 {
		t.Errorf("expected 1 required signer, got %d", len(a.requiredSigners))
	}
}

func TestAddReferenceInput(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	hashHex := "aabb000000000000000000000000000000000000000000000000000000000000"
	a, err := a.AddReferenceInput(hashHex, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.referenceInputs) != 1 {
		t.Errorf("expected 1 reference input, got %d", len(a.referenceInputs))
	}
}

func TestAddReferenceInputInvalidHex(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	_, err := a.AddReferenceInput("not-hex!", 0)
	if err == nil {
		t.Error("expected error for invalid hex")
	}
	if len(a.referenceInputs) != 0 {
		t.Error("expected no reference inputs for invalid hex")
	}
}

func TestAddReferenceInputNegativeIndex(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	hashHex := "aabb000000000000000000000000000000000000000000000000000000000000"
	_, err := a.AddReferenceInput(hashHex, -1)
	if err == nil {
		t.Error("expected error for negative index")
	}
}

func TestMintWithoutRedeemer(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	u := NewUnit("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", "746f6b656e", 100)
	a.Mint(u, nil, nil)
	if len(a.mint) != 1 {
		t.Errorf("expected 1 mint, got %d", len(a.mint))
	}
	if a.isEstimateRequired {
		t.Error("expected isEstimateRequired to be false for nil redeemer")
	}
}

func TestSetFee(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc).SetFee(200000)
	if a.Fee != 200000 {
		t.Errorf("expected fee 200000, got %d", a.Fee)
	}
}

func TestCompleteSimpleTransfer(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetTtl(50000000)

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}

	tx := a.GetTx()
	if tx == nil {
		t.Fatal("expected non-nil transaction")
	}
	if tx.Body.TxFee == 0 {
		t.Error("expected non-zero fee")
	}
	if len(tx.Body.TxOutputs) < 1 {
		t.Error("expected at least 1 output")
	}
}

func TestCompleteCborEncoding(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetTtl(50000000)

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}

	txCbor, err := a.GetTxCbor()
	if err != nil {
		t.Fatal(err)
	}
	if len(txCbor) == 0 {
		t.Error("expected non-empty CBOR")
	}
}

func TestCompleteInsufficientFunds(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 1_000_000, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 100_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetTtl(50000000)

	_, err = a.Complete()
	if err == nil {
		t.Error("expected insufficient funds error")
	}
}

func TestCompleteWithPreselectedInputs(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	var txHash common.Blake2b256
	txHash[0] = 0x01

	input := shelley.ShelleyTransactionInput{
		TxId:        txHash,
		OutputIndex: 0,
	}
	output := babbage.BabbageTransactionOutput{
		OutputAddress: addr,
		OutputAmount: mary.MaryTransactionOutputValue{
			Amount: 10_000_000,
		},
	}
	utxo := common.Utxo{Id: input, Output: &output}

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddInput(utxo).
		AddPayment(p).
		SetTtl(50000000)

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}
	if a.GetTx() == nil {
		t.Fatal("expected built transaction")
	}
}

func TestCompleteMultiplePayments(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 50_000_000, 0x01, 0)

	w := NewExternalWallet(addr)
	p1, err := NewPayment(validTestAddrBech32, 5_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := NewPayment(validTestAddrBech32, 3_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	p3, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p1).
		AddPayment(p2).
		AddPayment(p3).
		SetTtl(50000000)

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}

	tx := a.GetTx()
	// At least 3 payment outputs + change
	if len(tx.Body.TxOutputs) < 3 {
		t.Errorf("expected at least 3 outputs, got %d", len(tx.Body.TxOutputs))
	}
}

func TestCompleteWithExplicitFee(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetFee(300000).
		SetTtl(50000000)

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}
	if a.GetTx().Body.TxFee != 300000 {
		t.Errorf("expected fee 300000, got %d", a.GetTx().Body.TxFee)
	}
}

func TestExternalWalletAddress(t *testing.T) {
	addr := testAddress(t)
	w := NewExternalWallet(addr)
	got := w.Address()
	// Compare the raw address bytes since String() encoding may differ
	gotBytes, err := got.Bytes()
	if err != nil {
		t.Fatalf("failed to get bytes from wallet address: %v", err)
	}
	wantBytes, err := addr.Bytes()
	if err != nil {
		t.Fatalf("failed to get bytes from original address: %v", err)
	}
	if len(gotBytes) != len(wantBytes) {
		t.Errorf("address bytes length mismatch: got %d, want %d", len(gotBytes), len(wantBytes))
	}
	for i := range gotBytes {
		if gotBytes[i] != wantBytes[i] {
			t.Errorf("address byte mismatch at index %d", i)
			break
		}
	}
}

func TestExternalWalletCannotSign(t *testing.T) {
	w := NewExternalWallet(testAddress(t))
	_, err := w.SignTxBody(common.Blake2b256{})
	if err == nil {
		t.Error("expected error from external wallet sign")
	}
}

// --- Smart Contract Method Tests ---

func TestCollectFrom(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	var hash common.Blake2b256
	hash[0] = 1
	utxo := makeTestUtxo(t, hash, 0, 5_000_000)
	redeemer := common.Datum{}
	exUnits := common.ExUnits{Memory: 1000, Steps: 2000}

	a.CollectFrom(utxo, redeemer, exUnits)
	if len(a.preselectedUtxos) != 1 {
		t.Errorf("expected 1 preselected utxo, got %d", len(a.preselectedUtxos))
	}
	if !a.isEstimateRequired {
		t.Error("expected isEstimateRequired to be true")
	}
	if len(a.redeemers) != 1 {
		t.Errorf("expected 1 redeemer, got %d", len(a.redeemers))
	}
}

func TestPayToContract(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)
	datum := common.Datum{}

	// Inline datum (default)
	a.PayToContract(addr, &datum, 2_000_000)
	if len(a.payments) != 1 {
		t.Fatalf("expected 1 payment, got %d", len(a.payments))
	}
}

func TestPayToContractWithDatumHash(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)
	datum := common.Datum{}

	a, err := a.PayToContractWithDatumHash(addr, &datum, 2_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.payments) != 1 {
		t.Fatalf("expected 1 payment, got %d", len(a.payments))
	}
	if len(a.datums) != 1 {
		t.Errorf("expected 1 datum in witness set, got %d", len(a.datums))
	}
}

func TestMintWithRedeemer(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	unit := NewUnit("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", "746f6b656e", 100)
	redeemer := common.Datum{}
	exUnits := common.ExUnits{Memory: 1000, Steps: 2000}

	a.Mint(unit, &redeemer, &exUnits)
	if len(a.mint) != 1 {
		t.Errorf("expected 1 mint, got %d", len(a.mint))
	}
	if len(a.mintRedeemers) != 1 {
		t.Errorf("expected 1 mint redeemer, got %d", len(a.mintRedeemers))
	}
	if !a.isEstimateRequired {
		t.Error("expected isEstimateRequired to be true")
	}
}

// --- AttachScript Tests ---

func TestAttachScriptV1Dedup(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	script := common.PlutusV1Script([]byte{0x01, 0x02, 0x03})

	a.AttachScript(script)
	a.AttachScript(script) // duplicate
	if len(a.v1scripts) != 1 {
		t.Errorf("expected 1 script (dedup), got %d", len(a.v1scripts))
	}
}

func TestAttachScriptV2Dedup(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	script := common.PlutusV2Script([]byte{0x01, 0x02, 0x03})

	a.AttachScript(script)
	a.AttachScript(script) // duplicate
	if len(a.v2scripts) != 1 {
		t.Errorf("expected 1 script (dedup), got %d", len(a.v2scripts))
	}
}

func TestAttachScriptV3Dedup(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	script := common.PlutusV3Script([]byte{0x01, 0x02, 0x03})

	a.AttachScript(script)
	a.AttachScript(script) // duplicate
	if len(a.v3scripts) != 1 {
		t.Errorf("expected 1 script (dedup), got %d", len(a.v3scripts))
	}
}

func TestAttachScriptNativeScript(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	var keyHash common.Blake2b224
	keyHash[0] = 0xAA
	ns, err := NewNativeScriptPubkey(keyHash)
	if err != nil {
		t.Fatal(err)
	}
	a.AttachScript(ns)
	if len(a.nativescripts) != 1 {
		t.Errorf("expected 1 native script, got %d", len(a.nativescripts))
	}
	// Dedup
	a.AttachScript(ns)
	if len(a.nativescripts) != 1 {
		t.Errorf("expected 1 native script after dedup, got %d", len(a.nativescripts))
	}
}

func TestAttachScriptMixedTypes(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	// Same bytes, different types - should not be deduped
	a.AttachScript(common.PlutusV1Script([]byte{0x01}))
	a.AttachScript(common.PlutusV2Script([]byte{0x01}))
	if len(a.v1scripts) != 1 {
		t.Errorf("expected 1 v1 script, got %d", len(a.v1scripts))
	}
	if len(a.v2scripts) != 1 {
		t.Errorf("expected 1 v2 script, got %d", len(a.v2scripts))
	}
}

func TestAddDatum(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	datum := common.Datum{}
	a.AddDatum(&datum)
	if len(a.datums) != 1 {
		t.Errorf("expected 1 datum, got %d", len(a.datums))
	}
}

// --- Convenience Payment Method Tests ---

func TestPayToAddress(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)

	a.PayToAddress(addr, 2_000_000)
	if len(a.payments) != 1 {
		t.Errorf("expected 1 payment, got %d", len(a.payments))
	}
}

func TestPayToAddressWithUnits(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)
	unit := NewUnit("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", "746f6b656e", 100)

	a.PayToAddress(addr, 2_000_000, unit)
	if len(a.payments) != 1 {
		t.Errorf("expected 1 payment, got %d", len(a.payments))
	}
}

// --- Reference Script Payment Method Tests ---

func TestPayToAddressWithReferenceScript(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)
	script := common.PlutusV2Script([]byte{0x01, 0x02})

	a, err := a.PayToAddressWithReferenceScript(addr, 2_000_000, script)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.payments) != 1 {
		t.Errorf("expected 1 payment, got %d", len(a.payments))
	}
}

// --- Withdrawal Tests ---

func TestAddWithdrawal(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)

	a.AddWithdrawal(addr, 1_000_000, nil, nil)
	if len(a.withdrawals) != 1 {
		t.Errorf("expected 1 withdrawal, got %d", len(a.withdrawals))
	}
}

func TestAddWithdrawalWithRedeemer(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)
	redeemer := common.Datum{}

	a.AddWithdrawal(addr, 1_000_000, &redeemer, nil)
	if len(a.withdrawals) != 1 {
		t.Errorf("expected 1 withdrawal, got %d", len(a.withdrawals))
	}
	if !a.isEstimateRequired {
		t.Error("expected isEstimateRequired to be true")
	}
	if len(a.stakeRedeemers) != 1 {
		t.Errorf("expected 1 stake redeemer, got %d", len(a.stakeRedeemers))
	}
}

// --- Metadata Tests ---

func TestSetShelleyMetadata(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)

	metadata := map[uint64]any{
		0: "test metadata",
		1: int64(42),
	}
	a.SetShelleyMetadata(metadata)
	if a.auxiliaryData == nil {
		t.Fatal("expected non-nil auxiliaryData")
	}
}

func TestCompleteWithMetadata(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetTtl(50000000)

	a.SetShelleyMetadata(map[uint64]any{
		0: "hello",
	})

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}

	tx := a.GetTx()
	if tx == nil {
		t.Fatal("expected non-nil transaction")
	}
	if tx.Body.TxAuxDataHash == nil {
		t.Error("expected non-nil aux data hash")
	}
	if tx.TxMetadata == nil {
		t.Error("expected non-nil metadata")
	}
}

// --- Change Address Tests ---

func TestSetChangeAddress(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)

	a.SetChangeAddress(addr)
	if a.changeAddress == nil {
		t.Error("expected non-nil change address")
	}
}

// --- Clone Tests ---

func TestClone(t *testing.T) {
	cc := setupFixedContext()
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetTtl(1000).
		SetFeePadding(5000).
		AddPayment(p)

	clone := a.Clone()
	if clone.Ttl != a.Ttl {
		t.Error("TTL not cloned")
	}
	if clone.FeePadding != a.FeePadding {
		t.Error("FeePadding not cloned")
	}
	if len(clone.payments) != len(a.payments) {
		t.Error("payments not cloned")
	}

	// Verify independence
	clone.Ttl = 9999
	if a.Ttl == 9999 {
		t.Error("clone is not independent from original")
	}
}

// --- Loading/Utility Tests ---

func TestLoadTxCborInvalidHex(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)

	_, err := a.LoadTxCbor("not-hex!")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestUtxoFromRefInvalidHex(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)

	_, err := a.UtxoFromRef("not-hex!", 0)
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestGetUsedUTxOs(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	a.usedUtxos = []string{"abc#0", "def#1"}

	used := a.GetUsedUTxOs()
	if len(used) != 2 {
		t.Errorf("expected 2 used utxos, got %d", len(used))
	}
}

func TestGetWallet(t *testing.T) {
	cc := setupFixedContext()
	w := NewExternalWallet(testAddress(t))
	a := New(cc).SetWallet(w)

	if a.GetWallet() == nil {
		t.Error("expected non-nil wallet")
	}
}

func TestGetBurnsMintPositive(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	u := NewUnit("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", "746f6b656e", 100)
	a.Mint(u, nil, nil)

	burns, err := a.GetBurns()
	if err != nil {
		t.Fatal(err)
	}
	if !burns.HasAssets() {
		t.Error("expected mint value to have assets")
	}
}

func TestGetBurnsMintNegative(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	// Negative quantity represents a burn
	u := NewUnit("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", "746f6b656e", -100)
	a.Mint(u, nil, nil)

	burns, err := a.GetBurns()
	if err != nil {
		t.Fatal(err)
	}
	// HasAssets returns false for negative quantities since MultiAssetIsEmpty
	// only considers positive quantities as "non-empty"
	if burns.Assets == nil {
		t.Error("expected burn to populate Assets field")
	}
}

func TestDisableExecutionUnitsEstimation(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	if !a.estimateExUnits {
		t.Error("expected estimateExUnits to be true by default")
	}

	a.DisableExecutionUnitsEstimation()
	if a.estimateExUnits {
		t.Error("expected estimateExUnits to be false after disable")
	}
}

func TestSetCollateralAmount(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc).SetCollateralAmount(10_000_000)
	if a.collateralAmount != 10_000_000 {
		t.Errorf("expected 10000000, got %d", a.collateralAmount)
	}
}

func TestAddRequiredSignerPaymentKey(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)

	a.AddRequiredSignerPaymentKey(addr)
	if len(a.requiredSigners) != 1 {
		t.Errorf("expected 1 required signer, got %d", len(a.requiredSigners))
	}
}

func TestAddRequiredSignerStakeKey(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)

	a.AddRequiredSignerStakeKey(addr)
	if len(a.requiredSigners) != 1 {
		t.Errorf("expected 1 required signer, got %d", len(a.requiredSigners))
	}
}

func TestAddRequiredSignerBothKeys(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)

	a.AddRequiredSignerPaymentKey(addr).AddRequiredSignerStakeKey(addr)
	if len(a.requiredSigners) != 2 {
		t.Errorf("expected 2 required signers (payment+staking), got %d", len(a.requiredSigners))
	}
}

// --- NewScriptRef Tests ---

func TestNewScriptRefV1(t *testing.T) {
	script := common.PlutusV1Script([]byte{0x01, 0x02})
	ref, err := NewScriptRef(script)
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil {
		t.Fatal("expected non-nil ScriptRef")
	}
	if ref.Type != 1 {
		t.Errorf("expected type 1, got %d", ref.Type)
	}
}

func TestNewScriptRefV2(t *testing.T) {
	script := common.PlutusV2Script([]byte{0x01, 0x02})
	ref, err := NewScriptRef(script)
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil {
		t.Fatal("expected non-nil ScriptRef")
	}
	if ref.Type != 2 {
		t.Errorf("expected type 2, got %d", ref.Type)
	}
}

func TestNewScriptRefV3(t *testing.T) {
	script := common.PlutusV3Script([]byte{0x01, 0x02})
	ref, err := NewScriptRef(script)
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil {
		t.Fatal("expected non-nil ScriptRef")
	}
	if ref.Type != 3 {
		t.Errorf("expected type 3, got %d", ref.Type)
	}
}

// --- Complete with Withdrawals ---

func TestCompleteWithWithdrawal(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetTtl(50000000)

	a.AddWithdrawal(addr, 500_000, nil, nil)

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}
	tx := a.GetTx()
	if tx == nil {
		t.Fatal("expected non-nil transaction")
	}
	if len(tx.Body.TxWithdrawals) != 1 {
		t.Errorf("expected 1 withdrawal, got %d", len(tx.Body.TxWithdrawals))
	}
}

// --- Complete with Change Address ---

func TestCompleteWithCustomChangeAddress(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetChangeAddress(addr).
		SetTtl(50000000)

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}
	if a.GetTx() == nil {
		t.Fatal("expected non-nil transaction")
	}
}

// --- Reference Inputs in Complete ---

func TestCompleteWithReferenceInputs(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	addTestUtxo(cc, addr, 10_000_000, 0x01, 0)

	hashHex := "aabb000000000000000000000000000000000000000000000000000000000000"
	w := NewExternalWallet(addr)
	p, err := NewPayment(validTestAddrBech32, 2_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cc).
		SetWallet(w).
		AddPayment(p).
		SetTtl(50000000)

	a, err = a.AddReferenceInput(hashHex, 0)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.AddReferenceInput(hashHex, 1)
	if err != nil {
		t.Fatal(err)
	}

	a, err = a.Complete()
	if err != nil {
		t.Fatal(err)
	}
	tx := a.GetTx()
	if tx == nil {
		t.Fatal("expected non-nil transaction")
	}
	refInputs := tx.Body.TxReferenceInputs.Items()
	if len(refInputs) != 2 {
		t.Errorf("expected 2 reference inputs, got %d", len(refInputs))
	}
}

// --- AddVerificationKeyWitness ---

func TestAddVerificationKeyWitnessNoTx(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	witness := common.VkeyWitness{
		Vkey:      make([]byte, 32),
		Signature: make([]byte, 64),
	}
	_, err := a.AddVerificationKeyWitness(witness)
	if err == nil {
		t.Error("expected error when no transaction built")
	}
}

// --- SignWithSkey ---

func TestSignWithSkeyNoTx(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	vkey := make([]byte, 32)
	skey := make([]byte, 32)
	_, err := a.SignWithSkey(vkey, skey)
	if err == nil {
		t.Error("expected error when no transaction built")
	}
}

// --- ConsumeUTxO ---

func TestConsumeUTxO(t *testing.T) {
	cc := setupFixedContext()
	addr := testAddress(t)
	w := NewExternalWallet(addr)
	a := New(cc).SetWallet(w)

	var hash common.Blake2b256
	hash[0] = 1
	utxo := makeTestUtxo(t, hash, 0, 10_000_000)
	payment, err := NewPayment(validTestAddrBech32, 3_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}

	a, err = a.ConsumeUTxO(utxo, payment)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.preselectedUtxos) != 1 {
		t.Errorf("expected 1 preselected utxo, got %d", len(a.preselectedUtxos))
	}
	// Should have the original payment + a remainder payment
	if len(a.payments) < 1 {
		t.Errorf("expected at least 1 payment, got %d", len(a.payments))
	}
}

// --- resolveCredential Tests ---

func TestResolveCredentialFromBech32(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)

	// Should work with bech32 string
	a, err := a.RegisterStake(validTestAddrBech32)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(a.certificates))
	}
}

func TestResolveCredentialFromAddress(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)
	addr := testAddress(t)

	// Should work with common.Address
	a, err := a.RegisterStake(addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(a.certificates))
	}
}

func TestResolveCredentialInvalidBech32(t *testing.T) {
	cc := setupFixedContext()
	a := New(cc)

	_, err := a.RegisterStake("not-a-valid-address")
	if err == nil {
		t.Error("expected error for invalid bech32")
	}
}
