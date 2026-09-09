// Package mempool owns Ark's pending transactions, authenticated lane eligibility,
// bounded admission, and preferential block/gossip selection. CometBFT app mode
// delegates pending storage to this package. Ordering is local proposer policy;
// ProcessProposal verifies transaction validity independently of local capacity.
package mempool

import sdk "github.com/cosmos/cosmos-sdk/types"

// Lane values identify independent admission reservations and service phases.
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

// Set is the privileged message surface, keyed by proto type URL. The zero
// Set privileges nothing, leaving a plain fee-ordered pool. Ante uses the set
// to classify eligibility and records the resulting lane in the context that
// the pool reads on insertion.
type Set struct {
	lanes   map[string]int8
	vouches map[string]func(sdk.Context, sdk.Msg) (bool, error)
}

// NewSet builds a lane set from privileges. A privilege without a vouch, or a
// message privileged twice, is a wiring error and panics at construction.
func NewSet(privileges ...Privilege) Set {
	classes := make(map[string]int8)
	vouches := make(map[string]func(sdk.Context, sdk.Msg) (bool, error))
	for _, privilege := range privileges {
		if privilege.Lane != LaneGovernance && privilege.Lane != LaneCommittee {
			panic("lanes: invalid privileged lane")
		}
		if privilege.Vouch == nil {
			panic("lanes: privilege without a vouch")
		}
		for _, msg := range privilege.Msgs {
			url := sdk.MsgTypeURL(msg)
			if _, dup := vouches[url]; dup {
				panic("lanes: " + url + " privileged twice")
			}
			vouches[url] = privilege.Vouch
			classes[url] = privilege.Lane
		}
	}
	return Set{vouches: vouches, lanes: classes}
}

// Has reports whether a message type is a priority candidate.
func (s Set) Has(msg sdk.Msg) bool {
	_, ok := s.vouches[sdk.MsgTypeURL(msg)]
	return ok
}

// URLs returns a copy of the type URLs in the set, so callers can pin it
// against their interface registry.
func (s Set) URLs() map[string]struct{} {
	urls := make(map[string]struct{}, len(s.vouches))
	for url := range s.vouches {
		urls[url] = struct{}{}
	}
	return urls
}

// Vouch evaluates one registered message. Unregistered messages cannot earn
// priority, and do not invoke a check.
func (s Set) Vouch(ctx sdk.Context, msg sdk.Msg) (bool, error) {
	vouch, ok := s.vouches[sdk.MsgTypeURL(msg)]
	if !ok {
		return false, nil
	}
	return vouch(ctx, msg)
}

// Classify evaluates eligibility only when every top-level message is a
// candidate. Mixed and authz-wrapped transactions stay normal without reads.
func (s Set) Classify(ctx sdk.Context, tx sdk.Tx) (int8, error) {
	lane := s.CandidateLane(tx)
	if lane == LaneNormal {
		return LaneNormal, nil
	}
	for _, msg := range tx.GetMsgs() {
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

// CandidateLane identifies candidates by type. Every message must qualify:
// a mixed transaction
// rides the normal lane, so one cheap privileged message cannot tow arbitrary
// messages past the fee market. Authz-wrapped messages are deliberately not
// unwrapped.
func (s Set) CandidateLane(tx sdk.Tx) int8 {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return LaneNormal
	}
	lane := s.lanes[sdk.MsgTypeURL(msgs[0])]
	for _, msg := range msgs {
		if !s.Has(msg) || s.lanes[sdk.MsgTypeURL(msg)] != lane {
			return LaneNormal
		}
	}
	return lane
}

type laneKey struct{}

// WithLane records the result of authenticated priority eligibility checks.
func WithLane(ctx sdk.Context, lane int8) sdk.Context {
	return ctx.WithValue(laneKey{}, lane)
}

// FromContext defaults to normal when no eligibility result was supplied.
func FromContext(ctx sdk.Context) int8 {
	lane, _ := ctx.Value(laneKey{}).(int8)
	if lane != LaneGovernance && lane != LaneCommittee {
		return LaneNormal
	}
	return lane
}
