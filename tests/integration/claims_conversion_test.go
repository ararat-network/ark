package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
)

// TestNativeClaimsConversion runs stablecoin claims through the real Market and Bank: a claim buys
// what Insurance custody lacks with Insurance NOAH when it is submitted, a committee claim charges its
// limit with what it drew, a cancelled claim's coins wait uncommitted for the next, and settlement
// pays from custody in the claim's denomination.
func TestNativeClaimsConversion(t *testing.T) {
	// The disbursement fixture prices axdr and ausd at one NOAH each and keeps the rates fresh.
	f := newNativeDisbursement(t)
	insurance := authtypes.NewModuleAddress(claimstypes.InsuranceName)
	claimant, committee := nativeAddress("claimant"), nativeAddress("claims-committee")
	funder := nativeAddress("insurance-funder")
	apptestutil.FundAccount(t, f.app, f.ctx, funder, sdk.NewCoins(chain.NoahCoin(math.NewInt(20_000))))
	require.NoError(t, f.exec(&banktypes.MsgSend{FromAddress: funder.String(), ToAddress: insurance.String(), Amount: sdk.NewCoins(chain.NoahCoin(math.NewInt(20_000)))}))
	params, err := f.app.ClaimsKeeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.exec(&claimstypes.MsgSetClaimsMandate{
		Authority: f.authority.String(), Committee: committee.String(), ActivationTime: f.now(), ExpiryTime: f.now() + 4*params.ClaimCancellationPeriod,
		CommitteeClaimLimit: math.NewInt(10_000),
	}))
	balance := func(denom string) math.Int { return f.app.BankKeeper.GetBalance(f.ctx, insurance, denom).Amount }
	uncommitted := func(denom string) math.Int {
		u, err := f.app.ClaimsKeeper.Uncommitted(f.ctx, denom)
		require.NoError(t, err)
		return u
	}
	claim := func(id uint64) claimstypes.Claim {
		c, err := f.app.ClaimsKeeper.Claims.Get(f.ctx, id)
		require.NoError(t, err)
		return c
	}

	// A governance claim converts the whole amount: the 2% spread floor makes 2,000 ausd cost more
	// than 2,000 NOAH, and a cap below that refuses the claim without trading.
	submit := func(amount sdk.Coin, maxOffer int64) error {
		return f.exec(&claimstypes.MsgSubmitClaim{Authority: f.authority.String(), Reference: "incident", Recipient: claimant.String(), Amount: amount, MaxOffer: math.NewInt(maxOffer)})
	}
	require.ErrorContains(t, submit(sdk.NewInt64Coin("ausd", 2_000), 2_000), "maximum offer exceeded")
	require.True(t, balance(chain.NoahBaseDenom).Equal(math.NewInt(20_000)), "a refused claim buys nothing")
	require.NoError(t, submit(sdk.NewInt64Coin("ausd", 2_000), 3_000))
	first := claim(1)
	require.True(t, first.Charge.IsZero(), "a governance claim charges nothing")
	spent := math.NewInt(20_000).Sub(balance(chain.NoahBaseDenom))
	require.True(t, spent.GT(math.NewInt(2_000)) && spent.LTE(math.NewInt(3_000)), "spent %s", spent)
	require.True(t, balance("ausd").GTE(math.NewInt(2_000)))
	converted := f.events(t, &claimstypes.EventClaimConverted{})
	require.Len(t, converted, 1)
	require.True(t, converted[0].(*claimstypes.EventClaimConverted).Offer.Equal(spent))
	// The bought denomination is an attribute of its own, so a node query can match it.
	require.Equal(t, `"ausd"`, attributesOf(f.emitted, "ark.claims.v1.EventClaimConverted")[0]["amount_denom"])

	// A committee claim charges its limit with the NOAH its buy cost, not the claim's face value.
	usedBefore, err := f.app.ClaimsKeeper.ClaimsAllowanceUsed.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, usedBefore.IsZero())
	noahBefore, surplus := balance(chain.NoahBaseDenom), uncommitted("ausd")
	require.True(t, surplus.LT(math.NewInt(1_000)), "the first buy's surplus is a few units")
	require.NoError(t, f.exec(&claimstypes.MsgCommitteeSubmitClaim{
		Committee: committee.String(), ExpectedTerm: 1, Reference: "incident", Recipient: claimant.String(),
		Amount: sdk.NewInt64Coin("ausd", 1_000), MaxOffer: math.NewInt(2_000),
	}))
	second := claim(2)
	// At one NOAH a unit, the charge is the surplus custody drawn plus the NOAH offer.
	offer := noahBefore.Sub(balance(chain.NoahBaseDenom))
	require.True(t, second.Charge.Equal(surplus.Add(offer)), "charge %s, surplus %s, offer %s", second.Charge, surplus, offer)
	require.True(t, second.Charge.GT(math.NewInt(1_000)), "the spread counts against the limit")
	used, err := f.app.ClaimsKeeper.ClaimsAllowanceUsed.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, used.Equal(second.Charge))

	// Cancelling leaves the bought coins uncommitted, and the next claim in the denomination draws
	// them before converting anything.
	require.NoError(t, f.exec(&claimstypes.MsgCancelClaim{Authority: f.authority.String(), ClaimId: 2}))
	require.True(t, uncommitted("ausd").GTE(math.NewInt(1_000)))
	noahBefore = balance(chain.NoahBaseDenom)
	require.NoError(t, submit(sdk.NewInt64Coin("ausd", 1_000), 0))
	require.True(t, balance(chain.NoahBaseDenom).Equal(noahBefore), "custody funds the claim")

	// Settlement pays both pending claims from custody in their denomination.
	f.advance(int64(params.ClaimCancellationPeriod))
	require.NoError(t, f.app.ClaimsKeeper.EndBlocker(f.ctx))
	require.Equal(t, claimstypes.ClaimStatus_CLAIM_STATUS_PAID, claim(1).Status)
	require.Equal(t, claimstypes.ClaimStatus_CLAIM_STATUS_PAID, claim(3).Status)
	require.True(t, f.app.BankKeeper.GetBalance(f.ctx, claimant, "ausd").Amount.Equal(math.NewInt(3_000)))
	exported, err := f.app.ClaimsKeeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.True(t, exported.InsuranceReserved.Empty())
	require.NoError(t, exported.Validate())
	recognised, err := f.app.ClaimsKeeper.RecognisedCapital(f.ctx)
	require.NoError(t, err)
	require.True(t, recognised.Equal(balance(chain.NoahBaseDenom)), "stablecoin custody counts for nothing")
}
