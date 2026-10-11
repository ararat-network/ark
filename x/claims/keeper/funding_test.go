package keeper_test

import (
	"errors"

	"github.com/cosmos/gogoproto/proto"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/claims/types"
)

// usd is a whole-unit amount of the suite's registered stablecoin.
func usd(amount int64) sdk.Coin {
	return sdk.NewInt64Coin(chain.USDBaseDenom, amount)
}

// conversions returns every claim conversion event the block has published.
func (s *KeeperTestSuite) conversions() []*types.EventClaimConverted {
	var out []*types.EventClaimConverted
	for _, e := range sdk.UnwrapSDKContext(s.ctx).EventManager().ABCIEvents() {
		if e.Type != proto.MessageName(&types.EventClaimConverted{}) {
			continue
		}
		parsed, err := sdk.ParseTypedEvent(e)
		s.Require().NoError(err)
		out = append(out, parsed.(*types.EventClaimConverted))
	}
	return out
}

func (s *KeeperTestSuite) requireInt(got math.Int, want int64) {
	s.T().Helper()
	s.Require().True(got.Equal(math.NewInt(want)), "got %s, want %d", got, want)
}

func (s *KeeperTestSuite) claim(id uint64) types.Claim {
	claim, err := s.keeper.Claims.Get(s.ctx, id)
	s.Require().NoError(err)
	return claim
}

func (s *KeeperTestSuite) uncommitted(denom string) math.Int {
	uncommitted, err := s.keeper.Uncommitted(s.ctx, denom)
	s.Require().NoError(err)
	return uncommitted
}

// TestStablecoinClaimDrawsCustodyFirst funds a claim custody covers without touching Market.
func (s *KeeperTestSuite) TestStablecoinClaimDrawsCustodyFirst() {
	s.SetupTest()
	s.atSubmission()
	s.fundInsurance(1_000)
	s.hold(usd(500))

	id, err := s.submitGovernance(usd(100), 0)
	s.Require().NoError(err)

	reserved, err := s.reserved(chain.USDBaseDenom)
	s.Require().NoError(err)
	s.requireInt(reserved, 100)
	s.requireInt(s.uncommitted(chain.USDBaseDenom), 400)
	s.requireInt(s.uncommitted(chain.NoahBaseDenom), 1_000)
	s.Require().Empty(s.conversions())
	s.Require().True(s.claim(id).Charge.IsZero())
}

// TestStablecoinClaimConvertsTheShortfall buys only what custody lacks; Market's surplus unit stays
// uncommitted.
func (s *KeeperTestSuite) TestStablecoinClaimConvertsTheShortfall() {
	s.SetupTest()
	s.atSubmission()
	s.fundInsurance(1_000)
	s.hold(usd(30))
	s.expectBuy(usd(70), usd(71), math.NewInt(200), math.NewInt(60))

	id, err := s.submitGovernance(usd(100), 200)
	s.Require().NoError(err)

	reserved, err := s.reserved(chain.USDBaseDenom)
	s.Require().NoError(err)
	s.requireInt(reserved, 100)
	s.requireInt(s.uncommitted(chain.USDBaseDenom), 1)
	s.requireInt(s.uncommitted(chain.NoahBaseDenom), 940)
	events := s.conversions()
	s.Require().Len(events, 1)
	s.Require().Equal(id, events[0].ClaimId)
	s.Require().Equal(chain.USDBaseDenom, events[0].AmountDenom)
	s.requireInt(events[0].Amount, 71)
	s.requireInt(events[0].Offer, 60)
}

func (s *KeeperTestSuite) TestStablecoinClaimFundingRefusals() {
	tests := []struct {
		name      string
		amount    sdk.Coin
		maxOffer  int64
		arrange   func()
		expectErr string
	}{
		{
			name:      "shortfall without a max offer",
			amount:    usd(100),
			arrange:   func() { s.hold(usd(30)) },
			expectErr: "custody holds 30ausd of 100ausd; set max_offer",
		},
		{
			name:      "NOAH claim with a max offer",
			amount:    noahCoin(100),
			maxOffer:  1,
			expectErr: "a NOAH claim converts nothing",
		},
		{
			name:      "suspended asset",
			amount:    usd(100),
			arrange:   func() { s.assets[chain.USDBaseDenom] = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED },
			expectErr: "a new claim requires an active asset",
		},
		{
			// A stablecoin's reservation never borrows NOAH coverage: the offer must come out of NOAH no
			// pending claim holds.
			name:     "offer past uncommitted NOAH",
			amount:   usd(100),
			maxOffer: 2_000,
			arrange: func() {
				s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, chain.NoahBaseDenom, math.NewInt(950)))
				s.expectBuy(usd(100), usd(100), math.NewInt(2_000), math.NewInt(60))
			},
			expectErr: "exceeds uncommitted Insurance NOAH",
		},
		{
			name:     "Market refuses the buy",
			amount:   usd(100),
			maxOffer: 50,
			arrange: func() {
				s.marketKeeper.EXPECT().Buy(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, errors.New("maximum offer exceeded"))
			},
			expectErr: "funding 100ausd: maximum offer exceeded",
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.atSubmission()
			s.fundInsurance(1_000)
			if tc.arrange != nil {
				tc.arrange()
			}
			_, err := s.submitGovernance(tc.amount, tc.maxOffer)
			s.Require().ErrorContains(err, tc.expectErr)
		})
	}
}

// TestCommitteeClaimCharge values what each claim drew in the reference unit: the suite prices the
// reference at one NOAH and the stablecoin at eight tenths.
func (s *KeeperTestSuite) TestCommitteeClaimCharge() {
	tests := []struct {
		name    string
		amount  sdk.Coin
		offer   int64
		arrange func()
		charge  int64
	}{
		{name: "NOAH charges its amount", amount: noahCoin(100), charge: 100},
		{
			name:    "custody charges its value",
			amount:  usd(101),
			arrange: func() { s.hold(usd(500)) },
			// 101 × 0.8 = 80.8, rounded up.
			charge: 81,
		},
		{
			name:   "a conversion charges custody and the NOAH it cost",
			amount: usd(100),
			offer:  200,
			arrange: func() {
				s.hold(usd(30))
				s.expectBuy(usd(70), usd(70), math.NewInt(200), math.NewInt(60))
			},
			// 30 × 0.8 = 24 of custody and 60 NOAH, not the claim's face value of 80.
			charge: 84,
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.atSubmission()
			s.appointCommittee(testAddress(1))
			s.fundInsurance(5_000)
			if tc.arrange != nil {
				tc.arrange()
			}
			id, err := s.submitCommittee(tc.amount, tc.offer)
			s.Require().NoError(err)
			s.requireInt(s.claim(id).Charge, tc.charge)
			used, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
			s.Require().NoError(err)
			s.requireInt(used, tc.charge)
		})
	}
}

// TestCommitteeClaimChargesTheSpread pins why the limit charges what a claim drew: a claim whose face
// value fits the limit is refused when its buy costs past it, since cancelling would not undo the
// spread.
func (s *KeeperTestSuite) TestCommitteeClaimChargesTheSpread() {
	s.SetupTest()
	s.atSubmission()
	s.appointCommittee(testAddress(1))
	s.fundInsurance(5_000)
	s.expectBuy(usd(1_000), usd(1_000), math.NewInt(5_000), math.NewInt(1_500))

	// 1,000 × 0.8 = 800 fits the limit of 1,000; the 1,500 NOAH the buy cost does not.
	_, err := s.submitCommittee(usd(1_000), 5_000)
	s.Require().ErrorContains(err, "claim charge 1500 takes Claims allowance usage to 1500, past mandate limit 1000")
}

// TestCommitteeClaimNeedsAFreshRate refuses a committee claim the oracle cannot value, while a
// governance claim, which charges nothing, never reads the oracle.
func (s *KeeperTestSuite) TestCommitteeClaimNeedsAFreshRate() {
	s.SetupTest()
	s.atSubmission()
	s.appointCommittee(testAddress(1))
	s.fundInsurance(5_000)
	delete(s.rates, chain.XDRBaseDenom)

	_, err := s.submitCommittee(noahCoin(100), 0)
	s.Require().ErrorContains(err, "valuing the claim")

	_, err = s.submitGovernance(noahCoin(100), 0)
	s.Require().NoError(err)
}

// TestCancelledStablecoinClaimLeavesCustody keeps a cancelled claim's bought coins uncommitted, and
// the next claim in the denomination draws them before converting anything.
func (s *KeeperTestSuite) TestCancelledStablecoinClaimLeavesCustody() {
	s.SetupTest()
	s.atSubmission()
	s.fundInsurance(1_000)
	s.expectBuy(usd(100), usd(100), math.NewInt(200), math.NewInt(130))
	id, err := s.submitGovernance(usd(100), 200)
	s.Require().NoError(err)

	_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{Authority: s.authority, ClaimId: id})
	s.Require().NoError(err)
	reserved, err := s.reserved(chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero())
	s.requireInt(s.uncommitted(chain.USDBaseDenom), 100)
	_, err = s.keeper.InsuranceReserved.Get(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err, "a released denomination leaves no zero entry")

	// No second buy is expected; the mock fails the test if one happens.
	_, err = s.submitGovernance(usd(100), 0)
	s.Require().NoError(err)
	s.requireInt(s.uncommitted(chain.USDBaseDenom), 0)
}

// TestEndBlockerPaysStablecoinClaims pays a stablecoin claim from custody, even after its asset is
// suspended, which leaves transfers untouched.
func (s *KeeperTestSuite) TestEndBlockerPaysStablecoinClaims() {
	s.SetupTest()
	s.atSubmission()
	s.fundInsurance(1_000)
	s.hold(usd(100))
	id, err := s.submitGovernance(usd(100), 0)
	s.Require().NoError(err)
	s.assets[chain.USDBaseDenom] = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED

	s.expectPayouts(1)
	s.setBlockTime(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(id, types.ClaimStatus_CLAIM_STATUS_PAID)
	reserved, err := s.reserved(chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero())
	s.Require().True(s.insurance.AmountOf(chain.USDBaseDenom).IsZero())
	s.requireInt(s.uncommitted(chain.NoahBaseDenom), 1_000)
}

// TestEndBlockerFailedStablecoinClaimLeavesCustody fails a claim Bank refuses and keeps its coins
// uncommitted for a fresh claim to a payable recipient.
func (s *KeeperTestSuite) TestEndBlockerFailedStablecoinClaimLeavesCustody() {
	s.SetupTest()
	s.atSubmission()
	s.hold(usd(100))
	id, err := s.submitGovernance(usd(100), 0)
	s.Require().NoError(err)

	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToAccount(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("recipient accepts NOAH only"))
	s.setBlockTime(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(id, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireInt(s.uncommitted(chain.USDBaseDenom), 100)
}
