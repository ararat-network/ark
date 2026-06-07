package types

// Treasury module event types
const (
	EventTypePolicyUpdate      = "policy_update"
	EventTypeSeigniorageSettle = "seigniorage_settle"

	AttributeKeyTaxRate             = "tax_rate"
	AttributeKeyRewardWeight        = "reward_weight"
	AttributeKeyTaxCap              = "tax_cap"
	AttributeKeyEpoch               = "epoch"
	AttributeKeySeigniorage         = "seigniorage"
	AttributeKeyBurnAmount          = "burn_amount"
	AttributeKeyOracleReward        = "oracle_reward"
	AttributeKeyCommunityPoolReward = "community_pool_reward"
)
