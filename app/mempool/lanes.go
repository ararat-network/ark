// Package mempool owns pending transactions, authenticated lanes, reserved admission, and local
// proposal ordering. CometBFT owns gossip, deduplication, and recheck scheduling. ProcessProposal
// validates independently of local capacity.
package mempool

import sdk "github.com/cosmos/cosmos-sdk/types"

// Lane values identify independent admission reservations and proposal service classes.
const (
	LaneNormal int8 = iota
	LaneGovernance
	LaneCommittee
)

// Privilege registers message types and their priority eligibility check.
// A refusal that only denies priority returns false, nil. Errors reject the
// transaction, including committee authorisation and eligibility read failures.
// Vouches must be read-only and bounded; they never execute handlers.
type Privilege struct {
	Lane  int8
	Msgs  []sdk.Msg
	Vouch func(ctx sdk.Context, msg sdk.Msg) (bool, error)
}

type privilege struct {
	lane  int8
	vouch func(sdk.Context, sdk.Msg) (bool, error)
}

// Set is the privileged message surface, keyed by proto type URL. The zero
// Set privileges nothing, leaving a plain fee-ordered pool. Ante classifies
// through it and records the lane in the context the pool reads on insertion.
type Set struct{ msgs map[string]privilege }

// NewSet builds a lane set from privileges. A privilege without a vouch, or a
// message privileged twice, is a wiring error and panics at construction.
func NewSet(privileges ...Privilege) Set {
	msgs := make(map[string]privilege)
	for _, p := range privileges {
		if p.Lane != LaneGovernance && p.Lane != LaneCommittee {
			panic("lanes: invalid privileged lane")
		}
		if p.Vouch == nil {
			panic("lanes: privilege without a vouch")
		}
		for _, msg := range p.Msgs {
			url := sdk.MsgTypeURL(msg)
			if _, dup := msgs[url]; dup {
				panic("lanes: " + url + " privileged twice")
			}
			msgs[url] = privilege{lane: p.Lane, vouch: p.Vouch}
		}
	}
	return Set{msgs: msgs}
}

// URLs returns a copy of the type URLs in the set, so callers can pin it
// against their interface registry.
func (s Set) URLs() map[string]struct{} {
	urls := make(map[string]struct{}, len(s.msgs))
	for url := range s.msgs {
		urls[url] = struct{}{}
	}
	return urls
}

// Vouch evaluates one registered message. Unregistered messages cannot earn
// priority, and do not invoke a check.
func (s Set) Vouch(ctx sdk.Context, msg sdk.Msg) (bool, error) {
	p, ok := s.msgs[sdk.MsgTypeURL(msg)]
	if !ok {
		return false, nil
	}
	return p.vouch(ctx, msg)
}

// Classify requires every top-level message to share one privileged lane
// before any vouch runs, so one cheap privileged message cannot tow arbitrary
// messages past the fee market. Mixed, empty and authz-wrapped transactions
// stay normal without reads.
func (s Set) Classify(ctx sdk.Context, tx sdk.Tx) (int8, error) {
	msgs := tx.GetMsgs()
	lane := LaneNormal
	for i, msg := range msgs {
		p, ok := s.msgs[sdk.MsgTypeURL(msg)]
		if !ok || (i > 0 && p.lane != lane) {
			return LaneNormal, nil
		}
		lane = p.lane
	}
	for _, msg := range msgs {
		eligible, err := s.Vouch(ctx, msg)
		if err != nil {
			return LaneNormal, err
		}
		if !eligible {
			return LaneNormal, nil
		}
	}
	return lane, nil
}

// Lane is carried through SDK context to Insert, like the SDK fee priority.
type laneKey struct{}

func WithLane(ctx sdk.Context, lane int8) sdk.Context { return ctx.WithValue(laneKey{}, lane) }
func Lane(ctx sdk.Context) int8 {
	lane, _ := ctx.Value(laneKey{}).(int8)
	return lane
}
