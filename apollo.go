package apollo

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
	"github.com/blinklabs-io/gouroboros/ledger/conway"
	"github.com/blinklabs-io/gouroboros/ledger/shelley"

	"github.com/Salvionied/apollo/v2/backend"
)

const (
	ExMemoryBuffer = 0.2
	ExStepBuffer   = 0.2
	StakeDeposit   = 2_000_000
)

// Apollo is the main transaction builder.
type Apollo struct {
	Context            backend.ChainContext
	payments           []PaymentI
	isEstimateRequired bool
	utxos              []common.Utxo
	preselectedUtxos   []common.Utxo
	inputAddresses     []common.Address
	tx                 *conway.ConwayTransaction
	datums             []common.Datum
	requiredSigners    []common.Blake2b224
	v1scripts          []common.PlutusV1Script
	v2scripts          []common.PlutusV2Script
	v3scripts          []common.PlutusV3Script
	redeemers          map[string]redeemerEntry // keyed by UTxO ref string
	stakeRedeemers     map[string]redeemerEntry
	mintRedeemers      map[string]redeemerEntry
	mint               []Unit
	collaterals        []common.Utxo
	Fee                int64
	FeePadding         int64
	Ttl                int64
	ValidityStart      int64
	totalCollateral    int64
	referenceInputs  []shelley.ShelleyTransactionInput
	collateralReturn *babbage.BabbageTransactionOutput
	nativescripts      []common.NativeScript
	usedUtxos          []string
	wallet             Wallet
	certificates       []common.CertificateWrapper
	withdrawals        map[string]withdrawalEntry
	auxiliaryData      *auxData
	collateralAmount   int64
	scriptHashes       []string
	changeAddress      *common.Address
	estimateExUnits    bool
}

type redeemerEntry struct {
	Tag     common.RedeemerTag
	Data    common.Datum
	ExUnits common.ExUnits
}

type withdrawalEntry struct {
	Address common.Address
	Amount  uint64
}

type auxData struct {
	metadata map[uint64]any
}

// New creates a new Apollo transaction builder with the given chain context.
func New(cc backend.ChainContext) *Apollo {
	return &Apollo{
		Context:         cc,
		redeemers:       make(map[string]redeemerEntry),
		stakeRedeemers:  make(map[string]redeemerEntry),
		mintRedeemers:   make(map[string]redeemerEntry),
		withdrawals:     make(map[string]withdrawalEntry),
		estimateExUnits: true,
	}
}

// SetWallet sets the wallet for the transaction builder.
func (a *Apollo) SetWallet(w Wallet) *Apollo {
	a.wallet = w
	return a
}

// SetWalletFromMnemonic creates a BursaWallet from a mnemonic and sets it.
func (a *Apollo) SetWalletFromMnemonic(mnemonic string) (*Apollo, error) {
	w, err := NewBursaWallet(mnemonic)
	if err != nil {
		return a, err
	}
	a.wallet = w
	return a, nil
}

// SetWalletFromMnemonicWithPassphrase creates a BursaWallet from a mnemonic and passphrase and sets it.
func (a *Apollo) SetWalletFromMnemonicWithPassphrase(mnemonic string, passphrase string) (*Apollo, error) {
	w, err := NewBursaWalletWithPassphrase(mnemonic, passphrase)
	if err != nil {
		return a, err
	}
	a.wallet = w
	return a, nil
}

// AddPayment adds a payment to the transaction.
func (a *Apollo) AddPayment(payment PaymentI) *Apollo {
	a.payments = append(a.payments, payment)
	return a
}

// AddLoadedUTxOs adds UTxOs to the available pool for coin selection.
func (a *Apollo) AddLoadedUTxOs(utxos ...common.Utxo) *Apollo {
	a.utxos = append(a.utxos, utxos...)
	return a
}

// AddInput adds a specific UTxO as a transaction input.
func (a *Apollo) AddInput(utxo common.Utxo) *Apollo {
	a.preselectedUtxos = append(a.preselectedUtxos, utxo)
	return a
}

// AddInputAddress adds an address whose UTxOs should be used for coin selection.
func (a *Apollo) AddInputAddress(addr common.Address) *Apollo {
	a.inputAddresses = append(a.inputAddresses, addr)
	return a
}

// AddRequiredSigner adds a required signer by key hash.
func (a *Apollo) AddRequiredSigner(pkh common.Blake2b224) *Apollo {
	a.requiredSigners = append(a.requiredSigners, pkh)
	return a
}

// AddRequiredSignerPaymentKey adds the payment key hash from an address as a required signer.
func (a *Apollo) AddRequiredSignerPaymentKey(addr common.Address) *Apollo {
	a.requiredSigners = append(a.requiredSigners, addr.PaymentKeyHash())
	return a
}

// AddRequiredSignerStakeKey adds the staking key hash from an address as a required signer.
func (a *Apollo) AddRequiredSignerStakeKey(addr common.Address) *Apollo {
	skh := addr.StakeKeyHash()
	if skh != (common.Blake2b224{}) {
		a.requiredSigners = append(a.requiredSigners, skh)
	}
	return a
}

// SetTtl sets the transaction time-to-live.
func (a *Apollo) SetTtl(ttl int64) *Apollo {
	a.Ttl = ttl
	return a
}

// SetValidityStart sets the validity start slot.
func (a *Apollo) SetValidityStart(start int64) *Apollo {
	a.ValidityStart = start
	return a
}

// SetFee sets a specific fee (disables fee estimation).
func (a *Apollo) SetFee(fee int64) *Apollo {
	a.Fee = fee
	return a
}

// SetFeePadding adds additional fee padding.
func (a *Apollo) SetFeePadding(padding int64) *Apollo {
	a.FeePadding = padding
	return a
}

// SetChangeAddress sets the address to receive change outputs.
func (a *Apollo) SetChangeAddress(addr common.Address) *Apollo {
	a.changeAddress = &addr
	return a
}

// AddCollateral adds a UTxO as collateral for script transactions.
func (a *Apollo) AddCollateral(utxo common.Utxo) *Apollo {
	a.collaterals = append(a.collaterals, utxo)
	return a
}

// AddDatum adds a datum to the witness set.
func (a *Apollo) AddDatum(datum *common.Datum) *Apollo {
	if datum != nil {
		a.datums = append(a.datums, *datum)
	}
	return a
}

// AddReferenceInput adds a reference input to the transaction.
func (a *Apollo) AddReferenceInput(txHash string, index int) (*Apollo, error) {
	hashBytes, err := hex.DecodeString(txHash)
	if err != nil {
		return a, fmt.Errorf("invalid tx hash hex: %w", err)
	}
	if len(hashBytes) != common.Blake2b256Size {
		return a, fmt.Errorf("invalid tx hash length: expected %d bytes, got %d", common.Blake2b256Size, len(hashBytes))
	}
	if index < 0 || index > math.MaxUint32 {
		return a, fmt.Errorf("index must be 0-%d, got %d", math.MaxUint32, index)
	}
	var hash common.Blake2b256
	copy(hash[:], hashBytes)
	input := shelley.ShelleyTransactionInput{
		TxId:        hash,
		OutputIndex: uint32(index),
	}
	a.referenceInputs = append(a.referenceInputs, input)
	return a, nil
}

// Mint adds tokens to mint. If redeemer and exUnits are provided, sets up script minting.
func (a *Apollo) Mint(unit Unit, redeemer *common.Datum, exUnits *common.ExUnits) *Apollo {
	a.mint = append(a.mint, unit)
	if redeemer != nil && exUnits != nil {
		a.mintRedeemers[unit.PolicyId] = redeemerEntry{
			Tag:     common.RedeemerTagMint,
			Data:    *redeemer,
			ExUnits: *exUnits,
		}
		a.isEstimateRequired = true
	}
	return a
}

// AttachScript attaches a script to the witness set, deduplicating by hash.
// Accepts PlutusV1Script, PlutusV2Script, PlutusV3Script, or NativeScript.
func (a *Apollo) AttachScript(script common.Script) *Apollo {
	hash := script.Hash().String()
	if a.hasScriptHash(hash) {
		return a
	}
	a.scriptHashes = append(a.scriptHashes, hash)
	switch s := script.(type) {
	case common.PlutusV1Script:
		a.v1scripts = append(a.v1scripts, s)
	case common.PlutusV2Script:
		a.v2scripts = append(a.v2scripts, s)
	case common.PlutusV3Script:
		a.v3scripts = append(a.v3scripts, s)
	case common.NativeScript:
		a.nativescripts = append(a.nativescripts, s)
	}
	return a
}

// DisableExecutionUnitsEstimation disables automatic ExUnit estimation.
func (a *Apollo) DisableExecutionUnitsEstimation() *Apollo {
	a.estimateExUnits = false
	return a
}

// --- Smart Contract Methods ---

// CollectFrom adds a script UTxO as input with a spending redeemer.
func (a *Apollo) CollectFrom(utxo common.Utxo, redeemer common.Datum, exUnits common.ExUnits) *Apollo {
	a.isEstimateRequired = true
	a.preselectedUtxos = append(a.preselectedUtxos, utxo)
	ref := utxoRef(utxo)
	a.redeemers[ref] = redeemerEntry{
		Tag:     common.RedeemerTagSpend,
		Data:    redeemer,
		ExUnits: exUnits,
	}
	return a
}

// PayToContract creates a payment to a script address with an inline datum.
func (a *Apollo) PayToContract(addr common.Address, datum *common.Datum, lovelace int64, units ...Unit) *Apollo {
	p := &Payment{
		Receiver: addr,
		Lovelace: lovelace,
		Units:    units,
		Datum:    datum,
		IsInline: true,
	}
	a.payments = append(a.payments, p)
	return a
}

// PayToContractWithDatumHash creates a payment to a script address with a datum hash.
// The datum is added to the witness set and its hash is placed in the output.
func (a *Apollo) PayToContractWithDatumHash(addr common.Address, datum *common.Datum, lovelace int64, units ...Unit) (*Apollo, error) {
	p := &Payment{
		Receiver: addr,
		Lovelace: lovelace,
		Units:    units,
	}
	if datum != nil {
		datumCbor, err := cbor.Encode(datum)
		if err != nil {
			return a, fmt.Errorf("failed to encode datum: %w", err)
		}
		hash := common.Blake2b256Hash(datumCbor)
		p.DatumHash = hash.Bytes()
		a.datums = append(a.datums, *datum)
	}
	a.payments = append(a.payments, p)
	return a, nil
}


// resolveCredential resolves a credential from various input types.
// Accepts: *common.Credential, common.Credential, common.Address, string (bech32), or nil (wallet fallback).
func (a *Apollo) resolveCredential(v any) (common.Credential, error) {
	switch val := v.(type) {
	case *common.Credential:
		if val != nil {
			return *val, nil
		}
		return a.GetStakeCredentialFromWallet()
	case common.Credential:
		return val, nil
	case common.Address:
		return GetStakeCredentialFromAddress(val)
	case string:
		addr, err := common.NewAddress(val)
		if err != nil {
			return common.Credential{}, fmt.Errorf("invalid bech32 address: %w", err)
		}
		return GetStakeCredentialFromAddress(addr)
	case nil:
		return a.GetStakeCredentialFromWallet()
	default:
		return common.Credential{}, fmt.Errorf("unsupported credential type: %T", v)
	}
}

func (a *Apollo) hasScriptHash(hash string) bool {
	for _, h := range a.scriptHashes {
		if h == hash {
			return true
		}
	}
	return false
}

// --- Convenience Payment Methods ---

// PayToAddress creates a simple payment to an address.
func (a *Apollo) PayToAddress(addr common.Address, lovelace int64, units ...Unit) *Apollo {
	p := &Payment{
		Receiver: addr,
		Lovelace: lovelace,
		Units:    units,
	}
	a.payments = append(a.payments, p)
	return a
}

// PayToAddressWithReferenceScript pays to address with a reference script attached.
// The script type (V1/V2/V3) is detected automatically.
func (a *Apollo) PayToAddressWithReferenceScript(addr common.Address, lovelace int64, script common.Script, units ...Unit) *Apollo {
	ref := NewScriptRef(script)
	p := &Payment{Receiver: addr, Lovelace: lovelace, Units: units, ScriptRef: ref}
	a.payments = append(a.payments, p)
	return a
}

// --- UTxO Consumption Methods ---

// ConsumeUTxO adds a utxo as input, deducts payments, and returns remainder as change.
func (a *Apollo) ConsumeUTxO(utxo common.Utxo, payments ...PaymentI) (*Apollo, error) {
	a.preselectedUtxos = append(a.preselectedUtxos, utxo)
	utxoVal := a.utxoValue(utxo)
	totalPayments := Value{}
	for _, p := range payments {
		pv, err := p.ToValue()
		if err != nil {
			return a, fmt.Errorf("failed to compute payment value: %w", err)
		}
		totalPayments, err = totalPayments.Add(pv)
		if err != nil {
			return a, fmt.Errorf("payment value overflow: %w", err)
		}
	}
	a.payments = append(a.payments, payments...)

	remainder, err := utxoVal.Sub(totalPayments)
	if err != nil {
		return a, fmt.Errorf("UTxO value insufficient for payments: %w", err)
	}
	if remainder.Coin > 0 || remainder.HasAssets() {
		if a.wallet == nil {
			return a, errors.New("wallet required to receive UTxO remainder")
		}
		remainderPayment := NewPaymentFromValue(a.wallet.Address(), remainder)
		a.payments = append(a.payments, remainderPayment)
	}
	return a, nil
}

func (a *Apollo) utxoValue(utxo common.Utxo) Value {
	v := Value{}
	if utxo.Output.Amount() != nil {
		v.Coin = utxo.Output.Amount().Uint64()
	}
	if utxo.Output.Assets() != nil {
		v.Assets = CloneMultiAsset(utxo.Output.Assets())
	}
	return v
}

// --- Staking Infrastructure ---

// GetStakeCredentialFromWallet extracts a staking credential from the wallet address.
func (a *Apollo) GetStakeCredentialFromWallet() (common.Credential, error) {
	if a.wallet == nil {
		return common.Credential{}, errors.New("no wallet set")
	}
	return GetStakeCredentialFromAddress(a.wallet.Address())
}

// SetCertificates sets the certificates for the transaction.
func (a *Apollo) SetCertificates(certs []common.CertificateWrapper) *Apollo {
	a.certificates = certs
	return a
}


// --- Stake Registration & Deregistration ---

// RegisterStake creates a stake registration certificate.
// credOrAddr can be: *common.Credential, common.Credential, common.Address, string (bech32), or nil (uses wallet).
func (a *Apollo) RegisterStake(credOrAddr any) (*Apollo, error) {
	cred, err := a.resolveCredential(credOrAddr)
	if err != nil {
		return a, err
	}
	cert := common.StakeRegistrationCertificate{
		CertType:        uint(common.CertificateTypeStakeRegistration),
		StakeCredential: cred,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypeStakeRegistration),
		Certificate: &cert,
	})
	return a, nil
}

// DeregisterStake creates a stake deregistration certificate.
// credOrAddr can be: *common.Credential, common.Credential, common.Address, string (bech32), or nil (uses wallet).
func (a *Apollo) DeregisterStake(credOrAddr any) (*Apollo, error) {
	cred, err := a.resolveCredential(credOrAddr)
	if err != nil {
		return a, err
	}
	cert := common.StakeDeregistrationCertificate{
		CertType:        uint(common.CertificateTypeStakeDeregistration),
		StakeCredential: cred,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypeStakeDeregistration),
		Certificate: &cert,
	})
	return a, nil
}

// --- Stake Delegation ---

// DelegateStake creates a stake delegation certificate.
// credOrAddr can be: *common.Credential, common.Credential, common.Address, string (bech32), or nil (uses wallet).
func (a *Apollo) DelegateStake(credOrAddr any, poolHash common.Blake2b224) (*Apollo, error) {
	cred, err := a.resolveCredential(credOrAddr)
	if err != nil {
		return a, err
	}
	cert := common.StakeDelegationCertificate{
		CertType:        uint(common.CertificateTypeStakeDelegation),
		StakeCredential: &cred,
		PoolKeyHash:     poolHash,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypeStakeDelegation),
		Certificate: &cert,
	})
	return a, nil
}

// RegisterAndDelegateStake creates a combined stake registration and delegation certificate.
// credOrAddr can be: *common.Credential, common.Credential, common.Address, string (bech32), or nil (uses wallet).
func (a *Apollo) RegisterAndDelegateStake(credOrAddr any, poolHash common.Blake2b224, coin int64) (*Apollo, error) {
	cred, err := a.resolveCredential(credOrAddr)
	if err != nil {
		return a, err
	}
	cert := common.StakeRegistrationDelegationCertificate{
		CertType:        uint(common.CertificateTypeStakeRegistrationDelegation),
		StakeCredential: cred,
		PoolKeyHash:     poolHash,
		Amount:          coin,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypeStakeRegistrationDelegation),
		Certificate: &cert,
	})
	return a, nil
}

// --- Vote Delegation ---

// DelegateVote creates a vote delegation certificate.
// credOrAddr can be: *common.Credential, common.Credential, common.Address, string (bech32), or nil (uses wallet).
func (a *Apollo) DelegateVote(credOrAddr any, drep common.Drep) (*Apollo, error) {
	cred, err := a.resolveCredential(credOrAddr)
	if err != nil {
		return a, err
	}
	cert := common.VoteDelegationCertificate{
		CertType:        uint(common.CertificateTypeVoteDelegation),
		StakeCredential: cred,
		Drep:            drep,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypeVoteDelegation),
		Certificate: &cert,
	})
	return a, nil
}

// DelegateStakeAndVote creates a combined stake+vote delegation certificate.
// credOrAddr can be: *common.Credential, common.Credential, common.Address, string (bech32), or nil (uses wallet).
func (a *Apollo) DelegateStakeAndVote(credOrAddr any, poolHash common.Blake2b224, drep common.Drep) (*Apollo, error) {
	cred, err := a.resolveCredential(credOrAddr)
	if err != nil {
		return a, err
	}
	cert := common.StakeVoteDelegationCertificate{
		CertType:        uint(common.CertificateTypeStakeVoteDelegation),
		StakeCredential: cred,
		PoolKeyHash:     poolHash,
		Drep:            drep,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypeStakeVoteDelegation),
		Certificate: &cert,
	})
	return a, nil
}

// RegisterAndDelegateVote creates a combined registration+vote delegation certificate.
// credOrAddr can be: *common.Credential, common.Credential, common.Address, string (bech32), or nil (uses wallet).
func (a *Apollo) RegisterAndDelegateVote(credOrAddr any, drep common.Drep, coin int64) (*Apollo, error) {
	cred, err := a.resolveCredential(credOrAddr)
	if err != nil {
		return a, err
	}
	cert := common.VoteRegistrationDelegationCertificate{
		CertType:        uint(common.CertificateTypeVoteRegistrationDelegation),
		StakeCredential: cred,
		Drep:            drep,
		Amount:          coin,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypeVoteRegistrationDelegation),
		Certificate: &cert,
	})
	return a, nil
}

// RegisterAndDelegateStakeAndVote creates a combined registration+stake+vote delegation certificate.
// credOrAddr can be: *common.Credential, common.Credential, common.Address, string (bech32), or nil (uses wallet).
func (a *Apollo) RegisterAndDelegateStakeAndVote(credOrAddr any, poolHash common.Blake2b224, drep common.Drep, coin int64) (*Apollo, error) {
	cred, err := a.resolveCredential(credOrAddr)
	if err != nil {
		return a, err
	}
	cert := common.StakeVoteRegistrationDelegationCertificate{
		CertType:        uint(common.CertificateTypeStakeVoteRegistrationDelegation),
		StakeCredential: cred,
		PoolKeyHash:     poolHash,
		Drep:            drep,
		Amount:          coin,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypeStakeVoteRegistrationDelegation),
		Certificate: &cert,
	})
	return a, nil
}

// --- Pool Operations ---

// RegisterPool adds a pool registration certificate.
func (a *Apollo) RegisterPool(params common.PoolRegistrationCertificate) *Apollo {
	params.CertType = uint(common.CertificateTypePoolRegistration)
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypePoolRegistration),
		Certificate: &params,
	})
	return a
}

// DeregisterPool adds a pool retirement certificate.
func (a *Apollo) DeregisterPool(poolHash common.Blake2b224, epoch uint64) *Apollo {
	cert := common.PoolRetirementCertificate{
		CertType:    uint(common.CertificateTypePoolRetirement),
		PoolKeyHash: poolHash,
		Epoch:       epoch,
	}
	a.certificates = append(a.certificates, common.CertificateWrapper{
		Type:        uint(common.CertificateTypePoolRetirement),
		Certificate: &cert,
	})
	return a
}

// --- Withdrawals ---

// AddWithdrawal adds a staking reward withdrawal to the transaction.
// For script-based withdrawals, provide a redeemer and execution units.
func (a *Apollo) AddWithdrawal(address common.Address, amount uint64, redeemerData *common.Datum, exUnits *common.ExUnits) *Apollo {
	a.withdrawals[address.String()] = withdrawalEntry{Address: address, Amount: amount}
	if redeemerData != nil {
		skh := address.StakeKeyHash()
		key := hex.EncodeToString(skh.Bytes())
		entry := redeemerEntry{
			Tag:  common.RedeemerTagReward,
			Data: *redeemerData,
		}
		if exUnits != nil {
			entry.ExUnits = *exUnits
		}
		a.stakeRedeemers[key] = entry
		a.isEstimateRequired = true
	}
	return a
}

// --- Metadata ---

// SetShelleyMetadata sets transaction metadata from a key-value map.
func (a *Apollo) SetShelleyMetadata(metadata map[uint64]any) *Apollo {
	a.auxiliaryData = &auxData{metadata: metadata}
	return a
}

// --- Signing & Witness Methods ---

// AddVerificationKeyWitness adds a VKey witness to the transaction.
func (a *Apollo) AddVerificationKeyWitness(witness common.VkeyWitness) (*Apollo, error) {
	if a.tx == nil {
		return a, errors.New("transaction not built - call Complete() first")
	}
	var witnesses []common.VkeyWitness
	if existing := a.tx.WitnessSet.VkeyWitnesses.Items(); existing != nil {
		witnesses = existing
	}
	witnesses = append(witnesses, witness)
	a.tx.WitnessSet.VkeyWitnesses = cbor.NewSetType(witnesses, true)
	return a, nil
}

// SignWithSkey signs the transaction with raw vkey/skey bytes.
func (a *Apollo) SignWithSkey(vkey, skey []byte) (*Apollo, error) {
	if a.tx == nil {
		return a, errors.New("transaction not built - call Complete() first")
	}
	bodyCbor, err := cbor.Encode(&a.tx.Body)
	if err != nil {
		return a, fmt.Errorf("failed to encode tx body: %w", err)
	}
	a.tx.Body.SetCbor(bodyCbor)
	txHash := a.tx.Body.Id()

	if len(skey) < 32 {
		return a, errors.New("skey must be at least 32 bytes")
	}
	edKey := ed25519.NewKeyFromSeed(skey[:32])
	signature := ed25519.Sign(edKey, txHash.Bytes())

	witness := common.VkeyWitness{
		Vkey:      vkey,
		Signature: signature,
	}
	return a.AddVerificationKeyWitness(witness)
}

// --- Collateral ---

// SetCollateralAmount sets the target collateral amount.
func (a *Apollo) SetCollateralAmount(amount int64) *Apollo {
	a.collateralAmount = amount
	return a
}


// --- Transaction Loading & Utility Methods ---

// LoadTxCbor loads a transaction from hex-encoded CBOR.
func (a *Apollo) LoadTxCbor(txCbor string) (*Apollo, error) {
	txBytes, err := hex.DecodeString(txCbor)
	if err != nil {
		return a, fmt.Errorf("invalid hex: %w", err)
	}
	var tx conway.ConwayTransaction
	if _, err := cbor.Decode(txBytes, &tx); err != nil {
		return a, fmt.Errorf("failed to decode transaction: %w", err)
	}
	a.tx = &tx
	return a, nil
}

// Clone returns a deep copy of this Apollo builder.
func (a *Apollo) Clone() *Apollo {
	clone := &Apollo{
		Context:            a.Context,
		isEstimateRequired: a.isEstimateRequired,
		Fee:                a.Fee,
		FeePadding:         a.FeePadding,
		Ttl:                a.Ttl,
		ValidityStart:      a.ValidityStart,
		totalCollateral:    a.totalCollateral,
		collateralAmount:   a.collateralAmount,
		estimateExUnits:    a.estimateExUnits,
		wallet:             a.wallet,
		redeemers:          make(map[string]redeemerEntry),
		stakeRedeemers:     make(map[string]redeemerEntry),
		mintRedeemers:      make(map[string]redeemerEntry),
		withdrawals:        make(map[string]withdrawalEntry),
	}
	clone.payments = append(clone.payments, a.payments...)
	clone.utxos = append(clone.utxos, a.utxos...)
	clone.preselectedUtxos = append(clone.preselectedUtxos, a.preselectedUtxos...)
	clone.inputAddresses = append(clone.inputAddresses, a.inputAddresses...)
	clone.datums = append(clone.datums, a.datums...)
	clone.requiredSigners = append(clone.requiredSigners, a.requiredSigners...)
	clone.v1scripts = append(clone.v1scripts, a.v1scripts...)
	clone.v2scripts = append(clone.v2scripts, a.v2scripts...)
	clone.v3scripts = append(clone.v3scripts, a.v3scripts...)
	clone.mint = append(clone.mint, a.mint...)
	clone.collaterals = append(clone.collaterals, a.collaterals...)
	clone.referenceInputs = append(clone.referenceInputs, a.referenceInputs...)
	clone.nativescripts = append(clone.nativescripts, a.nativescripts...)
	clone.usedUtxos = append(clone.usedUtxos, a.usedUtxos...)
	clone.certificates = append(clone.certificates, a.certificates...)
	clone.scriptHashes = append(clone.scriptHashes, a.scriptHashes...)
	for k, v := range a.redeemers {
		clone.redeemers[k] = v
	}
	for k, v := range a.stakeRedeemers {
		clone.stakeRedeemers[k] = v
	}
	for k, v := range a.mintRedeemers {
		clone.mintRedeemers[k] = v
	}
	for k, v := range a.withdrawals {
		clone.withdrawals[k] = v
	}
	if a.changeAddress != nil {
		addr := *a.changeAddress
		clone.changeAddress = &addr
	}
	if a.collateralReturn != nil {
		cr := *a.collateralReturn
		clone.collateralReturn = &cr
	}
	if a.auxiliaryData != nil {
		clonedMeta := make(map[uint64]any, len(a.auxiliaryData.metadata))
		for k, v := range a.auxiliaryData.metadata {
			clonedMeta[k] = v
		}
		clone.auxiliaryData = &auxData{metadata: clonedMeta}
	}
	if a.tx != nil {
		txCopy := *a.tx
		clone.tx = &txCopy
	}
	return clone
}

// UtxoFromRef looks up a UTxO by transaction hash and index.
func (a *Apollo) UtxoFromRef(txHash string, txIndex int) (*common.Utxo, error) {
	hashBytes, err := hex.DecodeString(txHash)
	if err != nil {
		return nil, fmt.Errorf("invalid tx hash hex: %w", err)
	}
	if len(hashBytes) != common.Blake2b256Size {
		return nil, fmt.Errorf("invalid tx hash length: expected %d bytes, got %d", common.Blake2b256Size, len(hashBytes))
	}
	if txIndex < 0 || txIndex > math.MaxUint32 {
		return nil, fmt.Errorf("tx index must be 0-%d, got %d", math.MaxUint32, txIndex)
	}
	var hash common.Blake2b256
	copy(hash[:], hashBytes)
	return a.Context.UtxoByRef(hash, uint32(txIndex))
}

// GetUsedUTxOs returns the list of used UTxO references.
func (a *Apollo) GetUsedUTxOs() []string {
	return a.usedUtxos
}

// GetBurns returns the total minted/burned value.
func (a *Apollo) GetBurns() (Value, error) {
	return a.mintValue()
}

// GetWallet returns the current wallet.
func (a *Apollo) GetWallet() Wallet {
	return a.wallet
}

// Complete performs coin selection, fee estimation, and builds the transaction.
func (a *Apollo) Complete() (*Apollo, error) {
	if a.tx != nil {
		return a, errors.New("transaction already built - call Complete() only once")
	}
	if a.wallet == nil {
		return a, errors.New("wallet is required to complete transaction")
	}

	// Load UTxOs from input addresses if needed (must happen before collateral selection)
	if err := a.loadUtxos(); err != nil {
		return a, err
	}

	// Auto-select collateral if needed (after UTxOs are loaded)
	a.setCollateral()

	// Build outputs from payments
	outputs, err := a.buildOutputs()
	if err != nil {
		return a, err
	}

	// Calculate total required value
	totalRequired, err := a.totalOutputValue(outputs)
	if err != nil {
		return a, err
	}

	// Adjust for certificate deposits using protocol parameter
	stakeDeposit := int64(StakeDeposit) // fallback
	if pp, ppErr := a.Context.ProtocolParams(); ppErr == nil {
		if d, dErr := strconv.ParseInt(pp.KeyDeposits, 10, 64); dErr == nil && d > 0 {
			stakeDeposit = d
		}
	}
	totalRequired = a.adjustForCertificateDeposits(totalRequired, stakeDeposit)

	// Add preselected UTxO value plus implicit inputs (withdrawals, mints)
	totalInput := a.totalPreselectedValue()
	if len(a.withdrawals) > 0 {
		totalInput, err = totalInput.Add(a.totalWithdrawalValue())
		if err != nil {
			return a, fmt.Errorf("withdrawal value overflow: %w", err)
		}
	}
	if a.hasMint() {
		mv, err := a.mintValue()
		if err != nil {
			return a, err
		}
		totalInput, err = totalInput.Add(mv)
		if err != nil {
			return a, fmt.Errorf("mint value overflow: %w", err)
		}
	}

	// Estimate a preliminary fee for coin selection so we don't under-select.
	// Use max fee as a conservative upper bound.
	prelimFee := int64(0)
	if maxFee, feeErr := a.Context.MaxTxFee(); feeErr == nil {
		prelimFee = int64(maxFee) //nolint:gosec // MaxTxFee fits in int64
	}
	selectionTarget, err := totalRequired.Add(NewSimpleValue(uint64(prelimFee)))
	if err != nil {
		return a, fmt.Errorf("selection target overflow: %w", err)
	}

	// Coin selection
	selectedUtxos, err := a.selectCoins(selectionTarget, totalInput)
	if err != nil {
		return a, fmt.Errorf("coin selection failed: %w", err)
	}

	// Build inputs (explicit allocation to avoid slice aliasing)
	allInputUtxos := make([]common.Utxo, 0, len(a.preselectedUtxos)+len(selectedUtxos))
	allInputUtxos = append(allInputUtxos, a.preselectedUtxos...)
	allInputUtxos = append(allInputUtxos, selectedUtxos...)
	allInputUtxos = SortInputs(allInputUtxos)

	// Automatic ExUnit estimation for script transactions
	if a.isEstimateRequired && a.estimateExUnits {
		if err := a.estimateExecutionUnits(allInputUtxos, outputs); err != nil {
			return a, fmt.Errorf("ExUnit estimation failed: %w", err)
		}
	}

	// Estimate fee
	fee, err := a.estimateFee(allInputUtxos, outputs)
	if err != nil {
		return a, fmt.Errorf("fee estimation failed: %w", err)
	}
	if a.Fee > 0 {
		fee = a.Fee
	}
	fee += a.FeePadding
	if fee < 0 {
		fee = 0
	}

	// Build change output
	totalInput = a.sumUtxoValues(allInputUtxos)
	if a.hasMint() {
		mv, err := a.mintValue()
		if err != nil {
			return a, err
		}
		totalInput, err = totalInput.Add(mv)
		if err != nil {
			return a, err
		}
	}
	// Withdrawals are implicit inputs in Cardano's balance equation
	if len(a.withdrawals) > 0 {
		totalInput, err = totalInput.Add(a.totalWithdrawalValue())
		if err != nil {
			return a, fmt.Errorf("withdrawal value overflow: %w", err)
		}
	}

	// Calculate change: totalRequired already includes deposits from adjustForCertificateDeposits.
	// Deposit refunds go to the rewards account, not the transaction.
	feeValue := NewSimpleValue(uint64(fee))
	totalNeeded, err := totalRequired.Add(feeValue)
	if err != nil {
		return a, fmt.Errorf("required value overflow: %w", err)
	}
	changeValue, err := totalInput.Sub(totalNeeded)
	if err != nil {
		return a, fmt.Errorf("insufficient funds: %w", err)
	}

	changeAddr := a.getChangeAddress()

	if changeValue.Coin > 0 || changeValue.HasAssets() {
		changeOutput := NewBabbageOutput(changeAddr, changeValue, nil, nil)
		pp, err := a.Context.ProtocolParams()
		if err != nil {
			return a, fmt.Errorf("failed to get protocol params for change output: %w", err)
		}
		minChange, err := MinLovelacePostAlonzo(&changeOutput, pp.CoinsPerUtxoByteValue())
		if err != nil {
			return a, fmt.Errorf("failed to compute min UTxO for change output: %w", err)
		}
		if int64(changeValue.Coin) >= minChange {
			outputs = append(outputs, changeOutput)
		} else if changeValue.HasAssets() {
			// Change has assets but insufficient ADA for min UTxO.
			// The shortfall must come from available inputs.
			shortfall := uint64(minChange) - changeValue.Coin
			changeValue.Coin = uint64(minChange)
			changeOutput = NewBabbageOutput(changeAddr, changeValue, nil, nil)
			actualMin, err := MinLovelacePostAlonzo(&changeOutput, pp.CoinsPerUtxoByteValue())
			if err != nil {
				return a, fmt.Errorf("failed to compute actual min UTxO for change output: %w", err)
			}
			if actualMin > minChange {
				shortfall += uint64(actualMin) - uint64(minChange)
				changeValue.Coin = uint64(actualMin)
				changeOutput = NewBabbageOutput(changeAddr, changeValue, nil, nil)
			}
			// Verify the shortfall is covered by total input
			totalInputCoin := totalInput.Coin
			totalOutputCoin := uint64(0)
			for _, out := range outputs {
				totalOutputCoin += out.OutputAmount.Amount
			}
			totalOutputCoin += changeValue.Coin + uint64(fee)
			if totalOutputCoin > totalInputCoin {
				return a, fmt.Errorf("insufficient funds: need %d more lovelace for change output min UTxO", totalOutputCoin-totalInputCoin)
			}
			_ = shortfall // verified via balance check above
			outputs = append(outputs, changeOutput)
		}
		// If change is ADA-only but below min UTxO and has no assets,
		// it is too small to create an output -- absorbed as additional fee.
	}

	// Build transaction body
	body, err := a.buildBody(allInputUtxos, outputs, uint64(fee))
	if err != nil {
		return a, err
	}

	// Build witness set
	witnessSet := a.buildWitnessSet(allInputUtxos)

	// Assemble transaction
	a.tx = &conway.ConwayTransaction{
		Body:       body,
		WitnessSet: witnessSet,
		TxIsValid:  true,
	}

	// Set metadata if present
	if a.auxiliaryData != nil {
		md := a.buildMetadata()
		if md != nil {
			a.tx.TxMetadata = md
		}
	}

	return a, nil
}

// Sign signs the transaction with the wallet.
func (a *Apollo) Sign() (*Apollo, error) {
	if a.tx == nil {
		return a, errors.New("transaction not built - call Complete() first")
	}
	if a.wallet == nil {
		return a, errors.New("no wallet set")
	}

	// Marshal body to CBOR and set it so Id() works
	bodyCbor, err := cbor.Encode(&a.tx.Body)
	if err != nil {
		return a, fmt.Errorf("failed to encode tx body: %w", err)
	}
	a.tx.Body.SetCbor(bodyCbor)

	txHash := a.tx.Body.Id()

	witness, err := a.wallet.SignTxBody(txHash)
	if err != nil {
		return a, fmt.Errorf("signing failed: %w", err)
	}

	var witnesses []common.VkeyWitness
	if existing := a.tx.WitnessSet.VkeyWitnesses.Items(); existing != nil {
		witnesses = existing
	}
	witnesses = append(witnesses, witness)
	a.tx.WitnessSet.VkeyWitnesses = cbor.NewSetType(witnesses, true)
	return a, nil
}

// GetTx returns the built transaction.
func (a *Apollo) GetTx() *conway.ConwayTransaction {
	return a.tx
}

// GetTxCbor returns the CBOR-encoded transaction.
func (a *Apollo) GetTxCbor() ([]byte, error) {
	if a.tx == nil {
		return nil, errors.New("no transaction built")
	}
	return cbor.Encode(a.tx)
}

// Submit submits the transaction to the chain.
func (a *Apollo) Submit() (common.Blake2b256, error) {
	txCbor, err := a.GetTxCbor()
	if err != nil {
		return common.Blake2b256{}, err
	}
	return a.Context.SubmitTx(txCbor)
}

// --- internal helpers ---

func (a *Apollo) loadUtxos() error {
	for _, addr := range a.inputAddresses {
		utxos, err := a.Context.Utxos(addr)
		if err != nil {
			return fmt.Errorf("failed to load UTxOs for %s: %w", addr.String(), err)
		}
		a.utxos = append(a.utxos, utxos...)
	}
	// If no UTxOs loaded and wallet is set, load from wallet address
	if len(a.utxos) == 0 && len(a.preselectedUtxos) == 0 && a.wallet != nil {
		utxos, err := a.Context.Utxos(a.wallet.Address())
		if err != nil {
			return fmt.Errorf("failed to load wallet UTxOs: %w", err)
		}
		a.utxos = utxos
	}
	return nil
}

func (a *Apollo) buildOutputs() ([]babbage.BabbageTransactionOutput, error) {
	outputs := make([]babbage.BabbageTransactionOutput, 0, len(a.payments))
	for _, payment := range a.payments {
		if err := payment.EnsureMinUTXO(a.Context); err != nil {
			return nil, fmt.Errorf("failed to ensure min UTxO: %w", err)
		}
		txOut, err := payment.ToTxOut()
		if err != nil {
			return nil, fmt.Errorf("failed to build payment output: %w", err)
		}
		outputs = append(outputs, *txOut)
	}
	return outputs, nil
}

func (a *Apollo) totalOutputValue(outputs []babbage.BabbageTransactionOutput) (Value, error) {
	total := Value{}
	for _, out := range outputs {
		var err error
		total, err = total.Add(ValueFromMaryValue(out.OutputAmount))
		if err != nil {
			return Value{}, fmt.Errorf("output value overflow: %w", err)
		}
	}
	return total, nil
}

func (a *Apollo) totalPreselectedValue() Value {
	return a.sumUtxoValues(a.preselectedUtxos)
}

func (a *Apollo) sumUtxoValues(utxos []common.Utxo) Value {
	total := Value{}
	for _, utxo := range utxos {
		amt := utxo.Output.Amount()
		if amt != nil {
			sum := total.Coin + amt.Uint64()
			if sum < total.Coin {
				// Overflow: saturate at max uint64
				total.Coin = math.MaxUint64
			} else {
				total.Coin = sum
			}
		}
		if utxo.Output.Assets() != nil {
			if total.Assets == nil {
				total.Assets = CloneMultiAsset(utxo.Output.Assets())
			} else {
				total.Assets.Add(utxo.Output.Assets())
			}
		}
	}
	return total
}

func (a *Apollo) selectCoins(required, currentInput Value) ([]common.Utxo, error) {
	if currentInput.GreaterOrEqual(required) {
		return nil, nil
	}

	remaining, err := required.Sub(currentInput)
	if err != nil {
		return nil, fmt.Errorf("failed to compute remaining required value: %w", err)
	}
	sorted := SortUtxos(a.utxos)
	var selected []common.Utxo

	for _, utxo := range sorted {
		ref := utxoRef(utxo)
		if a.isUsed(ref) {
			continue
		}

		selected = append(selected, utxo)
		a.usedUtxos = append(a.usedUtxos, ref)

		amt := utxo.Output.Amount()
		if amt != nil {
			if remaining.Coin <= amt.Uint64() {
				remaining.Coin = 0
			} else {
				remaining.Coin -= amt.Uint64()
			}
		}

		// Subtract assets from remaining
		if remaining.Assets != nil && utxo.Output.Assets() != nil {
			subtractAssetsSaturating(remaining.Assets, utxo.Output.Assets())
		}

		if remaining.Coin == 0 && !remaining.HasAssets() {
			return selected, nil
		}
	}

	if remaining.Coin > 0 || remaining.HasAssets() {
		return nil, errors.New("insufficient UTxOs to cover required value")
	}
	return selected, nil
}

func (a *Apollo) estimateFee(inputs []common.Utxo, outputs []babbage.BabbageTransactionOutput) (int64, error) {
	pp, err := a.Context.ProtocolParams()
	if err != nil {
		return 0, err
	}

	// Build a dummy transaction to estimate size
	body, err := a.buildBody(inputs, outputs, 0)
	if err != nil {
		return 0, err
	}
	ws := a.buildWitnessSet(inputs)
	// Add fake vkey witnesses for size estimation (1 for wallet + 1 per required signer)
	witnessCount := 1 + len(a.requiredSigners)
	fakeWitnesses := make([]common.VkeyWitness, witnessCount)
	for i := range fakeWitnesses {
		fakeWitnesses[i] = common.VkeyWitness{
			Vkey:      make([]byte, 32),
			Signature: make([]byte, 64),
		}
	}
	ws.VkeyWitnesses = cbor.NewSetType(fakeWitnesses, true)

	dummyTx := conway.ConwayTransaction{
		Body:       body,
		WitnessSet: ws,
		TxIsValid:  true,
	}

	txBytes, err := cbor.Encode(&dummyTx)
	if err != nil {
		return 0, fmt.Errorf("failed to encode dummy tx: %w", err)
	}

	txSize := len(txBytes)
	fee := int64(txSize)*pp.MinFeeCoefficient + pp.MinFeeConstant

	// Add execution unit costs for script transactions.
	// fee += priceMem * totalExMem + priceStep * totalExSteps
	redeemerMap := a.buildRedeemerMap(inputs)
	if len(redeemerMap) > 0 {
		var totalMem, totalSteps int64
		for _, rv := range redeemerMap {
			totalMem += rv.ExUnits.Memory
			totalSteps += rv.ExUnits.Steps
		}
		exUnitFee := int64(pp.PriceMem*float32(totalMem) + pp.PriceStep*float32(totalSteps))
		fee += exUnitFee
	}

	return fee, nil
}

// estimateExecutionUnits builds a preliminary transaction and evaluates it
// against the chain to get actual execution units for script redeemers.
// The returned ExUnits include a buffer for safety.
func (a *Apollo) estimateExecutionUnits(inputs []common.Utxo, outputs []babbage.BabbageTransactionOutput) error {
	// Build preliminary tx with current (possibly zero) ExUnits
	body, err := a.buildBody(inputs, outputs, 0)
	if err != nil {
		return fmt.Errorf("failed to build preliminary tx body: %w", err)
	}
	ws := a.buildWitnessSet(inputs)

	// Add fake vkey witnesses for a realistic tx
	witnessCount := 1 + len(a.requiredSigners)
	fakeWitnesses := make([]common.VkeyWitness, witnessCount)
	for i := range fakeWitnesses {
		fakeWitnesses[i] = common.VkeyWitness{
			Vkey:      make([]byte, 32),
			Signature: make([]byte, 64),
		}
	}
	ws.VkeyWitnesses = cbor.NewSetType(fakeWitnesses, true)

	prelimTx := conway.ConwayTransaction{
		Body:       body,
		WitnessSet: ws,
		TxIsValid:  true,
	}
	txBytes, err := cbor.Encode(&prelimTx)
	if err != nil {
		return fmt.Errorf("failed to encode preliminary tx: %w", err)
	}

	evalResult, err := a.Context.EvaluateTx(txBytes)
	if err != nil {
		return fmt.Errorf("EvaluateTx failed: %w", err)
	}

	// Update redeemers with evaluated ExUnits + buffer
	for evalKey, evalUnits := range evalResult {
		bufferedUnits := common.ExUnits{
			Memory: int64(float64(evalUnits.Memory) * (1 + ExMemoryBuffer)),
			Steps:  int64(float64(evalUnits.Steps) * (1 + ExStepBuffer)),
		}
		switch evalKey.Tag {
		case common.RedeemerTagSpend:
			// Find the spending redeemer for this input index
			if int(evalKey.Index) < len(inputs) {
				ref := utxoRef(inputs[evalKey.Index])
				if entry, ok := a.redeemers[ref]; ok {
					entry.ExUnits = bufferedUnits
					a.redeemers[ref] = entry
				}
			}
		case common.RedeemerTagMint:
			sortedPolicies := a.sortedMintPolicyIds()
			if int(evalKey.Index) < len(sortedPolicies) {
				policyHex := sortedPolicies[evalKey.Index]
				if entry, ok := a.mintRedeemers[policyHex]; ok {
					entry.ExUnits = bufferedUnits
					a.mintRedeemers[policyHex] = entry
				}
			}
		case common.RedeemerTagReward:
			sortedWdAddrs := a.sortedWithdrawalKeys()
			if int(evalKey.Index) < len(sortedWdAddrs) {
				addrKey := sortedWdAddrs[evalKey.Index]
				wd := a.withdrawals[addrKey]
				skhHex := hex.EncodeToString(wd.Address.StakeKeyHash().Bytes())
				if entry, ok := a.stakeRedeemers[skhHex]; ok {
					entry.ExUnits = bufferedUnits
					a.stakeRedeemers[skhHex] = entry
				}
			}
		}
	}

	return nil
}

func (a *Apollo) buildBody(
	inputs []common.Utxo,
	outputs []babbage.BabbageTransactionOutput,
	fee uint64,
) (conway.ConwayTransactionBody, error) {
	// Build input set
	txInputs := make([]shelley.ShelleyTransactionInput, 0, len(inputs))
	for _, utxo := range inputs {
		txId := utxo.Id.Id()
		idx := utxo.Id.Index()
		input := shelley.ShelleyTransactionInput{
			TxId:        txId,
			OutputIndex: idx,
		}
		txInputs = append(txInputs, input)
	}

	inputSet := conway.NewConwayTransactionInputSet(txInputs)

	body := conway.ConwayTransactionBody{
		TxInputs:  inputSet,
		TxOutputs: outputs,
		TxFee:     fee,
	}

	if a.Ttl > 0 {
		body.Ttl = uint64(a.Ttl)
	}
	if a.ValidityStart > 0 {
		body.TxValidityIntervalStart = uint64(a.ValidityStart)
	}

	// Mint
	if a.hasMint() {
		mintAsset, err := a.buildMintAsset()
		if err != nil {
			return body, err
		}
		body.TxMint = mintAsset
	}

	// Required signers
	if len(a.requiredSigners) > 0 {
		body.TxRequiredSigners = cbor.NewSetType(a.requiredSigners, true)
	}

	// Reference inputs
	if len(a.referenceInputs) > 0 {
		body.TxReferenceInputs = cbor.NewSetType(a.referenceInputs, true)
	}

	// Certificates
	if len(a.certificates) > 0 {
		body.TxCertificates = a.certificates
	}

	// Withdrawals
	if len(a.withdrawals) > 0 {
		wdMap := make(map[*common.Address]uint64, len(a.withdrawals))
		for _, wd := range a.withdrawals {
			addr := wd.Address
			wdMap[&addr] = wd.Amount
		}
		body.TxWithdrawals = wdMap
	}

	// Auxiliary data hash
	if a.auxiliaryData != nil {
		auxHash, auxErr := a.computeAuxDataHash()
		if auxErr != nil {
			return body, fmt.Errorf("failed to compute aux data hash: %w", auxErr)
		}
		body.TxAuxDataHash = auxHash
	}

	// Collateral
	if len(a.collaterals) > 0 {
		collInputs := make([]shelley.ShelleyTransactionInput, 0, len(a.collaterals))
		for _, utxo := range a.collaterals {
			txId := utxo.Id.Id()
			idx := utxo.Id.Index()
			collInputs = append(collInputs, shelley.ShelleyTransactionInput{
				TxId:        txId,
				OutputIndex: idx,
			})
		}
		body.TxCollateral = cbor.NewSetType(collInputs, true)
		if a.totalCollateral > 0 {
			body.TxTotalCollateral = uint64(a.totalCollateral)
		}
		if a.collateralReturn != nil {
			body.TxCollateralReturn = a.collateralReturn
		}
	}

	// Script data hash
	if len(a.redeemers) > 0 || len(a.mintRedeemers) > 0 || len(a.stakeRedeemers) > 0 || len(a.datums) > 0 {
		pp, err := a.Context.ProtocolParams()
		if err != nil {
			return body, err
		}
		redeemerMap := a.buildRedeemerMap(inputs)
		hash, err := ComputeScriptDataHash(redeemerMap, a.datums, pp.CostModels)
		if err != nil {
			return body, err
		}
		body.TxScriptDataHash = hash
	}

	// Network ID
	netId := a.Context.NetworkId()
	body.TxNetworkId = &netId

	return body, nil
}

func (a *Apollo) buildWitnessSet(inputs []common.Utxo) conway.ConwayTransactionWitnessSet {
	ws := conway.ConwayTransactionWitnessSet{}

	if len(a.v1scripts) > 0 {
		ws.WsPlutusV1Scripts = cbor.NewSetType(a.v1scripts, true)
	}
	if len(a.v2scripts) > 0 {
		ws.WsPlutusV2Scripts = cbor.NewSetType(a.v2scripts, true)
	}
	if len(a.v3scripts) > 0 {
		ws.WsPlutusV3Scripts = cbor.NewSetType(a.v3scripts, true)
	}
	if len(a.nativescripts) > 0 {
		ws.WsNativeScripts = cbor.NewSetType(a.nativescripts, true)
	}
	if len(a.datums) > 0 {
		ws.WsPlutusData = cbor.NewSetType(a.datums, true)
	}

	redeemerMap := a.buildRedeemerMap(inputs)
	if len(redeemerMap) > 0 {
		ws.WsRedeemers = conway.ConwayRedeemers{
			Redeemers: redeemerMap,
		}
	}

	return ws
}

func (a *Apollo) buildRedeemerMap(inputs []common.Utxo) map[common.RedeemerKey]common.RedeemerValue {
	result := make(map[common.RedeemerKey]common.RedeemerValue)

	// Spending redeemers - index based on sorted input position
	for ref, entry := range a.redeemers {
		found := false
		idx := uint32(0)
		for i, utxo := range inputs {
			if utxoRef(utxo) == ref {
				idx = uint32(i)
				found = true
				break
			}
		}
		if !found {
			continue
		}
		key := common.RedeemerKey{Tag: entry.Tag, Index: idx}
		result[key] = common.RedeemerValue{Data: entry.Data, ExUnits: entry.ExUnits}
	}

	// Mint redeemers - index based on sorted policy ID position in mint field
	if len(a.mintRedeemers) > 0 {
		sortedPolicies := a.sortedMintPolicyIds()
		for policyHex, entry := range a.mintRedeemers {
			found := false
			idx := uint32(0)
			for i, p := range sortedPolicies {
				if p == policyHex {
					idx = uint32(i)
					found = true
					break
				}
			}
			if !found {
				continue
			}
			key := common.RedeemerKey{Tag: common.RedeemerTagMint, Index: idx}
			result[key] = common.RedeemerValue{Data: entry.Data, ExUnits: entry.ExUnits}
		}
	}

	// Stake redeemers - index based on sorted withdrawal address position
	if len(a.stakeRedeemers) > 0 {
		sortedWdAddrs := a.sortedWithdrawalKeys()
		for skhHex, entry := range a.stakeRedeemers {
			found := false
			idx := uint32(0)
			for i, addrKey := range sortedWdAddrs {
				wd := a.withdrawals[addrKey]
				addrSKH := hex.EncodeToString(wd.Address.StakeKeyHash().Bytes())
				if addrSKH == skhHex {
					idx = uint32(i)
					found = true
					break
				}
			}
			if !found {
				continue
			}
			key := common.RedeemerKey{Tag: common.RedeemerTagReward, Index: idx}
			result[key] = common.RedeemerValue{Data: entry.Data, ExUnits: entry.ExUnits}
		}
	}

	return result
}

// sortedMintPolicyIds returns unique policy IDs from mint units in sorted order.
func (a *Apollo) sortedMintPolicyIds() []string {
	seen := make(map[string]bool)
	var policies []string
	for _, unit := range a.mint {
		if !seen[unit.PolicyId] {
			seen[unit.PolicyId] = true
			policies = append(policies, unit.PolicyId)
		}
	}
	sort.Strings(policies)
	return policies
}

// sortedWithdrawalKeys returns withdrawal map keys sorted by raw address bytes.
// This matches CBOR canonical ordering used by the Cardano ledger for redeemer indices.
// totalWithdrawalValue returns the total lovelace from all withdrawals.
func (a *Apollo) totalWithdrawalValue() Value {
	var total uint64
	for _, wd := range a.withdrawals {
		sum := total + wd.Amount
		if sum < total {
			// Overflow: saturate at max uint64
			total = math.MaxUint64
			break
		}
		total = sum
	}
	return NewSimpleValue(total)
}

func (a *Apollo) sortedWithdrawalKeys() []string {
	keys := make([]string, 0, len(a.withdrawals))
	for k := range a.withdrawals {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		addrI := a.withdrawals[keys[i]].Address
		addrJ := a.withdrawals[keys[j]].Address
		bytesI, _ := addrI.Bytes()
		bytesJ, _ := addrJ.Bytes()
		return bytes.Compare(bytesI, bytesJ) < 0
	})
	return keys
}

func (a *Apollo) hasMint() bool {
	return len(a.mint) > 0
}

func (a *Apollo) mintValue() (Value, error) {
	total := Value{}
	for _, unit := range a.mint {
		var err error
		total, err = total.Add(unit.ToValue())
		if err != nil {
			return Value{}, fmt.Errorf("mint value overflow: %w", err)
		}
	}
	return total, nil
}

func (a *Apollo) buildMintAsset() (*common.MultiAsset[common.MultiAssetTypeMint], error) {
	data := make(map[common.Blake2b224]map[cbor.ByteString]*big.Int)
	for _, unit := range a.mint {
		policyBytes, err := hex.DecodeString(unit.PolicyId)
		if err != nil {
			return nil, fmt.Errorf("invalid mint policy ID hex %q: %w", unit.PolicyId, err)
		}
		if len(policyBytes) != common.Blake2b224Size {
			return nil, fmt.Errorf("invalid policy ID length for %q: expected %d bytes, got %d", unit.PolicyId, common.Blake2b224Size, len(policyBytes))
		}
		var policyId common.Blake2b224
		copy(policyId[:], policyBytes)

		nameBytes, err := hex.DecodeString(unit.Name)
		if err != nil {
			nameBytes = []byte(unit.Name)
		}

		if _, ok := data[policyId]; !ok {
			data[policyId] = make(map[cbor.ByteString]*big.Int)
		}
		key := cbor.NewByteString(nameBytes)
		if existing, ok := data[policyId][key]; ok {
			data[policyId][key] = new(big.Int).Add(existing, big.NewInt(unit.Quantity))
		} else {
			data[policyId][key] = big.NewInt(unit.Quantity)
		}
	}
	result := common.NewMultiAsset[common.MultiAssetTypeMint](data)
	return &result, nil
}

func (a *Apollo) isUsed(ref string) bool {
	for _, used := range a.usedUtxos {
		if used == ref {
			return true
		}
	}
	// Also check preselected
	for _, utxo := range a.preselectedUtxos {
		if utxoRef(utxo) == ref {
			return true
		}
	}
	return false
}

func utxoRef(utxo common.Utxo) string {
	return hex.EncodeToString(utxo.Id.Id().Bytes()) + "#" + strconv.Itoa(int(utxo.Id.Index()))
}

// getChangeAddress returns the change address (explicit or wallet).
func (a *Apollo) getChangeAddress() common.Address {
	if a.changeAddress != nil {
		return *a.changeAddress
	}
	return a.wallet.Address()
}

// hasScripts returns true if any Plutus scripts are attached.
func (a *Apollo) hasScripts() bool {
	return len(a.v1scripts) > 0 || len(a.v2scripts) > 0 || len(a.v3scripts) > 0
}

// setCollateral auto-selects collateral from UTxOs if needed.
func (a *Apollo) setCollateral() {
	if len(a.collaterals) > 0 || !a.hasScripts() {
		return
	}
	// Find ADA-only UTxOs from loaded or wallet UTxOs
	minCollateral := int64(5_000_000)
	if a.collateralAmount > 0 {
		minCollateral = a.collateralAmount
	}

	candidates := a.utxos
	if len(candidates) == 0 && a.wallet != nil {
		loaded, err := a.Context.Utxos(a.wallet.Address())
		if err == nil {
			candidates = loaded
		}
	}

	for _, utxo := range candidates {
		if a.isUsed(utxoRef(utxo)) {
			continue
		}
		if utxo.Output.Assets() != nil {
			continue
		}
		amt := utxo.Output.Amount()
		if amt != nil && amt.Int64() >= minCollateral {
			a.collaterals = append(a.collaterals, utxo)
			a.usedUtxos = append(a.usedUtxos, utxoRef(utxo))
			a.totalCollateral = minCollateral
			// Build collateral return for the remainder
			remainder := amt.Int64() - minCollateral
			if remainder > 0 && a.wallet != nil {
				returnVal := Value{Coin: uint64(remainder)}
				ret := NewBabbageOutput(a.wallet.Address(), returnVal, nil, nil)
				a.collateralReturn = &ret
			}
			return
		}
	}
}

// adjustForCertificateDeposits adjusts the total required value for certificate deposits.
func (a *Apollo) adjustForCertificateDeposits(required Value, depositPerCert int64) Value {
	adj := a.certificateDepositAdjustment(depositPerCert)
	if adj > 0 {
		required.Coin += uint64(adj)
	}
	// Refunds (adj < 0) go to the rewards account, not the transaction,
	// so they do not reduce totalRequired.
	return required
}

// certificateDepositAdjustment calculates the net deposit change from certificates.
// Positive means deposits needed, negative means refunds.
func (a *Apollo) certificateDepositAdjustment(depositPerCert int64) int64 {
	var adjustment int64
	for _, cert := range a.certificates {
		switch cert.Type {
		case uint(common.CertificateTypeStakeRegistration),
			uint(common.CertificateTypeRegistration),
			uint(common.CertificateTypeStakeRegistrationDelegation),
			uint(common.CertificateTypeVoteRegistrationDelegation),
			uint(common.CertificateTypeStakeVoteRegistrationDelegation):
			adjustment += depositPerCert
		case uint(common.CertificateTypeStakeDeregistration),
			uint(common.CertificateTypeDeregistration):
			adjustment -= depositPerCert
		}
	}
	return adjustment
}

// computeAuxDataHash computes the blake2b-256 hash of the CBOR-encoded auxiliary data.
// It must encode the same MetaMap structure used in the transaction to ensure the hash matches.
func (a *Apollo) computeAuxDataHash() (*common.Blake2b256, error) {
	if a.auxiliaryData == nil {
		return nil, nil
	}
	md := a.buildMetadata()
	if md == nil {
		return nil, nil
	}
	mdBytes, err := cbor.Encode(md)
	if err != nil {
		return nil, fmt.Errorf("failed to encode metadata: %w", err)
	}
	hash := common.Blake2b256Hash(mdBytes)
	return &hash, nil
}

// buildMetadata converts auxiliary data to a MetaMap with deterministic key ordering.
func (a *Apollo) buildMetadata() *common.MetaMap {
	if a.auxiliaryData == nil {
		return nil
	}
	// Sort keys for deterministic CBOR encoding (required for consistent hashing)
	keys := make([]uint64, 0, len(a.auxiliaryData.metadata))
	for k := range a.auxiliaryData.metadata {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	pairs := make([]common.MetaPair, 0, len(a.auxiliaryData.metadata))
	for _, k := range keys {
		v := a.auxiliaryData.metadata[k]
		key := common.MetaInt{Value: new(big.Int).SetUint64(k)}
		var val common.TransactionMetadatum
		switch tv := v.(type) {
		case string:
			val = common.MetaText{Value: tv}
		case int:
			val = common.MetaInt{Value: big.NewInt(int64(tv))}
		case int64:
			val = common.MetaInt{Value: big.NewInt(tv)}
		case uint64:
			val = common.MetaInt{Value: new(big.Int).SetUint64(tv)}
		case []byte:
			val = common.MetaBytes{Value: tv}
		default:
			val = common.MetaText{Value: fmt.Sprintf("%v", tv)}
		}
		pairs = append(pairs, common.MetaPair{Key: key, Value: val})
	}
	return &common.MetaMap{Pairs: pairs}
}
