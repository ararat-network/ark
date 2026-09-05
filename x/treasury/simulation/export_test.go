package simulation

import (
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"github.com/ararat-network/ark/x/treasury/types"
)

// The corridor draw helpers are unexported because nothing outside the
// package draws a policy. Their guard against an inverted or pinned range is
// worth holding directly: it is reached only by a mandate shape the factory
// tests cannot produce on demand.

func BetweenDec(r *simsx.XRand, minimum, maximum math.LegacyDec) math.LegacyDec {
	return betweenDec(r, minimum, maximum)
}

func BetweenInt(r *simsx.XRand, minimum, maximum math.Int) math.Int {
	return betweenInt(r, minimum, maximum)
}

func PolicyWithin(r *simsx.XRand, mandate types.EconomicMandate) types.EconomicPolicy {
	return policyWithin(r, mandate)
}
