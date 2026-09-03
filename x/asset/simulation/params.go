package simulation

import (
	"math/rand"

	"github.com/ararat-network/ark/x/asset/types"
)

// RandomisedParams returns valid Asset parameters. The activation delay is the
// module's only parameter: validation refuses zero, so the range starts at one
// block and stops at the domain cap.
func RandomisedParams(r *rand.Rand) types.Params {
	return types.Params{
		SettlementActivationDelayBlocks: 1 + uint64(
			r.Int63n(int64(types.MaxSettlementActivationDelayBlocks)),
		),
	}
}
