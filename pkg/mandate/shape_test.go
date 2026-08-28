package mandate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256r1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/mandate"
)

const (
	baseAccountType   = "cosmos.auth.v1beta1.BaseAccount"
	moduleAccountType = "cosmos.auth.v1beta1.ModuleAccount"
)

func simpleKey(t *testing.T) cryptotypes.PubKey {
	t.Helper()

	return secp256k1.GenPrivKey().PubKey()
}

func accountWithKey(t *testing.T, key cryptotypes.PubKey) sdk.AccountI {
	t.Helper()

	address := sdk.AccAddress("0123456789abcdefghij")
	if key != nil {
		address = sdk.AccAddress(key.Address())
	}

	return authtypes.NewBaseAccount(address, key, 0, 0)
}

// rawMultisig assembles a multisig without NewLegacyAminoPubKey, which panics
// on the malformed thresholds that only an import could carry.
func rawMultisig(t *testing.T, threshold uint32, keys ...cryptotypes.PubKey) *kmultisig.LegacyAminoPubKey {
	t.Helper()

	packed := make([]*codectypes.Any, len(keys))
	for i, key := range keys {
		any, err := codectypes.NewAnyWithValue(key)
		require.NoError(t, err)
		packed[i] = any
	}

	return &kmultisig.LegacyAminoPubKey{Threshold: threshold, PubKeys: packed}
}

func TestShape(t *testing.T) {
	first, second, third := simpleKey(t), simpleKey(t), simpleKey(t)

	tests := []struct {
		name    string
		account func(*testing.T) sdk.AccountI
		want    mandate.CommitteeShape
	}{
		{
			name:    "no account at the address",
			account: func(*testing.T) sdk.AccountI { return nil },
			want: mandate.CommitteeShape{
				KeyKind: mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_ABSENT,
			},
		},
		{
			name: "account with no registered key",
			account: func(t *testing.T) sdk.AccountI {
				return accountWithKey(t, nil)
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_KEYLESS,
			},
		},
		{
			name: "secp256k1 single key",
			account: func(t *testing.T) sdk.AccountI {
				return accountWithKey(t, first)
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_SINGLE,
			},
		},
		{
			name: "ed25519 single key",
			account: func(t *testing.T) sdk.AccountI {
				return accountWithKey(t, ed25519.GenPrivKey().PubKey())
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_SINGLE,
			},
		},
		{
			name: "secp256r1 single key",
			account: func(t *testing.T) sdk.AccountI {
				key, err := secp256r1.GenPrivKey()
				require.NoError(t, err)

				return accountWithKey(t, key.PubKey())
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_SINGLE,
			},
		},
		{
			name: "two of three distinct members",
			account: func(t *testing.T) sdk.AccountI {
				key := kmultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{first, second, third})

				return accountWithKey(t, key)
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
				Threshold:   2,
				MemberCount: 3,
			},
		},
		{
			name: "one of one is a multisig in name only, and recorded as one",
			account: func(t *testing.T) sdk.AccountI {
				key := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{first})

				return accountWithKey(t, key)
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
				Threshold:   1,
				MemberCount: 1,
			},
		},
		{
			// One holder supplies all three signatures, so the K-of-N this
			// reads as is not the one it requires.
			name: "repeated member zeroes the shape",
			account: func(t *testing.T) sdk.AccountI {
				key := kmultisig.NewLegacyAminoPubKey(3, []cryptotypes.PubKey{first, first, first})

				return accountWithKey(t, key)
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
			},
		},
		{
			// A nested member can re-list a key already counted at the top
			// level, so distinctness there proves nothing.
			name: "nested multisig member zeroes the shape",
			account: func(t *testing.T) sdk.AccountI {
				inner := kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{first})
				key := kmultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{inner, second, third})

				return accountWithKey(t, key)
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
			},
		},
		{
			name: "zero threshold zeroes the shape",
			account: func(t *testing.T) sdk.AccountI {
				return accountWithKey(t, rawMultisig(t, 0, first, second))
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
			},
		},
		{
			name: "threshold above the member count zeroes the shape",
			account: func(t *testing.T) sdk.AccountI {
				return accountWithKey(t, rawMultisig(t, 3, first, second))
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
			},
		},
		{
			name: "module credential names its module",
			account: func(t *testing.T) sdk.AccountI {
				credential, err := authtypes.NewModuleCredential("group", []byte{1}, []byte{2})
				require.NoError(t, err)

				return accountWithKey(t, credential)
			},
			want: mandate.CommitteeShape{
				AccountType: baseAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MODULE,
				Module:      "group",
			},
		},
		{
			name: "module account is keyless and names its module",
			account: func(*testing.T) sdk.AccountI {
				return authtypes.NewEmptyModuleAccount("insurance")
			},
			want: mandate.CommitteeShape{
				AccountType: moduleAccountType,
				KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_KEYLESS,
				Module:      "insurance",
			},
		},
		{
			// The account type is empty because the stub is not a registered
			// proto message; what matters is that an unrecognised key does not
			// fall through to SINGLE.
			name: "key type this binary predates is not read as any known shape",
			account: func(*testing.T) sdk.AccountI {
				return &unknownKeyAccount{}
			},
			want: mandate.CommitteeShape{
				KeyKind: mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_OTHER,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, mandate.Shape(test.account(t)))
		})
	}
}

func TestObserve(t *testing.T) {
	key := kmultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{
		simpleKey(t), simpleKey(t), simpleKey(t),
	})
	account := accountWithKey(t, key)

	t.Run("enabled appointment records what it saw", func(t *testing.T) {
		envelope := mandate.Envelope{
			Term:             1,
			Committee:        account.GetAddress().String(),
			ActivationHeight: 1,
			ExpiryHeight:     100,
		}
		envelope.Observe(account)

		require.Equal(t, mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG, envelope.CommitteeShape.KeyKind)
		require.Equal(t, uint32(2), envelope.CommitteeShape.Threshold)
		require.Equal(t, uint32(3), envelope.CommitteeShape.MemberCount)
		require.NoError(t, envelope.Validate())
	})

	t.Run("an account that is not the committee records nothing", func(t *testing.T) {
		envelope := mandate.Envelope{
			Term:             1,
			Committee:        sdk.AccAddress("0123456789abcdefghij").String(),
			ActivationHeight: 1,
			ExpiryHeight:     100,
		}
		envelope.Observe(account)

		require.True(t, envelope.CommitteeShape.IsZero())
		require.NoError(t, envelope.Validate())
	})

	t.Run("disabled appointment observes nothing", func(t *testing.T) {
		envelope := mandate.Disabled(7)
		envelope.Observe(account)

		require.True(t, envelope.CommitteeShape.IsZero())
		require.NoError(t, envelope.Validate())
	})
}

func TestEnvelopeValidateCommitteeShape(t *testing.T) {
	live := func() mandate.Envelope {
		return mandate.Envelope{
			Term:             1,
			Committee:        sdk.AccAddress("0123456789abcdefghij").String(),
			ActivationHeight: 1,
			ExpiryHeight:     100,
		}
	}

	tests := []struct {
		name     string
		envelope func() mandate.Envelope
		wantErr  string
	}{
		{
			name: "disabled envelope carrying a shape",
			envelope: func() mandate.Envelope {
				envelope := mandate.Disabled(3)
				envelope.CommitteeShape.KeyKind = mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_KEYLESS

				return envelope
			},
			wantErr: "disabled envelope must not carry a committee shape",
		},
		{
			name: "threshold without a member count",
			envelope: func() mandate.Envelope {
				envelope := live()
				envelope.CommitteeShape.Threshold = 2

				return envelope
			},
			wantErr: "committee threshold and member count must be zero together",
		},
		{
			name: "member count without a threshold",
			envelope: func() mandate.Envelope {
				envelope := live()
				envelope.CommitteeShape.MemberCount = 3

				return envelope
			},
			wantErr: "committee threshold and member count must be zero together",
		},
		{
			name: "threshold above the member count",
			envelope: func() mandate.Envelope {
				envelope := live()
				envelope.CommitteeShape.Threshold = 4
				envelope.CommitteeShape.MemberCount = 3

				return envelope
			},
			wantErr: "committee threshold cannot exceed the member count",
		},
		{
			name: "unclassified committee is accepted as unknown",
			envelope: func() mandate.Envelope {
				return live()
			},
		},
		{
			name: "coherent shape is accepted",
			envelope: func() mandate.Envelope {
				envelope := live()
				envelope.CommitteeShape = mandate.CommitteeShape{
					AccountType: baseAccountType,
					KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
					Threshold:   2,
					MemberCount: 3,
				}

				return envelope
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.envelope().Validate()
			if test.wantErr == "" {
				require.NoError(t, err)

				return
			}
			require.EqualError(t, err, test.wantErr)
		})
	}
}

// unknownPubKey stands for a key type registered by a later binary than this
// one, which must classify as OTHER rather than as a single key.
type unknownPubKey struct{}

func (unknownPubKey) Reset()                           {}
func (unknownPubKey) String() string                   { return "unknown" }
func (unknownPubKey) ProtoMessage()                    {}
func (unknownPubKey) Address() cryptotypes.Address     { return cryptotypes.Address{} }
func (unknownPubKey) Bytes() []byte                    { return nil }
func (unknownPubKey) VerifySignature(_, _ []byte) bool { return false }
func (unknownPubKey) Equals(cryptotypes.PubKey) bool   { return false }
func (unknownPubKey) Type() string                     { return "unknown" }

// unknownKeyAccount serves that key directly. A BaseAccount cannot hold it:
// storing a pubkey packs it into an Any, which an unregistered message cannot
// survive.
type unknownKeyAccount struct {
	authtypes.BaseAccount
}

func (*unknownKeyAccount) GetPubKey() cryptotypes.PubKey { return unknownPubKey{} }
