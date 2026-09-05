package lanes

import sdk "github.com/cosmos/cosmos-sdk/types"

// Privilege is one reason a message rides the priority lane: the messages
// that carry it, and the vouch that admits one of them at CheckTx. The vouch
// is the first refusal the message's handler would make, no more, so it
// refuses nothing execution would accept and everything the lane would
// otherwise carry for free.
type Privilege struct {
	Msgs  []sdk.Msg
	Vouch func(ctx sdk.Context, msg sdk.Msg) error
}

// Set is the privileged message surface, keyed by proto type URL. The zero
// Set privileges nothing, leaving a plain fee-ordered pool. The mempool reads
// it to classify and the ante chain reads it to vouch, so the two cannot
// disagree about what is privileged.
type Set struct {
	vouches map[string]func(sdk.Context, sdk.Msg) error
}

// NewSet builds a lane set from privileges. A privilege without a vouch, or a
// message privileged twice, is a wiring error and panics at construction.
func NewSet(privileges ...Privilege) Set {
	vouches := make(map[string]func(sdk.Context, sdk.Msg) error)
	for _, privilege := range privileges {
		if privilege.Vouch == nil {
			panic("lanes: privilege without a vouch")
		}
		for _, msg := range privilege.Msgs {
			url := sdk.MsgTypeURL(msg)
			if _, dup := vouches[url]; dup {
				panic("lanes: " + url + " privileged twice")
			}
			vouches[url] = privilege.Vouch
		}
	}
	return Set{vouches: vouches}
}

// Has reports whether a message qualifies for the priority lane.
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

// Vouch runs the privilege's check on msg. A message outside the set passes:
// it earns no lane and needs no vouch.
func (s Set) Vouch(ctx sdk.Context, msg sdk.Msg) error {
	vouch, ok := s.vouches[sdk.MsgTypeURL(msg)]
	if !ok {
		return nil
	}
	return vouch(ctx, msg)
}

// Classify assigns the lane. Every message must qualify: a mixed transaction
// rides the normal lane, so one cheap privileged message cannot tow arbitrary
// messages past the fee market. Authz-wrapped messages are deliberately not
// unwrapped.
func (s Set) Classify(tx sdk.Tx) int8 {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return LaneNormal
	}
	for _, msg := range msgs {
		if !s.Has(msg) {
			return LaneNormal
		}
	}
	return LanePriority
}
