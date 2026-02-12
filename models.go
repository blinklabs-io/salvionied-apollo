package apollo

import (
	"encoding/hex"
	"fmt"
	"math"
	"math/big"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"

	"github.com/Salvionied/apollo/v2/backend"
)

// Unit represents a native asset quantity.
type Unit struct {
	PolicyId string
	Name     string
	Quantity int64
}

// NewUnit creates a new Unit.
func NewUnit(policyId, name string, quantity int64) Unit {
	return Unit{
		PolicyId: policyId,
		Name:     name,
		Quantity: quantity,
	}
}

// ToValue converts a Unit to a Value containing this asset.
func (u *Unit) ToValue() Value {
	if u.PolicyId == "" || u.PolicyId == "lovelace" {
		if u.Quantity < 0 {
			return Value{}
		}
		return NewSimpleValue(uint64(u.Quantity)) //nolint:gosec // validated non-negative above
	}
	policyBytes, err := hex.DecodeString(u.PolicyId)
	if err != nil || len(policyBytes) != common.Blake2b224Size {
		return Value{}
	}
	var policyId common.Blake2b224
	copy(policyId[:], policyBytes)

	nameBytes, err := hex.DecodeString(u.Name)
	if err != nil {
		nameBytes = []byte(u.Name)
	}

	data := map[common.Blake2b224]map[cbor.ByteString]common.MultiAssetTypeOutput{
		policyId: {
			cbor.NewByteString(nameBytes): big.NewInt(u.Quantity),
		},
	}
	assets := common.NewMultiAsset[common.MultiAssetTypeOutput](data)
	return NewValue(0, &assets)
}

// PaymentI is the interface for payment types.
type PaymentI interface {
	EnsureMinUTXO(cc backend.ChainContext) error
	ToTxOut() (*babbage.BabbageTransactionOutput, error)
	ToValue() (Value, error)
}

// Payment represents a transaction output with receiver, lovelace, and optional assets.
type Payment struct {
	Lovelace  int64
	Receiver  common.Address
	Units     []Unit
	Datum     *common.Datum
	DatumHash []byte
	IsInline  bool
	ScriptRef *common.ScriptRef
}

// NewPayment creates a new Payment.
func NewPayment(receiver string, lovelace int64, units []Unit) (*Payment, error) {
	addr, err := common.NewAddress(receiver)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver address: %w", err)
	}
	return &Payment{
		Lovelace: lovelace,
		Receiver: addr,
		Units:    units,
	}, nil
}

// NewPaymentFromValue creates a Payment from an Address and Value.
func NewPaymentFromValue(receiver common.Address, value Value) *Payment {
	payment := &Payment{
		Receiver: receiver,
		Lovelace: int64(value.Coin), //nolint:gosec // ADA supply fits in int64
	}
	if value.Assets != nil {
		for _, policyId := range value.Assets.Policies() {
			for _, assetName := range value.Assets.Assets(policyId) {
				qty := value.Assets.Asset(policyId, assetName)
				// Use Int64() which truncates for values > MaxInt64.
				// This is safe because Cardano native asset quantities fit in int64.
				q := qty.Int64()
				if !qty.IsInt64() {
					// Saturate to MaxInt64 for out-of-range values rather than silently truncating.
					q = math.MaxInt64
				}
				payment.Units = append(payment.Units, Unit{
					PolicyId: hex.EncodeToString(policyId.Bytes()),
					Name:     hex.EncodeToString(assetName),
					Quantity: q,
				})
			}
		}
	}
	return payment
}

// PaymentFromTxOut creates a Payment from a BabbageTransactionOutput.
func PaymentFromTxOut(txOut *babbage.BabbageTransactionOutput) *Payment {
	payment := &Payment{
		Receiver: txOut.OutputAddress,
		Lovelace: int64(txOut.OutputAmount.Amount), //nolint:gosec // ADA supply fits in int64
	}
	if txOut.OutputAmount.Assets != nil {
		for _, policyId := range txOut.OutputAmount.Assets.Policies() {
			for _, assetName := range txOut.OutputAmount.Assets.Assets(policyId) {
				qty := txOut.OutputAmount.Assets.Asset(policyId, assetName)
				q := qty.Int64()
				if !qty.IsInt64() {
					q = math.MaxInt64
				}
				payment.Units = append(payment.Units, Unit{
					PolicyId: hex.EncodeToString(policyId.Bytes()),
					Name:     hex.EncodeToString(assetName),
					Quantity: q,
				})
			}
		}
	}
	return payment
}

// ToValue converts a Payment to a Value.
func (p *Payment) ToValue() (Value, error) {
	if p.Lovelace < 0 {
		return Value{}, fmt.Errorf("negative lovelace amount: %d", p.Lovelace)
	}
	coin := uint64(p.Lovelace) //nolint:gosec // validated non-negative above
	v := NewSimpleValue(coin)
	for _, unit := range p.Units {
		uv := unit.ToValue()
		var err error
		v, err = v.Add(uv)
		if err != nil {
			return Value{}, err
		}
	}
	return v, nil
}

// EnsureMinUTXO ensures the payment meets the minimum UTxO requirement.
func (p *Payment) EnsureMinUTXO(cc backend.ChainContext) error {
	if len(p.Units) == 0 && p.Lovelace >= 1_000_000 {
		return nil
	}
	txOut, err := p.ToTxOut()
	if err != nil {
		return fmt.Errorf("failed to build tx output: %w", err)
	}
	pp, err := cc.ProtocolParams()
	if err != nil {
		return fmt.Errorf("failed to get protocol params: %w", err)
	}
	coins, err := MinLovelacePostAlonzo(txOut, pp.CoinsPerUtxoByteValue())
	if err != nil {
		return fmt.Errorf("failed to compute min UTxO: %w", err)
	}
	if p.Lovelace < coins {
		p.Lovelace = coins
	}
	return nil
}

// ToTxOut converts a Payment to a BabbageTransactionOutput.
func (p *Payment) ToTxOut() (*babbage.BabbageTransactionOutput, error) {
	val, err := p.ToValue()
	if err != nil {
		return nil, fmt.Errorf("failed to compute payment value: %w", err)
	}
	output := NewBabbageOutput(p.Receiver, val, nil, p.ScriptRef)

	if p.IsInline && p.Datum != nil {
		datumOpt, err := NewDatumOptionInline(p.Datum)
		if err != nil {
			return nil, fmt.Errorf("failed to create inline datum: %w", err)
		}
		output.DatumOption = datumOpt
	} else if len(p.DatumHash) == common.Blake2b256Size {
		var hash common.Blake2b256
		copy(hash[:], p.DatumHash)
		datumOpt, err := NewDatumOptionHash(hash)
		if err != nil {
			return nil, fmt.Errorf("failed to create datum hash: %w", err)
		}
		output.DatumOption = datumOpt
	}
	return &output, nil
}
