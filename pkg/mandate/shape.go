package mandate

import (
	"github.com/cosmos/gogoproto/proto"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// multisigTypeURL identifies a member that is itself a multisig. Comparing
// type URLs reads the member without unpacking it, which keeps the whole
// classification independent of whether an interface registry was on hand.
var multisigTypeURL = "/" + proto.MessageName(&kmultisig.LegacyAminoPubKey{})

// Shape classifies one committee account, taking the account rather than the
// store it came from so this package still reads no module state. A nil
// account is an address holding no account at all.
//
// Classification is total. The interface registry closes the set of key types
// — one it does not know cannot decode into an account — so every account the
// chain can hold lands on exactly one kind, and a type this binary predates
// lands on OTHER rather than being read as a shape it is not.
func Shape(account sdk.AccountI) CommitteeShape {
	if account == nil {
		return CommitteeShape{KeyKind: CommitteeKeyKind_COMMITTEE_KEY_KIND_ABSENT}
	}

	shape := CommitteeShape{AccountType: proto.MessageName(account)}
	if moduleAccount, ok := account.(sdk.ModuleAccountI); ok {
		shape.Module = moduleAccount.GetName()
	}

	switch key := account.GetPubKey().(type) {
	case nil:
		shape.KeyKind = CommitteeKeyKind_COMMITTEE_KEY_KIND_KEYLESS
	case *authtypes.ModuleCredential:
		shape.KeyKind = CommitteeKeyKind_COMMITTEE_KEY_KIND_MODULE
		if key != nil {
			shape.Module = key.ModuleName
		}
	case *kmultisig.LegacyAminoPubKey:
		shape.KeyKind = CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG
		shape.Threshold, shape.MemberCount = verifiedMultisig(key)
	case *secp256k1.PubKey, *ed25519.PubKey, *secp256r1.PubKey:
		shape.KeyKind = CommitteeKeyKind_COMMITTEE_KEY_KIND_SINGLE
	default:
		shape.KeyKind = CommitteeKeyKind_COMMITTEE_KEY_KIND_OTHER
	}

	return shape
}

// verifiedMultisig reports the K-of-N a multisig provably requires, and zeroes
// for one whose members do not each contribute an independent signature. A
// repeated member multiplies one holder's weight, and a nested multisig can
// re-list a key already counted, so neither is the K-of-N it reads as — and a
// threshold recorded from either would be the observation lying.
func verifiedMultisig(key *kmultisig.LegacyAminoPubKey) (uint32, uint32) {
	if key == nil {
		return 0, 0
	}
	members := key.PubKeys
	// Neither bound is reachable through a transaction: a threshold of zero or
	// one above the member count cannot authenticate, so the ante handler never
	// registers such a key. The guard keeps the observation honest for a state
	// that arrived another way.
	if key.Threshold == 0 || uint64(key.Threshold) > uint64(len(members)) {
		return 0, 0
	}

	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		if member == nil || member.TypeUrl == multisigTypeURL {
			return 0, 0
		}
		identity := member.TypeUrl + "/" + string(member.Value)
		if _, duplicate := seen[identity]; duplicate {
			return 0, 0
		}
		seen[identity] = struct{}{}
	}

	return key.Threshold, uint32(len(members))
}

// IsZero reports the shape a disabled mandate carries: nothing observed.
func (s CommitteeShape) IsZero() bool {
	return s == CommitteeShape{}
}
