package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	claimskeeper "github.com/ararat-network/ark/x/claims/keeper"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
)

// TestClaimsCommitteeShapeRecordsRegisteredMultisig proves the appointment
// path reaches a real account: a genuine 2-of-3 whose key is registered on
// chain is recorded as one, rather than as the keyless shape an address with
// no registered key would produce.
func TestClaimsCommitteeShapeRecordsRegisteredMultisig(t *testing.T) {
	memberPubKeys := make([]cryptotypes.PubKey, 3)
	for i := range memberPubKeys {
		memberPubKeys[i] = secp256k1.GenPrivKey().PubKey()
	}
	committeePubKey := kmultisig.NewLegacyAminoPubKey(2, memberPubKeys)
	committee := sdk.AccAddress(committeePubKey.Address())

	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})
	account := arkApp.AccountKeeper.NewAccountWithAddress(ctx, committee)
	require.NoError(t, account.SetPubKey(committeePubKey))
	arkApp.AccountKeeper.SetAccount(ctx, account)
	apptestutil.FundModule(t, arkApp, ctx, claimstypes.InsuranceName, sdk.NewCoins(chain.NoahCoin(math.NewInt(1_000))))

	_, err := claimskeeper.NewMsgServerImpl(arkApp.ClaimsKeeper).SetClaimsMandate(
		ctx,
		&claimstypes.MsgSetClaimsMandate{
			Authority:           authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			Committee:           committee.String(),
			ActivationTime:      0,
			ExpiryTime:          chain.MaxBlockSeconds,
			CommitteeClaimLimit: math.NewInt(1_000),
		},
	)
	require.NoError(t, err)

	stored, err := arkApp.ClaimsKeeper.ClaimsMandate.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, mandate.CommitteeShape{
		AccountType: "cosmos.auth.v1beta1.BaseAccount",
		KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
		Threshold:   2,
		MemberCount: 3,
	}, stored.CommitteeShape)
}
