package constants

const MinLovelace = 1_000_000

type Network int

const (
	MAINNET Network = iota
	TESTNET
	PREVIEW
	PREPROD
)

const BlockfrostBaseUrlMainnet = "https://cardano-mainnet.blockfrost.io/api"
const BlockfrostBaseUrlTestnet = "https://cardano-testnet.blockfrost.io/api"
const BlockfrostBaseUrlPreview = "https://cardano-preview.blockfrost.io/api"
const BlockfrostBaseUrlPreprod = "https://cardano-preprod.blockfrost.io/api"
