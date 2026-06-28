package apollo

import (
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strconv"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
	"github.com/blinklabs-io/gouroboros/ledger/shelley"

	"github.com/Salvionied/apollo/v2/backend"
)

// BuildReport explains the transaction assembled by Complete or CompleteContext.
type BuildReport struct {
	TxHash       string
	TxSize       int
	CoinSelector string
	Inputs       InputReport
	Outputs      OutputReport
	Fees         FeeReport
	Balance      BalanceReport
	Collateral   CollateralReport
	Redeemers    []RedeemerReport
	Scripts      ScriptReport
}

// InputReport summarizes how transaction inputs were chosen.
type InputReport struct {
	InputAddresses     []string
	AvailableUtxoCount int
	Preselected        []UtxoReport
	Selected           []UtxoReport
	Final              []UtxoReport
	SelectionTarget    Value
}

// UtxoReport summarizes a resolved UTxO used while building a transaction.
type UtxoReport struct {
	Ref     string
	Address string
	Value   Value
}

// OutputReport summarizes requested, base, final, and change outputs.
type OutputReport struct {
	Requested   []TxOutputReport
	Base        []TxOutputReport
	Final       []TxOutputReport
	ChangeIndex int
	Change      Value
}

// TxOutputReport summarizes a transaction output without exposing ledger internals.
type TxOutputReport struct {
	Index           int
	Address         string
	Value           Value
	IsChange        bool
	MinLovelace     int64
	MinUtxoIncrease uint64
	HasInlineDatum  bool
	HasDatumHash    bool
	HasScriptRef    bool
}

// FeeReport summarizes fee inputs used by the builder.
type FeeReport struct {
	Final                  int64
	Size                   int64
	Padding                int64
	Forced                 bool
	Preliminary            int64
	ReferenceScriptReserve int64
	ReferenceScriptBytes   int
	ReferenceScriptFee     int64
	ExecutionUnits         common.ExUnits
	ExecutionUnitFee       int64
}

// BalanceReport summarizes the high-level Cardano balance equation components.
type BalanceReport struct {
	TotalInput         Value
	TotalRequired      Value
	TotalOutput        Value
	Change             Value
	Fee                int64
	Withdrawals        Value
	Mint               Value
	Burn               Value
	CertificateDeposit int64
	CertificateRefund  Value
	GovernanceRequired Value
	ProposalDeposit    uint64
	TreasuryDonation   uint64
}

// CollateralReport summarizes collateral inputs and return output.
type CollateralReport struct {
	Inputs       []UtxoReport
	Required     int64
	Total        int64
	Return       *TxOutputReport
	AutoSelected bool
	OverlapRef   string
}

// RedeemerReport summarizes a redeemer after final canonical indexing.
type RedeemerReport struct {
	Tag     common.RedeemerTag
	Index   uint32
	Source  string
	ExUnits common.ExUnits
}

// ScriptReport summarizes attached scripts and script-related transaction data.
type ScriptReport struct {
	PlutusV1           int
	PlutusV2           int
	PlutusV3           int
	Native             int
	Datums             int
	ReferenceInputs    int
	RequiredSigners    int
	ReferenceInputRefs []string
	ScriptHashes       []string
	RequiredSignerKeys []string
}

type buildReportSnapshot struct {
	requestedOutputs        []TxOutputReport
	baseOutputReports       []TxOutputReport
	finalOutputReports      []TxOutputReport
	preselectedUtxos        []common.Utxo
	selectedUtxos           []common.Utxo
	finalInputUtxos         []common.Utxo
	selectionTarget         Value
	totalInput              Value
	totalRequired           Value
	governanceRequired      Value
	refundValue             Value
	stakeDeposit            int64
	preliminaryFee          int64
	refScriptFeeReserve     int64
	refScriptSize           int
	refScriptFee            int64
	executionUnits          common.ExUnits
	executionUnitFee        int64
	feeSizeComponent        int64
	finalFee                int64
	collateralRequired      int64
	collateralReturn        *TxOutputReport
	redeemers               []RedeemerReport
	mintValue               Value
	burnValue               Value
	totalOutput             Value
	collateralAutoSelected  bool
	collateralOverlapRef    string
	changeOutputIndexOffset int
}

func (a *Apollo) newBuildReport(snapshot buildReportSnapshot) (*BuildReport, error) {
	if a.tx == nil {
		return nil, errors.New("transaction not built")
	}
	bodyCbor, err := cbor.Encode(&a.tx.Body)
	if err != nil {
		return nil, err
	}
	txCbor, err := cbor.Encode(a.tx)
	if err != nil {
		return nil, err
	}
	txHash := common.Blake2b256Hash(bodyCbor)

	redeemers := append([]RedeemerReport(nil), snapshot.redeemers...)
	depositAdjustment := a.certificateDepositAdjustment(snapshot.stakeDeposit)
	certificateDeposit := int64(0)
	if depositAdjustment > 0 {
		certificateDeposit = depositAdjustment
	}

	return &BuildReport{
		TxHash:       hex.EncodeToString(txHash.Bytes()),
		TxSize:       len(txCbor),
		CoinSelector: a.coinSelectorName(),
		Inputs: InputReport{
			InputAddresses:     addressStrings(a.inputAddresses),
			AvailableUtxoCount: len(a.utxos),
			Preselected:        utxoReportsFromUtxos(snapshot.preselectedUtxos),
			Selected:           utxoReportsFromUtxos(snapshot.selectedUtxos),
			Final:              utxoReportsFromUtxos(snapshot.finalInputUtxos),
			SelectionTarget:    snapshot.selectionTarget.Clone(),
		},
		Outputs: OutputReport{
			Requested:   cloneTxOutputReports(snapshot.requestedOutputs),
			Base:        cloneTxOutputReports(snapshot.baseOutputReports),
			Final:       cloneTxOutputReports(snapshot.finalOutputReports),
			ChangeIndex: snapshot.changeOutputIndex(),
			Change:      changeValueFromReports(snapshot.finalOutputReports, snapshot.changeOutputIndex()),
		},
		Fees: FeeReport{
			Final:                  snapshot.finalFee,
			Size:                   snapshot.feeSizeComponent,
			Padding:                a.FeePadding,
			Forced:                 a.forceFee || a.Fee > 0,
			Preliminary:            snapshot.preliminaryFee,
			ReferenceScriptReserve: snapshot.refScriptFeeReserve,
			ReferenceScriptBytes:   snapshot.refScriptSize,
			ReferenceScriptFee:     snapshot.refScriptFee,
			ExecutionUnits:         snapshot.executionUnits,
			ExecutionUnitFee:       snapshot.executionUnitFee,
		},
		Balance: BalanceReport{
			TotalInput:         snapshot.totalInput.Clone(),
			TotalRequired:      snapshot.totalRequired.Clone(),
			TotalOutput:        snapshot.totalOutput.Clone(),
			Change:             changeValueFromReports(snapshot.finalOutputReports, snapshot.changeOutputIndex()),
			Fee:                snapshot.finalFee,
			Withdrawals:        a.totalWithdrawalValue().Clone(),
			Mint:               snapshot.mintValue.Clone(),
			Burn:               snapshot.burnValue.Clone(),
			CertificateDeposit: certificateDeposit,
			CertificateRefund:  snapshot.refundValue.Clone(),
			GovernanceRequired: snapshot.governanceRequired.Clone(),
			ProposalDeposit:    a.proposalDepositTotal(),
			TreasuryDonation:   uint64(a.treasuryDonation), //nolint:gosec // validated non-negative by AddTreasuryDonation
		},
		Collateral: CollateralReport{
			Inputs:       utxoReportsFromUtxos(a.collaterals),
			Required:     snapshot.collateralRequired,
			Total:        a.totalCollateral,
			Return:       cloneTxOutputReportPtr(snapshot.collateralReturn),
			AutoSelected: snapshot.collateralAutoSelected,
			OverlapRef:   snapshot.collateralOverlapRef,
		},
		Redeemers: redeemers,
		Scripts: ScriptReport{
			PlutusV1:           len(a.v1scripts),
			PlutusV2:           len(a.v2scripts),
			PlutusV3:           len(a.v3scripts),
			Native:             len(a.nativescripts),
			Datums:             len(a.datums),
			ReferenceInputs:    len(a.referenceInputs),
			RequiredSigners:    len(a.requiredSigners),
			ReferenceInputRefs: referenceInputRefs(a.referenceInputs),
			ScriptHashes:       append([]string(nil), a.scriptHashes...),
			RequiredSignerKeys: requiredSignerKeys(a.requiredSigners),
		},
	}, nil
}

func (s buildReportSnapshot) changeOutputIndex() int {
	if len(s.finalOutputReports) > s.changeOutputIndexOffset {
		return s.changeOutputIndexOffset
	}
	return -1
}

func (a *Apollo) requestedOutputReports(coinsPerUtxoByte int64) ([]TxOutputReport, error) {
	reports := make([]TxOutputReport, 0, len(a.payments))
	for i, payment := range a.payments {
		txOut, err := payment.ToTxOut()
		if err != nil {
			return nil, err
		}
		report, err := outputReportFromTxOut(i, *txOut, false, coinsPerUtxoByte)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func (a *Apollo) coinSelectorName() string {
	if a.coinSelector != nil {
		return a.coinSelector.Name()
	}
	return defaultCoinSelector.Name()
}

func (a *Apollo) redeemerReports(inputs []common.Utxo) []RedeemerReport {
	redeemerMap := a.buildRedeemerMap(inputs)
	reports := make([]RedeemerReport, 0, len(redeemerMap))
	for key, value := range redeemerMap {
		reports = append(reports, RedeemerReport{
			Tag:     key.Tag,
			Index:   key.Index,
			Source:  a.redeemerSource(key, inputs),
			ExUnits: value.ExUnits,
		})
	}
	sortRedeemerReports(reports)
	return reports
}

func (a *Apollo) redeemerSource(key common.RedeemerKey, inputs []common.Utxo) string {
	switch key.Tag {
	case common.RedeemerTagSpend:
		if int(key.Index) < len(inputs) {
			return utxoRef(inputs[key.Index])
		}
	case common.RedeemerTagMint:
		policies := a.sortedMintPolicyIds()
		if int(key.Index) < len(policies) {
			return policies[key.Index]
		}
	case common.RedeemerTagReward:
		keys := a.sortedWithdrawalKeys()
		if int(key.Index) < len(keys) {
			return keys[key.Index]
		}
	}
	return ""
}

func outputReportsFromTxOuts(
	outputs []babbage.BabbageTransactionOutput,
	changeIndex int,
	coinsPerUtxoByte int64,
	requested []TxOutputReport,
) ([]TxOutputReport, error) {
	reports := make([]TxOutputReport, 0, len(outputs))
	for i, output := range outputs {
		report, err := outputReportFromTxOut(i, output, i == changeIndex, coinsPerUtxoByte)
		if err != nil {
			return nil, err
		}
		if i < len(requested) && report.Value.Coin > requested[i].Value.Coin {
			report.MinUtxoIncrease = report.Value.Coin - requested[i].Value.Coin
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func outputReportPtr(output *babbage.BabbageTransactionOutput, index int, isChange bool, coinsPerUtxoByte int64) (*TxOutputReport, error) {
	if output == nil {
		return nil, nil
	}
	report, err := outputReportFromTxOut(index, *output, isChange, coinsPerUtxoByte)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

func outputReportFromTxOut(index int, output babbage.BabbageTransactionOutput, isChange bool, coinsPerUtxoByte int64) (TxOutputReport, error) {
	minLovelace, err := MinLovelacePostAlonzo(&output, coinsPerUtxoByte)
	if err != nil {
		return TxOutputReport{}, err
	}
	report := TxOutputReport{
		Index:          index,
		Address:        output.OutputAddress.String(),
		Value:          ValueFromMaryValue(output.OutputAmount),
		IsChange:       isChange,
		MinLovelace:    minLovelace,
		HasInlineDatum: output.Datum() != nil,
		HasDatumHash:   output.DatumHash() != nil,
		HasScriptRef:   output.ScriptRef() != nil,
	}
	if minLovelace > 0 && report.Value.Coin < uint64(minLovelace) {
		report.MinUtxoIncrease = uint64(minLovelace) - report.Value.Coin //nolint:gosec // minLovelace > 0
	}
	return report, nil
}

func utxoReportsFromUtxos(utxos []common.Utxo) []UtxoReport {
	reports := make([]UtxoReport, 0, len(utxos))
	for _, utxo := range utxos {
		reports = append(reports, utxoReportFromUtxo(utxo))
	}
	return reports
}

func utxoReportFromUtxo(utxo common.Utxo) UtxoReport {
	report := UtxoReport{Ref: utxoRef(utxo)}
	if utxo.Output == nil {
		return report
	}
	report.Address = utxo.Output.Address().String()
	amount := utxo.Output.Amount()
	if amount != nil && amount.IsUint64() {
		report.Value.Coin = amount.Uint64()
	}
	report.Value.Assets = CloneMultiAsset(utxo.Output.Assets())
	return report
}

func changeValueFromReports(outputs []TxOutputReport, changeIndex int) Value {
	if changeIndex < 0 || changeIndex >= len(outputs) {
		return Value{}
	}
	return outputs[changeIndex].Value.Clone()
}

func addressStrings(addrs []common.Address) []string {
	result := make([]string, len(addrs))
	for i, addr := range addrs {
		result[i] = addr.String()
	}
	return result
}

func referenceInputRefs(inputs []shelley.ShelleyTransactionInput) []string {
	refs := make([]string, len(inputs))
	for i, input := range inputs {
		refs[i] = hex.EncodeToString(input.TxId.Bytes()) + "#" + strconv.Itoa(int(input.OutputIndex))
	}
	return refs
}

func requiredSignerKeys(signers []common.Blake2b224) []string {
	keys := make([]string, len(signers))
	for i, signer := range signers {
		keys[i] = hex.EncodeToString(signer.Bytes())
	}
	return keys
}

func totalRedeemerExUnits(redeemers []RedeemerReport) common.ExUnits {
	var total common.ExUnits
	for _, redeemer := range redeemers {
		total.Memory += redeemer.ExUnits.Memory
		total.Steps += redeemer.ExUnits.Steps
	}
	return total
}

func executionUnitFee(pp backend.ProtocolParameters, exUnits common.ExUnits) (int64, error) {
	if exUnits.Memory == 0 && exUnits.Steps == 0 {
		return 0, nil
	}
	fee := math.Ceil(float64(pp.PriceMem)*float64(exUnits.Memory) + float64(pp.PriceStep)*float64(exUnits.Steps))
	if !(fee >= 0 && fee < float64(math.MaxInt64)) {
		return 0, errors.New("execution unit fee out of range")
	}
	return int64(fee), nil
}

func feeSizeComponent(finalFee, padding, referenceScriptFee, executionUnitFee int64) int64 {
	sizeFee := finalFee - padding - referenceScriptFee - executionUnitFee
	if sizeFee < 0 {
		return 0
	}
	return sizeFee
}

func collateralRequired(fee int64, collateralPercent int) (int64, error) {
	if fee <= 0 || collateralPercent <= 0 {
		return 0, nil
	}
	if fee > (math.MaxInt64-99)/int64(collateralPercent) {
		return 0, errors.New("collateral requirement overflows")
	}
	return (fee*int64(collateralPercent) + 99) / 100, nil
}

func (a *Apollo) proposalDepositTotal() uint64 {
	var total uint64
	for _, proposal := range a.proposalProcedures {
		deposit := proposal.Deposit()
		if math.MaxUint64-total < deposit {
			return math.MaxUint64
		}
		total += deposit
	}
	return total
}

func sortRedeemerReports(reports []RedeemerReport) {
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Tag != reports[j].Tag {
			return reports[i].Tag < reports[j].Tag
		}
		if reports[i].Index != reports[j].Index {
			return reports[i].Index < reports[j].Index
		}
		return reports[i].Source < reports[j].Source
	})
}

func (r *BuildReport) clone() *BuildReport {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Inputs.InputAddresses = append([]string(nil), r.Inputs.InputAddresses...)
	cp.Inputs.Preselected = cloneUtxoReports(r.Inputs.Preselected)
	cp.Inputs.Selected = cloneUtxoReports(r.Inputs.Selected)
	cp.Inputs.Final = cloneUtxoReports(r.Inputs.Final)
	cp.Inputs.SelectionTarget = r.Inputs.SelectionTarget.Clone()
	cp.Outputs.Requested = cloneTxOutputReports(r.Outputs.Requested)
	cp.Outputs.Base = cloneTxOutputReports(r.Outputs.Base)
	cp.Outputs.Final = cloneTxOutputReports(r.Outputs.Final)
	cp.Outputs.Change = r.Outputs.Change.Clone()
	cp.Balance.TotalInput = r.Balance.TotalInput.Clone()
	cp.Balance.TotalRequired = r.Balance.TotalRequired.Clone()
	cp.Balance.TotalOutput = r.Balance.TotalOutput.Clone()
	cp.Balance.Change = r.Balance.Change.Clone()
	cp.Balance.Withdrawals = r.Balance.Withdrawals.Clone()
	cp.Balance.Mint = r.Balance.Mint.Clone()
	cp.Balance.Burn = r.Balance.Burn.Clone()
	cp.Balance.CertificateRefund = r.Balance.CertificateRefund.Clone()
	cp.Balance.GovernanceRequired = r.Balance.GovernanceRequired.Clone()
	cp.Collateral.Inputs = cloneUtxoReports(r.Collateral.Inputs)
	cp.Collateral.Return = cloneTxOutputReportPtr(r.Collateral.Return)
	cp.Redeemers = append([]RedeemerReport(nil), r.Redeemers...)
	cp.Scripts.ReferenceInputRefs = append([]string(nil), r.Scripts.ReferenceInputRefs...)
	cp.Scripts.ScriptHashes = append([]string(nil), r.Scripts.ScriptHashes...)
	cp.Scripts.RequiredSignerKeys = append([]string(nil), r.Scripts.RequiredSignerKeys...)
	return &cp
}

func cloneUtxoReports(reports []UtxoReport) []UtxoReport {
	if reports == nil {
		return nil
	}
	cp := make([]UtxoReport, len(reports))
	for i, report := range reports {
		cp[i] = report
		cp[i].Value = report.Value.Clone()
	}
	return cp
}

func cloneTxOutputReports(reports []TxOutputReport) []TxOutputReport {
	if reports == nil {
		return nil
	}
	cp := make([]TxOutputReport, len(reports))
	for i, report := range reports {
		cp[i] = report
		cp[i].Value = report.Value.Clone()
	}
	return cp
}

func cloneTxOutputReportPtr(report *TxOutputReport) *TxOutputReport {
	if report == nil {
		return nil
	}
	cp := *report
	cp.Value = report.Value.Clone()
	return &cp
}
