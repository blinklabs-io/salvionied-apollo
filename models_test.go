package apollo

import (
	"testing"
)

func TestNewUnit(t *testing.T) {
	u := NewUnit("abc123", "token", 100)
	if u.PolicyId != "abc123" {
		t.Errorf("expected abc123, got %s", u.PolicyId)
	}
	if u.Name != "token" {
		t.Errorf("expected token, got %s", u.Name)
	}
	if u.Quantity != 100 {
		t.Errorf("expected 100, got %d", u.Quantity)
	}
}

func TestUnitToValueLovelace(t *testing.T) {
	u := NewUnit("lovelace", "", 5000000)
	v, err := u.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	if v.Coin != 5000000 {
		t.Errorf("expected 5000000 lovelace, got %d", v.Coin)
	}
	if v.HasAssets() {
		t.Error("lovelace unit should not have assets")
	}
}

func TestUnitToValueEmpty(t *testing.T) {
	u := NewUnit("", "", 1000000)
	v, err := u.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	if v.Coin != 1000000 {
		t.Errorf("expected 1000000 lovelace, got %d", v.Coin)
	}
}

func TestUnitToValueAsset(t *testing.T) {
	// Use a valid 56-char hex policy ID
	policyHex := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
	nameHex := "746f6b656e" // "token" in hex
	u := NewUnit(policyHex, nameHex, 42)
	v, err := u.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	if v.Coin != 0 {
		t.Errorf("expected 0 lovelace for asset unit, got %d", v.Coin)
	}
	if !v.HasAssets() {
		t.Error("expected assets for non-lovelace unit")
	}
}

func TestUnitToValueInvalidPolicy(t *testing.T) {
	u := NewUnit("not-hex!", "token", 100)
	_, err := u.ToValue()
	if err == nil {
		t.Error("expected error for invalid policy")
	}
}

func TestNewPayment(t *testing.T) {
	p, err := NewPayment(validTestAddrBech32, 2000000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Lovelace != 2000000 {
		t.Errorf("expected 2000000, got %d", p.Lovelace)
	}
	if p.Receiver.String() == "" {
		t.Error("expected valid receiver address")
	}
}

func TestPaymentToValue(t *testing.T) {
	p := &Payment{
		Lovelace: 5000000,
	}
	v, err := p.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	if v.Coin != 5000000 {
		t.Errorf("expected 5000000, got %d", v.Coin)
	}
}

func TestPaymentToTxOut(t *testing.T) {
	addr := testAddress(t)
	p := &Payment{
		Receiver: addr,
		Lovelace: 3000000,
	}
	txOut, err := p.ToTxOut()
	if err != nil {
		t.Fatal(err)
	}
	if txOut.OutputAmount.Amount != 3000000 {
		t.Errorf("expected 3000000, got %d", txOut.OutputAmount.Amount)
	}
}

func TestPaymentFromTxOut(t *testing.T) {
	addr := testAddress(t)
	output := NewBabbageOutputSimple(addr, 7000000)
	p := PaymentFromTxOut(&output)
	if p.Lovelace != 7000000 {
		t.Errorf("expected 7000000, got %d", p.Lovelace)
	}
}

func TestNewPaymentFromValue(t *testing.T) {
	addr := testAddress(t)
	v := NewSimpleValue(4000000)
	p := NewPaymentFromValue(addr, v)
	if p.Lovelace != 4000000 {
		t.Errorf("expected 4000000, got %d", p.Lovelace)
	}
}
