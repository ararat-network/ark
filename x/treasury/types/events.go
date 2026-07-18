package types

const (
	EventTypeTaxCapsUpdated          = "tax_caps_updated"
	EventTypeTaxCapsUpdateSkipped    = "tax_caps_update_skipped"
	EventTypeBlockRewardsToppedUp    = "block_rewards_topped_up"
	EventTypeBlockRewardTopUpSkipped = "block_reward_top_up_skipped"
	EventTypeExpansionAllocated      = "expansion_allocated"
	EventTypeRedemptionBufferDrawn   = "redemption_buffer_drawn"
	EventTypeClaimSubmitted          = "claim_submitted"
	EventTypeClaimPaid               = "claim_paid"

	AttributeKeyClaimID                    = "claim_id"
	AttributeKeyRecipient                  = "recipient"
	AttributeKeyAmount                     = "amount"
	AttributeKeyTaxCaps                    = "tax_caps"
	AttributeKeySkipReason                 = "skip_reason"
	AttributeKeyTarget                     = "target"
	AttributeKeyOrganic                    = "organic"
	AttributeKeyPaid                       = "paid"
	AttributeKeyRedemptionBufferCredit     = "redemption_buffer_credit"
	AttributeKeyStrategicReserveCredit     = "strategic_reserve_credit"
	AttributeKeyInsuranceCredit            = "insurance_credit"
	AttributeKeySpreadAndDustBurn          = "spread_and_dust_burn"
	AttributeKeyOverflowBurn               = "overflow_burn"
	AttributeKeyTargetValuationComplete    = "target_valuation_complete"
	AttributeKeyAggregateValuationComplete = "aggregate_valuation_complete"
	AttributeKeyRedemptionBufferPayment    = "redemption_buffer_payment"
)
