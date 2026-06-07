package types

// Oracle module event types
const (
	EventTypeExchangeRateUpdate = "exchange_rate_update"
	EventTypeOracleSlash        = "oracle_slash"
	EventTypeOracleReward       = "oracle_reward"

	AttributeKeyDenom        = "denom"
	AttributeKeyExchangeRate = "exchange_rate"
	AttributeKeyValidator    = "validator"
	AttributeKeyRewardAmount = "reward_amount"
)
