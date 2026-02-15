package maestro

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
	"github.com/blinklabs-io/gouroboros/ledger/mary"
	"github.com/blinklabs-io/gouroboros/ledger/shelley"
	maestroClient "github.com/maestro-org/go-sdk/client"
	"github.com/maestro-org/go-sdk/models"
	"github.com/maestro-org/go-sdk/utils"

	"github.com/Salvionied/apollo/v2/backend"
)

// MaestroChainContext implements backend.ChainContext using the Maestro API.
type MaestroChainContext struct {
	client    *maestroClient.Client
	networkId uint8
}

// NewMaestroChainContext creates a new Maestro chain context.
func NewMaestroChainContext(networkId uint8, projectId string) (*MaestroChainContext, error) {
	networkStr := networkString(networkId)
	client := maestroClient.NewClient(projectId, networkStr)
	return &MaestroChainContext{
		client:    client,
		networkId: networkId,
	}, nil
}

func networkString(networkId uint8) string {
	switch networkId {
	case 0: // constants.MAINNET
		return "mainnet"
	case 1, 3: // constants.TESTNET, constants.PREPROD
		return "preprod"
	case 2: // constants.PREVIEW
		return "preview"
	default:
		return "preprod"
	}
}

func (m *MaestroChainContext) ProtocolParams() (backend.ProtocolParameters, error) {
	resp, err := m.client.ProtocolParameters()
	if err != nil {
		return backend.ProtocolParameters{}, err
	}

	data := resp.Data
	priceMem, err := backend.ParseFraction(data.ScriptExecutionPrices.Memory)
	if err != nil {
		return backend.ProtocolParameters{}, fmt.Errorf("invalid memory price: %w", err)
	}
	priceStep, err := backend.ParseFraction(data.ScriptExecutionPrices.Steps)
	if err != nil {
		return backend.ProtocolParameters{}, fmt.Errorf("invalid step price: %w", err)
	}

	pp := backend.ProtocolParameters{
		MinFeeCoefficient:   data.MinFeeCoefficient,
		MinFeeConstant:      data.MinFeeConstant.LovelaceAmount.Lovelace,
		MaxBlockSize:        int(data.MaxBlockBodySize.Bytes),
		MaxTxSize:           int(data.MaxTransactionSize.Bytes),
		MaxBlockHeaderSize:  int(data.MaxBlockHeaderSize.Bytes),
		KeyDeposits:         strconv.FormatInt(data.StakeCredentialDeposit.LovelaceAmount.Lovelace, 10),
		PoolDeposits:        strconv.FormatInt(data.StakePoolDeposit.LovelaceAmount.Lovelace, 10),
		MinPoolCost:         strconv.FormatInt(data.MinStakePoolCost.LovelaceAmount.Lovelace, 10),
		MaxTxExMem:          strconv.FormatInt(data.MaxExecutionUnitsPerTransaction.Memory, 10),
		MaxTxExSteps:        strconv.FormatInt(data.MaxExecutionUnitsPerTransaction.Steps, 10),
		MaxBlockExMem:       strconv.FormatInt(data.MaxExecutionUnitsPerBlock.Memory, 10),
		MaxBlockExSteps:     strconv.FormatInt(data.MaxExecutionUnitsPerBlock.Steps, 10),
		MaxValSize:          strconv.FormatInt(data.MaxValueSize.Bytes, 10),
		CollateralPercent:   int(data.CollateralPercentage),
		MaxCollateralInputs: int(data.MaxCollateralInputs),
		CoinsPerUtxoByte:    strconv.FormatInt(data.MinUtxoDepositCoefficient, 10),
		PriceMem:            priceMem,
		PriceStep:           priceStep,
	}

	// Parse cost models from Maestro response.
	// PlutusCostModels is typed as `any`; when unmarshaled from JSON it is
	// map[string]interface{} with keys like "plutus:v1", "plutus:v2", "plutus:v3"
	// and values that are []interface{} of float64.
	// ComputeScriptDataHash expects keys "PlutusV1", "PlutusV2", "PlutusV3".
	if rawModels, ok := data.PlutusCostModels.(map[string]interface{}); ok {
		pp.CostModels = make(map[string][]int64, len(rawModels))
		for key, val := range rawModels {
			costs, ok := val.([]interface{})
			if !ok {
				continue
			}
			int64Costs := make([]int64, 0, len(costs))
			for _, c := range costs {
				if f, ok := c.(float64); ok {
					int64Costs = append(int64Costs, int64(f))
				}
			}
			pp.CostModels[maestroCostModelKey(key)] = int64Costs
		}
	}

	return pp, nil
}


func (m *MaestroChainContext) GenesisParams() (backend.GenesisParameters, error) {
	return backend.GenesisParameters{}, errors.New("genesis params not available via Maestro API")
}

func (m *MaestroChainContext) NetworkId() uint8 {
	return m.networkId
}

func (m *MaestroChainContext) CurrentEpoch() (uint64, error) {
	resp, err := m.client.CurrentEpoch()
	if err != nil {
		return 0, err
	}
	if resp.Data.EpochNo < 0 {
		return 0, fmt.Errorf("invalid epoch value: %d", resp.Data.EpochNo)
	}
	return uint64(resp.Data.EpochNo), nil
}

func (m *MaestroChainContext) MaxTxFee() (uint64, error) {
	pp, err := m.ProtocolParams()
	if err != nil {
		return 0, err
	}
	return backend.ComputeMaxTxFee(pp)
}

func (m *MaestroChainContext) Tip() (uint64, error) {
	resp, err := m.client.ChainTip()
	if err != nil {
		return 0, err
	}
	if resp.Data.Slot < 0 {
		return 0, fmt.Errorf("invalid slot value: %d", resp.Data.Slot)
	}
	return uint64(resp.Data.Slot), nil
}

func (m *MaestroChainContext) Utxos(address common.Address) ([]common.Utxo, error) {
	const maxPages = 1000
	var allUtxos []common.Utxo
	params := utils.NewParameters()

	for page := 0; page < maxPages; page++ {
		resp, err := m.client.UtxosAtAddress(address.String(), params)
		if err != nil {
			return nil, err
		}

		for _, raw := range resp.Data {
			utxo, err := maestroUtxoToCommon(raw, address)
			if err != nil {
				continue
			}
			allUtxos = append(allUtxos, utxo)
		}

		if resp.NextCursor == "" {
			break
		}
		params = utils.NewParameters()
		params.Cursor(resp.NextCursor)
	}

	return allUtxos, nil
}

func (m *MaestroChainContext) SubmitTx(txCbor []byte) (common.Blake2b256, error) {
	txHex := hex.EncodeToString(txCbor)
	resp, err := m.client.SubmitTx(txHex)
	if err != nil {
		return common.Blake2b256{}, err
	}
	hashBytes, err := hex.DecodeString(resp.Data)
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

func (m *MaestroChainContext) EvaluateTx(txCbor []byte) (map[common.RedeemerKey]common.ExUnits, error) {
	txHex := hex.EncodeToString(txCbor)
	evalResp, err := m.client.EvaluateTx(txHex)
	if err != nil {
		return nil, err
	}

	result := make(map[common.RedeemerKey]common.ExUnits)
	for _, eval := range evalResp {
		tag := backend.ParseRedeemerTag(eval.RedeemerTag)
		key := common.RedeemerKey{Tag: tag, Index: uint32(eval.RedeemerIndex)}
		result[key] = common.ExUnits{Memory: eval.ExUnits.Mem, Steps: eval.ExUnits.Steps}
	}
	return result, nil
}

func (m *MaestroChainContext) UtxoByRef(txHash common.Blake2b256, index uint32) (*common.Utxo, error) {
	hashHex := hex.EncodeToString(txHash.Bytes())
	resp, err := m.client.TransactionOutputFromReference(hashHex, int(index), nil)
	if err != nil {
		return nil, err
	}

	addr, err := common.NewAddress(resp.Data.Address)
	if err != nil {
		return nil, err
	}
	utxo, err := maestroUtxoToCommon(resp.Data, addr)
	if err != nil {
		return nil, err
	}
	return &utxo, nil
}

func (m *MaestroChainContext) ScriptCbor(scriptHash common.Blake2b224) ([]byte, error) {
	hashHex := hex.EncodeToString(scriptHash.Bytes())
	resp, err := m.client.ScriptByHash(hashHex)
	if err != nil {
		return nil, err
	}
	if resp.Data.Bytes == "" {
		return nil, errors.New("no script CBOR available")
	}
	return hex.DecodeString(resp.Data.Bytes)
}

func maestroUtxoToCommon(raw models.Utxo, address common.Address) (common.Utxo, error) {
	hashBytes, err := hex.DecodeString(raw.TxHash)
	if err != nil {
		return common.Utxo{}, err
	}
	if len(hashBytes) != common.Blake2b256Size {
		return common.Utxo{}, fmt.Errorf("invalid tx hash length: expected %d bytes, got %d", common.Blake2b256Size, len(hashBytes))
	}
	var txId common.Blake2b256
	copy(txId[:], hashBytes)

	if raw.Index < 0 {
		return common.Utxo{}, fmt.Errorf("negative output index: %d", raw.Index)
	}
	input := shelley.ShelleyTransactionInput{
		TxId:        txId,
		OutputIndex: uint32(raw.Index),
	}

	var lovelace uint64
	assetData := make(map[common.Blake2b224]map[cbor.ByteString]*big.Int)

	for _, asset := range raw.Assets {
		if asset.Unit == "lovelace" {
			if asset.Amount < 0 {
				continue
			}
			lovelace = uint64(asset.Amount) //nolint:gosec // validated non-negative above
		} else if len(asset.Unit) >= 56 {
			policyHex := asset.Unit[:56]
			nameHex := asset.Unit[56:]
			policyBytes, err := hex.DecodeString(policyHex)
			if err != nil {
				continue
			}
			if len(policyBytes) != common.Blake2b224Size {
				continue
			}
			var policyId common.Blake2b224
			copy(policyId[:], policyBytes)

			nameBytes, err := hex.DecodeString(nameHex)
			if err != nil {
				nameBytes = []byte{}
			}

			if _, ok := assetData[policyId]; !ok {
				assetData[policyId] = make(map[cbor.ByteString]*big.Int)
			}
			assetData[policyId][cbor.NewByteString(nameBytes)] = big.NewInt(asset.Amount)
		}
	}

	var assets *common.MultiAsset[common.MultiAssetTypeOutput]
	if len(assetData) > 0 {
		ma := common.NewMultiAsset[common.MultiAssetTypeOutput](assetData)
		assets = &ma
	}

	output := babbage.BabbageTransactionOutput{
		OutputAddress: address,
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


// maestroCostModelKey translates Maestro cost model keys to the canonical form
// expected by ComputeScriptDataHash ("PlutusV1", "PlutusV2", "PlutusV3").
func maestroCostModelKey(key string) string {
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
