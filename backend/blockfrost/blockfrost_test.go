package blockfrost

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
)

func testAddress(t *testing.T) common.Address {
	t.Helper()
	var raw [57]byte
	raw[0] = 0x00
	raw[1] = 0xAA
	raw[29] = 0xBB
	addr, err := common.NewAddressFromBytes(raw[:])
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestHydrateUtxoResolvesInlineDatumAndReferenceScript(t *testing.T) {
	script := common.PlutusV2Script([]byte{0x01, 0x02})
	scriptHashHex := hex.EncodeToString(script.Hash().Bytes())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/scripts/"+scriptHashHex+"/cbor" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"cbor": hex.EncodeToString(script),
		})
	}))
	defer server.Close()

	addr := testAddress(t)
	ctx := NewBlockFrostChainContext(server.URL, 0, "")
	raw := bfAddressUTxO{
		TxHash:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		OutputIndex:         0,
		Address:             addr.String(),
		Amount:              []bfAddressAmount{{Unit: "lovelace", Quantity: "1000000"}},
		InlineDatum:         json.RawMessage(`{"int":42}`),
		ReferenceScriptHash: scriptHashHex,
	}

	utxo, err := ctx.hydrateUtxo(raw, addr)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := utxo.Output.(*babbage.BabbageTransactionOutput)
	if !ok {
		t.Fatalf("unexpected output type %T", utxo.Output)
	}
	if output.Datum() == nil {
		t.Fatal("expected inline datum to be populated")
	}
	scriptRef := output.ScriptRef()
	if scriptRef == nil {
		t.Fatal("expected reference script to be populated")
	}
	if _, ok := scriptRef.(common.PlutusV2Script); !ok {
		t.Fatalf("expected PlutusV2 reference script, got %T", scriptRef)
	}
}
