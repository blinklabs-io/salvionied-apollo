package blockfrost

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger/babbage"
	"github.com/blinklabs-io/gouroboros/ledger/common"
	"github.com/blinklabs-io/gouroboros/ledger/mary"
	"github.com/blinklabs-io/gouroboros/ledger/shelley"

	"github.com/Salvionied/apollo/v2/backend"
)

// BlockFrostChainContext implements backend.ChainContext using the BlockFrost API.
type BlockFrostChainContext struct {
	baseUrl   string
	projectId string
	networkId uint8
	client    *http.Client

	mu             sync.Mutex
	cachedParams   *backend.ProtocolParameters
	cachedGenesis  *backend.GenesisParameters
	paramsCacheAt  time.Time
	genesisCacheAt time.Time
}

const cacheExpiry = 5 * time.Minute

// NewBlockFrostChainContext creates a new BlockFrost backend.
func NewBlockFrostChainContext(baseUrl string, networkId uint8, projectId string) *BlockFrostChainContext {
	// Ensure base URL ends with version path
	if !strings.HasSuffix(baseUrl, "/v0") {
		baseUrl = strings.TrimRight(baseUrl, "/") + "/v0"
	}
	return &BlockFrostChainContext{
		baseUrl:   baseUrl,
		projectId: projectId,
		networkId: networkId,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (b *BlockFrostChainContext) request(method, path string, body io.Reader, contentType string) ([]byte, error) {
	url := b.baseUrl + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if b.projectId != "" {
		req.Header.Set("project_id", b.projectId)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Body == nil {
		return nil, errors.New("blockfrost: nil response")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("blockfrost API error %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (b *BlockFrostChainContext) ProtocolParams() (backend.ProtocolParameters, error) {
	b.mu.Lock()
	if b.cachedParams != nil && time.Since(b.paramsCacheAt) < cacheExpiry {
		pp := *b.cachedParams
		// Deep copy CostModels to prevent callers from mutating the cache.
		if pp.CostModels != nil {
			cm := make(map[string][]int64, len(pp.CostModels))
			for k, v := range pp.CostModels {
				dup := make([]int64, len(v))
				copy(dup, v)
				cm[k] = dup
			}
			pp.CostModels = cm
		}
		b.mu.Unlock()
		return pp, nil
	}
	b.mu.Unlock()

	data, err := b.request("GET", "/epochs/latest/parameters", nil, "")
	if err != nil {
		return backend.ProtocolParameters{}, err
	}

	var raw bfProtocolParams
	if err := json.Unmarshal(data, &raw); err != nil {
		return backend.ProtocolParameters{}, err
	}

	pp := raw.toProtocolParams()

	// Deep copy CostModels before storing to prevent callers from mutating the cache.
	cached := pp
	if cached.CostModels != nil {
		cm := make(map[string][]int64, len(cached.CostModels))
		for k, v := range cached.CostModels {
			dup := make([]int64, len(v))
			copy(dup, v)
			cm[k] = dup
		}
		cached.CostModels = cm
	}

	b.mu.Lock()
	b.cachedParams = &cached
	b.paramsCacheAt = time.Now()
	b.mu.Unlock()

	return pp, nil
}

func (b *BlockFrostChainContext) GenesisParams() (backend.GenesisParameters, error) {
	b.mu.Lock()
	if b.cachedGenesis != nil && time.Since(b.genesisCacheAt) < cacheExpiry {
		gp := *b.cachedGenesis
		b.mu.Unlock()
		return gp, nil
	}
	b.mu.Unlock()

	data, err := b.request("GET", "/genesis", nil, "")
	if err != nil {
		return backend.GenesisParameters{}, err
	}

	var raw bfGenesisParams
	if err := json.Unmarshal(data, &raw); err != nil {
		return backend.GenesisParameters{}, err
	}

	gp := backend.GenesisParameters{
		ActiveSlotsCoefficient: float32(raw.ActiveSlotsCoefficient),
		UpdateQuorum:           raw.UpdateQuorum,
		NetworkMagic:           raw.NetworkMagic,
		EpochLength:            raw.EpochLength,
		MaxLovelaceSupply:      strconv.FormatInt(raw.MaxLovelaceSupply, 10),
		SlotLength:             raw.SlotLength,
		SlotsPerKesPeriod:      raw.SlotsPerKesPeriod,
		MaxKesEvolutions:       raw.MaxKesEvolutions,
		SecurityParam:          raw.SecurityParam,
	}

	b.mu.Lock()
	b.cachedGenesis = &gp
	b.genesisCacheAt = time.Now()
	b.mu.Unlock()

	return gp, nil
}

func (b *BlockFrostChainContext) NetworkId() uint8 {
	return b.networkId
}

func (b *BlockFrostChainContext) CurrentEpoch() (uint64, error) {
	data, err := b.request("GET", "/epochs/latest", nil, "")
	if err != nil {
		return 0, err
	}
	var result struct {
		Epoch int `json:"epoch"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, err
	}
	if result.Epoch < 0 {
		return 0, fmt.Errorf("invalid epoch value: %d", result.Epoch)
	}
	return uint64(result.Epoch), nil
}

func (b *BlockFrostChainContext) MaxTxFee() (uint64, error) {
	pp, err := b.ProtocolParams()
	if err != nil {
		return 0, err
	}
	return backend.ComputeMaxTxFee(pp)
}

func (b *BlockFrostChainContext) Tip() (uint64, error) {
	data, err := b.request("GET", "/blocks/latest", nil, "")
	if err != nil {
		return 0, err
	}
	var result struct {
		Slot int `json:"slot"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, err
	}
	if result.Slot < 0 {
		return 0, fmt.Errorf("invalid slot value: %d", result.Slot)
	}
	return uint64(result.Slot), nil
}

func (b *BlockFrostChainContext) Utxos(address common.Address) ([]common.Utxo, error) {
	const maxPages = 1000
	var allUtxos []common.Utxo
	page := 1

	for page <= maxPages {
		path := fmt.Sprintf("/addresses/%s/utxos?page=%d", address.String(), page)
		data, err := b.request("GET", path, nil, "")
		if err != nil {
			return nil, err
		}

		var rawUtxos []bfAddressUTxO
		if err := json.Unmarshal(data, &rawUtxos); err != nil {
			return nil, err
		}
		if len(rawUtxos) == 0 {
			break
		}

		for _, raw := range rawUtxos {
			utxo, err := raw.toUtxo(address)
			if err != nil {
				continue
			}
			allUtxos = append(allUtxos, utxo)
		}
		page++
	}
	return allUtxos, nil
}

func (b *BlockFrostChainContext) SubmitTx(txCbor []byte) (common.Blake2b256, error) {
	body := bytes.NewReader(txCbor)
	data, err := b.request("POST", "/tx/submit", body, "application/cbor")
	if err != nil {
		return common.Blake2b256{}, err
	}
	var txHash string
	if err := json.Unmarshal(data, &txHash); err != nil {
		return common.Blake2b256{}, err
	}
	hashBytes, err := hex.DecodeString(txHash)
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

func (b *BlockFrostChainContext) EvaluateTx(txCbor []byte) (map[common.RedeemerKey]common.ExUnits, error) {
	body := bytes.NewReader(txCbor)
	data, err := b.request("POST", "/utils/txs/evaluate", body, "application/cbor")
	if err != nil {
		return nil, err
	}

	var evalResult bfEvalResult
	if err := json.Unmarshal(data, &evalResult); err != nil {
		return nil, err
	}

	result := make(map[common.RedeemerKey]common.ExUnits)
	for key, budget := range evalResult.Result.EvaluationResult {
		parts := strings.Split(key, ":")
		if len(parts) != 2 {
			continue
		}
		tag := backend.ParseRedeemerTag(parts[0])
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		rKey := common.RedeemerKey{Tag: tag, Index: uint32(idx)}
		result[rKey] = common.ExUnits{Memory: int64(budget.Memory), Steps: int64(budget.Steps)}
	}
	return result, nil
}

func (b *BlockFrostChainContext) UtxoByRef(txHash common.Blake2b256, index uint32) (*common.Utxo, error) {
	hashHex := hex.EncodeToString(txHash.Bytes())
	path := fmt.Sprintf("/txs/%s/utxos", hashHex)
	data, err := b.request("GET", path, nil, "")
	if err != nil {
		return nil, err
	}

	var txUtxos struct {
		Outputs []bfAddressUTxO `json:"outputs"`
	}
	if err := json.Unmarshal(data, &txUtxos); err != nil {
		return nil, err
	}

	for _, raw := range txUtxos.Outputs {
		if uint32(raw.OutputIndex) == index {
			addr, err := common.NewAddress(raw.Address)
			if err != nil {
				return nil, err
			}
			utxo, err := raw.toUtxo(addr)
			if err != nil {
				return nil, err
			}
			return &utxo, nil
		}
	}
	return nil, errors.New("utxo not found")
}

func (b *BlockFrostChainContext) ScriptCbor(scriptHash common.Blake2b224) ([]byte, error) {
	hashHex := hex.EncodeToString(scriptHash.Bytes())
	path := fmt.Sprintf("/scripts/%s/cbor", hashHex)
	data, err := b.request("GET", path, nil, "")
	if err != nil {
		return nil, err
	}
	var result struct {
		Cbor string `json:"cbor"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	scriptCbor, err := hex.DecodeString(result.Cbor)
	if err != nil {
		return nil, fmt.Errorf("invalid script CBOR hex: %w", err)
	}
	return scriptCbor, nil
}

// --- BlockFrost response types ---

type bfProtocolParams struct {
	MinFeeA            int64           `json:"min_fee_a"`
	MinFeeB            int64           `json:"min_fee_b"`
	MaxBlockSize       int64           `json:"max_block_size"`
	MaxTxSize          int64           `json:"max_tx_size"`
	MaxBlockHeaderSize int64           `json:"max_block_header_size"`
	KeyDeposit         string          `json:"key_deposit"`
	PoolDeposit        string          `json:"pool_deposit"`
	Decentralisation   float64         `json:"decentralisation_param"`
	MinPoolCost        string          `json:"min_pool_cost"`
	PriceMem           float64         `json:"price_mem"`
	PriceStep          float64         `json:"price_step"`
	MaxTxExMem         string          `json:"max_tx_execution_units_memory"`
	MaxTxExSteps       string          `json:"max_tx_execution_units_steps"`
	MaxBlockExMem      string          `json:"max_block_execution_units_memory"`
	MaxBlockExSteps    string          `json:"max_block_execution_units_steps"`
	MaxValSize         string          `json:"max_val_size"`
	CollateralPercent  int64           `json:"collateral_percent"`
	MaxCollateralIn    int64           `json:"max_collateral_inputs"`
	CoinsPerUtxoSize   string          `json:"coins_per_utxo_size"`
	CostModels         json.RawMessage `json:"cost_models"`
}

func (p *bfProtocolParams) toProtocolParams() backend.ProtocolParameters {
	pp := backend.ProtocolParameters{
		MinFeeConstant:      p.MinFeeB,
		MinFeeCoefficient:   p.MinFeeA,
		MaxBlockSize:        int(p.MaxBlockSize),
		MaxTxSize:           int(p.MaxTxSize),
		MaxBlockHeaderSize:  int(p.MaxBlockHeaderSize),
		KeyDeposits:         p.KeyDeposit,
		PoolDeposits:        p.PoolDeposit,
		MinPoolCost:         p.MinPoolCost,
		PriceMem:            float32(p.PriceMem),
		PriceStep:           float32(p.PriceStep),
		MaxTxExMem:          p.MaxTxExMem,
		MaxTxExSteps:        p.MaxTxExSteps,
		MaxBlockExMem:       p.MaxBlockExMem,
		MaxBlockExSteps:     p.MaxBlockExSteps,
		MaxValSize:          p.MaxValSize,
		CollateralPercent:   int(p.CollateralPercent),
		MaxCollateralInputs: int(p.MaxCollateralIn),
		CoinsPerUtxoByte:    p.CoinsPerUtxoSize,
	}

	// Parse cost models from BlockFrost JSON.
	// BlockFrost uses keys "PlutusV1", "PlutusV2", "PlutusV3" which already
	// match the canonical form expected by ComputeScriptDataHash.
	if len(p.CostModels) > 0 {
		var rawModels map[string][]int64
		if err := json.Unmarshal(p.CostModels, &rawModels); err == nil {
			pp.CostModels = rawModels
		}
	}

	return pp
}

type bfGenesisParams struct {
	ActiveSlotsCoefficient float64 `json:"active_slots_coefficient"`
	UpdateQuorum           int     `json:"update_quorum"`
	NetworkMagic           int     `json:"network_magic"`
	EpochLength            int     `json:"epoch_length"`
	MaxLovelaceSupply      int64   `json:"max_lovelace_supply"`
	SlotLength             int     `json:"slot_length"`
	SlotsPerKesPeriod      int     `json:"slots_per_kes_period"`
	MaxKesEvolutions       int     `json:"max_kes_evolutions"`
	SecurityParam          int     `json:"security_param"`
}

type bfAddressUTxO struct {
	TxHash      string            `json:"tx_hash"`
	OutputIndex int               `json:"output_index"`
	Address     string            `json:"address"`
	Amount      []bfAddressAmount `json:"amount"`
	DataHash    string            `json:"data_hash"`
	InlineDatum json.RawMessage   `json:"inline_datum"`
}

type bfAddressAmount struct {
	Unit     string `json:"unit"`
	Quantity string `json:"quantity"`
}

func (raw *bfAddressUTxO) toUtxo(address common.Address) (common.Utxo, error) {
	hashBytes, err := hex.DecodeString(raw.TxHash)
	if err != nil {
		return common.Utxo{}, err
	}
	if len(hashBytes) != common.Blake2b256Size {
		return common.Utxo{}, fmt.Errorf("invalid tx hash length: expected %d bytes, got %d", common.Blake2b256Size, len(hashBytes))
	}
	var txId common.Blake2b256
	copy(txId[:], hashBytes)

	if raw.OutputIndex < 0 {
		return common.Utxo{}, fmt.Errorf("negative output index: %d", raw.OutputIndex)
	}
	input := shelley.ShelleyTransactionInput{
		TxId:        txId,
		OutputIndex: uint32(raw.OutputIndex),
	}

	// Parse amounts
	var lovelace uint64
	assetData := make(map[common.Blake2b224]map[cbor.ByteString]*big.Int)

	for _, amt := range raw.Amount {
		qty, err := strconv.ParseInt(amt.Quantity, 10, 64)
		if err != nil {
			continue
		}
		if amt.Unit == "lovelace" {
			if qty < 0 {
				continue
			}
			lovelace = uint64(qty) //nolint:gosec // validated non-negative above
		} else if len(amt.Unit) >= 56 {
			policyHex := amt.Unit[:56]
			nameHex := amt.Unit[56:]
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
			assetData[policyId][cbor.NewByteString(nameBytes)] = big.NewInt(qty)
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

type bfEvalResult struct {
	Result struct {
		EvaluationResult map[string]struct {
			Memory uint64 `json:"memory"`
			Steps  uint64 `json:"steps"`
		} `json:"EvaluationResult"`
	} `json:"result"`
}

