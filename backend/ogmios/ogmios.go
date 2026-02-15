package ogmios

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/SundaeSwap-finance/kugo"
	ogmigo "github.com/SundaeSwap-finance/ogmigo/v6"
	"github.com/SundaeSwap-finance/ogmigo/v6/ouroboros/chainsync"
	"github.com/SundaeSwap-finance/ogmigo/v6/ouroboros/shared"
	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
	"github.com/blinklabs-io/gouroboros/ledger/mary"
	"github.com/blinklabs-io/gouroboros/ledger/shelley"

	"github.com/Salvionied/apollo/v2/backend"
)

// OgmiosChainContext implements backend.ChainContext using Ogmios + Kupo.
type OgmiosChainContext struct {
	ogmios    *ogmigo.Client
	kupo      *kugo.Client
	networkId uint8
}

// NewOgmiosChainContext creates a new Ogmios chain context.
func NewOgmiosChainContext(ogmiosClient *ogmigo.Client, kupoClient *kugo.Client, networkId uint8) *OgmiosChainContext {
	return &OgmiosChainContext{
		ogmios:    ogmiosClient,
		kupo:      kupoClient,
		networkId: networkId,
	}
}

func (o *OgmiosChainContext) ProtocolParams() (backend.ProtocolParameters, error) {
	ctx := context.Background()
	raw, err := o.ogmios.CurrentProtocolParameters(ctx)
	if err != nil {
		return backend.ProtocolParameters{}, err
	}

	var params ogmiosProtocolParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return backend.ProtocolParameters{}, fmt.Errorf("failed to parse protocol params: %w", err)
	}

	return params.toProtocolParams()
}

func (o *OgmiosChainContext) GenesisParams() (backend.GenesisParameters, error) {
	ctx := context.Background()
	raw, err := o.ogmios.GenesisConfig(ctx, "shelley")
	if err != nil {
		return backend.GenesisParameters{}, err
	}

	var genesis ogmiosGenesisConfig
	if err := json.Unmarshal(raw, &genesis); err != nil {
		return backend.GenesisParameters{}, err
	}

	return genesis.toGenesisParams(), nil
}

func (o *OgmiosChainContext) NetworkId() uint8 {
	return o.networkId
}

func (o *OgmiosChainContext) CurrentEpoch() (uint64, error) {
	ctx := context.Background()
	return o.ogmios.CurrentEpoch(ctx)
}

func (o *OgmiosChainContext) MaxTxFee() (uint64, error) {
	pp, err := o.ProtocolParams()
	if err != nil {
		return 0, err
	}
	return backend.ComputeMaxTxFee(pp)
}

func (o *OgmiosChainContext) Tip() (uint64, error) {
	ctx := context.Background()
	point, err := o.ogmios.ChainTip(ctx)
	if err != nil {
		return 0, err
	}
	ps, ok := point.PointStruct()
	if !ok || ps == nil {
		return 0, errors.New("chain tip is origin")
	}
	return ps.Slot, nil
}

func (o *OgmiosChainContext) Utxos(address common.Address) ([]common.Utxo, error) {
	ctx := context.Background()
	matches, err := o.kupo.Matches(ctx, kugo.OnlyUnspent(), kugo.Address(address.String()))
	if err != nil {
		return nil, err
	}

	var utxos []common.Utxo
	for _, match := range matches {
		utxo, err := matchToUtxo(match, address)
		if err != nil {
			continue
		}
		utxos = append(utxos, utxo)
	}
	return utxos, nil
}

func (o *OgmiosChainContext) SubmitTx(txCbor []byte) (common.Blake2b256, error) {
	ctx := context.Background()
	txHex := hex.EncodeToString(txCbor)
	resp, err := o.ogmios.SubmitTx(ctx, txHex)
	if err != nil {
		return common.Blake2b256{}, err
	}
	if resp.Error != nil {
		return common.Blake2b256{}, fmt.Errorf("submit tx error: %s", resp.Error.Message)
	}
	hashBytes, err := hex.DecodeString(resp.ID)
	if err != nil {
		return common.Blake2b256{}, err
	}
	if len(hashBytes) != common.Blake2b256Size {
		return common.Blake2b256{}, fmt.Errorf("invalid tx hash length: expected %d bytes, got %d", common.Blake2b256Size, len(hashBytes))
	}
	var result common.Blake2b256
	copy(result[:], hashBytes)
	return result, nil
}

func (o *OgmiosChainContext) EvaluateTx(txCbor []byte) (map[common.RedeemerKey]common.ExUnits, error) {
	ctx := context.Background()
	txHex := hex.EncodeToString(txCbor)
	resp, err := o.ogmios.EvaluateTx(ctx, txHex)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("evaluate tx error: %s", resp.Error.Message)
	}

	result := make(map[common.RedeemerKey]common.ExUnits)
	for _, eu := range resp.ExUnits {
		tag := backend.ParseRedeemerTag(eu.Validator.Purpose)
		key := common.RedeemerKey{Tag: tag, Index: uint32(eu.Validator.Index)}
		result[key] = common.ExUnits{Memory: int64(eu.Budget.Memory), Steps: int64(eu.Budget.Cpu)}
	}
	return result, nil
}

func (o *OgmiosChainContext) UtxoByRef(txHash common.Blake2b256, index uint32) (*common.Utxo, error) {
	ctx := context.Background()
	hashHex := hex.EncodeToString(txHash.Bytes())
	query := chainsync.TxInQuery{
		Transaction: shared.UtxoTxID{ID: hashHex},
		Index:       index,
	}
	utxos, err := o.ogmios.UtxosByTxIn(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(utxos) == 0 {
		return nil, errors.New("utxo not found")
	}

	raw := utxos[0]
	addr, err := common.NewAddress(raw.Address)
	if err != nil {
		return nil, err
	}
	result, err := ogmiosUtxoToCommon(raw, addr)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (o *OgmiosChainContext) ScriptCbor(scriptHash common.Blake2b224) ([]byte, error) {
	if o.kupo == nil {
		return nil, errors.New("kupo client required for script lookup")
	}
	ctx := context.Background()
	hashHex := hex.EncodeToString(scriptHash.Bytes())
	script, err := o.kupo.Script(ctx, hashHex)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(script.Script)
}

// --- Ogmios response types and conversion ---

type ogmiosProtocolParams struct {
	MinFeeCoefficient int64           `json:"minFeeCoefficient"`
	MinFeeConstant    ogmiosLovelace  `json:"minFeeConstant"`
	MaxBlockBodySize  ogmiosBytes     `json:"maxBlockBodySize"`
	MaxBlockHeaderSize ogmiosBytes    `json:"maxBlockHeaderSize"`
	MaxTxSize         ogmiosBytes     `json:"maxTransactionSize"`
	StakeKeyDeposit   ogmiosLovelace  `json:"stakeCredentialDeposit"`
	PoolDeposit       ogmiosLovelace  `json:"stakePoolDeposit"`
	MinPoolCost       ogmiosLovelace  `json:"minStakePoolCost"`
	CollateralPercent int             `json:"collateralPercentage"`
	MaxCollateral     int             `json:"maxCollateralInputs"`
	MaxValSize        ogmiosBytes     `json:"maxValueSize"`
	ScriptPrices      ogmiosPrices    `json:"scriptExecutionPrices"`
	MaxTxExUnits      ogmiosExUnits   `json:"maxExecutionUnitsPerTransaction"`
	MaxBlockExUnits   ogmiosExUnits   `json:"maxExecutionUnitsPerBlock"`
	MinUtxoDeposit    int64           `json:"minUtxoDepositCoefficient"`
	CostModels        json.RawMessage `json:"plutusCostModels"`
}

type ogmiosLovelace struct {
	Lovelace int64 `json:"lovelace"`
}

type ogmiosBytes struct {
	Bytes int `json:"bytes"`
}

type ogmiosPrices struct {
	Memory string `json:"memory"`
	CPU    string `json:"cpu"`
}

type ogmiosExUnits struct {
	Memory int64 `json:"memory"`
	CPU    int64 `json:"cpu"`
}


func (p *ogmiosProtocolParams) toProtocolParams() (backend.ProtocolParameters, error) {
	priceMem, err := backend.ParseFraction(p.ScriptPrices.Memory)
	if err != nil {
		return backend.ProtocolParameters{}, fmt.Errorf("invalid memory price: %w", err)
	}
	priceStep, err := backend.ParseFraction(p.ScriptPrices.CPU)
	if err != nil {
		return backend.ProtocolParameters{}, fmt.Errorf("invalid CPU price: %w", err)
	}

	pp := backend.ProtocolParameters{
		MinFeeConstant:      p.MinFeeConstant.Lovelace,
		MinFeeCoefficient:   p.MinFeeCoefficient,
		MaxBlockSize:        p.MaxBlockBodySize.Bytes,
		MaxTxSize:           p.MaxTxSize.Bytes,
		MaxBlockHeaderSize:  p.MaxBlockHeaderSize.Bytes,
		KeyDeposits:         strconv.FormatInt(p.StakeKeyDeposit.Lovelace, 10),
		PoolDeposits:        strconv.FormatInt(p.PoolDeposit.Lovelace, 10),
		MinPoolCost:         strconv.FormatInt(p.MinPoolCost.Lovelace, 10),
		PriceMem:            priceMem,
		PriceStep:           priceStep,
		MaxTxExMem:          strconv.FormatInt(p.MaxTxExUnits.Memory, 10),
		MaxTxExSteps:        strconv.FormatInt(p.MaxTxExUnits.CPU, 10),
		MaxBlockExMem:       strconv.FormatInt(p.MaxBlockExUnits.Memory, 10),
		MaxBlockExSteps:     strconv.FormatInt(p.MaxBlockExUnits.CPU, 10),
		MaxValSize:          strconv.Itoa(p.MaxValSize.Bytes),
		CollateralPercent:   p.CollateralPercent,
		MaxCollateralInputs: p.MaxCollateral,
		CoinsPerUtxoByte:    strconv.FormatInt(p.MinUtxoDeposit, 10),
	}

	// Parse cost models from Ogmios JSON.
	// Ogmios uses keys like "plutus:v1", "plutus:v2", "plutus:v3".
	// ComputeScriptDataHash expects "PlutusV1", "PlutusV2", "PlutusV3".
	if len(p.CostModels) > 0 {
		var rawModels map[string][]int64
		if err := json.Unmarshal(p.CostModels, &rawModels); err == nil {
			pp.CostModels = make(map[string][]int64, len(rawModels))
			for key, costs := range rawModels {
				pp.CostModels[ogmiosCostModelKey(key)] = costs
			}
		}
	}

	return pp, nil
}

// ogmiosCostModelKey translates Ogmios cost model keys to the canonical form
// expected by ComputeScriptDataHash ("PlutusV1", "PlutusV2", "PlutusV3").
func ogmiosCostModelKey(key string) string {
	switch key {
	case "plutus:v1":
		return "PlutusV1"
	case "plutus:v2":
		return "PlutusV2"
	case "plutus:v3":
		return "PlutusV3"
	default:
		return key
	}
}

type ogmiosGenesisConfig struct {
	NetworkMagic      int     `json:"networkMagic"`
	EpochLength       int     `json:"epochLength"`
	SlotLength        int     `json:"slotLength"`
	SlotsPerKesPeriod int     `json:"slotsPerKesPeriod"`
	MaxKesEvolutions  int     `json:"maxKESEvolutions"`
	SecurityParam     int     `json:"securityParameter"`
	UpdateQuorum      int     `json:"updateQuorum"`
	ActiveSlots       float64 `json:"activeSlotsCoefficient"`
	MaxLovelaceSupply int64   `json:"maxLovelaceSupply"`
}

func (g *ogmiosGenesisConfig) toGenesisParams() backend.GenesisParameters {
	return backend.GenesisParameters{
		ActiveSlotsCoefficient: float32(g.ActiveSlots),
		UpdateQuorum:           g.UpdateQuorum,
		NetworkMagic:           g.NetworkMagic,
		EpochLength:            g.EpochLength,
		MaxLovelaceSupply:      strconv.FormatInt(g.MaxLovelaceSupply, 10),
		SlotLength:             g.SlotLength,
		SlotsPerKesPeriod:      g.SlotsPerKesPeriod,
		MaxKesEvolutions:       g.MaxKesEvolutions,
		SecurityParam:          g.SecurityParam,
	}
}

func matchToUtxo(match kugo.Match, address common.Address) (common.Utxo, error) {
	hashBytes, err := hex.DecodeString(match.TransactionID)
	if err != nil {
		return common.Utxo{}, err
	}
	if len(hashBytes) != common.Blake2b256Size {
		return common.Utxo{}, fmt.Errorf("invalid tx hash length: expected %d bytes, got %d", common.Blake2b256Size, len(hashBytes))
	}
	var txId common.Blake2b256
	copy(txId[:], hashBytes)
	if match.OutputIndex < 0 {
		return common.Utxo{}, fmt.Errorf("negative output index: %d", match.OutputIndex)
	}
	return sharedValueToUtxo(txId, uint32(match.OutputIndex), shared.Value(match.Value), address)
}

func ogmiosUtxoToCommon(raw shared.Utxo, addr common.Address) (common.Utxo, error) {
	hashBytes, err := hex.DecodeString(raw.Transaction.ID)
	if err != nil {
		return common.Utxo{}, err
	}
	if len(hashBytes) != common.Blake2b256Size {
		return common.Utxo{}, fmt.Errorf("invalid tx hash length: expected %d bytes, got %d", common.Blake2b256Size, len(hashBytes))
	}
	var txId common.Blake2b256
	copy(txId[:], hashBytes)
	return sharedValueToUtxo(txId, raw.Index, raw.Value, addr)
}

func sharedValueToUtxo(txId common.Blake2b256, outputIndex uint32, value shared.Value, addr common.Address) (common.Utxo, error) {
	input := shelley.ShelleyTransactionInput{
		TxId:        txId,
		OutputIndex: outputIndex,
	}

	lovelace := value.AdaLovelace().Uint64()
	assetData := make(map[common.Blake2b224]map[cbor.ByteString]*big.Int)

	for policyIdStr, assets := range value {
		if policyIdStr == "ada" {
			continue
		}
		policyBytes, err := hex.DecodeString(policyIdStr)
		if err != nil {
			continue
		}
		if len(policyBytes) != common.Blake2b224Size {
			continue
		}
		var policyId common.Blake2b224
		copy(policyId[:], policyBytes)

		for assetName, qty := range assets {
			nameBytes, err := hex.DecodeString(assetName)
			if err != nil {
				nameBytes = []byte(assetName)
			}
			if _, ok := assetData[policyId]; !ok {
				assetData[policyId] = make(map[cbor.ByteString]*big.Int)
			}
			assetData[policyId][cbor.NewByteString(nameBytes)] = qty.BigInt()
		}
	}

	var assets *common.MultiAsset[common.MultiAssetTypeOutput]
	if len(assetData) > 0 {
		ma := common.NewMultiAsset[common.MultiAssetTypeOutput](assetData)
		assets = &ma
	}

	output := babbage.BabbageTransactionOutput{
		OutputAddress: addr,
		OutputAmount: mary.MaryTransactionOutputValue{
			Amount: lovelace,
			Assets: assets,
		},
	}

	return common.Utxo{
		Id:     input,
		Output: &output,
	}, nil
}

