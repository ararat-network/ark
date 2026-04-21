package types

// Oracle module event types
const (
	EventTypeExchangeRateUpdate = "exchange_rate_update"
	EventTypePrevote            = "prevote"
	EventTypeVote               = "vote"
	EventTypeFeedDelegate       = "feed_delegate"

	AttributeKeyDenom         = "denom"
	AttributeKeyVoter         = "voter"
	AttributeKeyExchangeRate  = "exchange_rate"
	AttributeKeyExchangeRates = "exchange_rates"
	AttributeKeyOperator      = "operator"
	AttributeKeyFeeder        = "feeder"

	AttributeValueCategory = ModuleName
)
