package keeper_test

import (
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/types"
)

// expectBurn stubs one burn of exactly these coins from the Reserve account.
func (s *KeeperTestSuite) expectBurn(amounts sdk.Coins) {
	s.bankKeeper.EXPECT().
		BurnCoins(gomock.Any(), types.StrategicReserveName, amounts).
		Return(nil)
}

func (s *KeeperTestSuite) TestBurnReserveAssets() {
	s.Run("burns custody and honours the proposal floor", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 40)
		burn := sdk.NewCoins(noahCoin(300), paperCoin(40))
		s.expectBurn(burn)

		_, err := s.msgServer.BurnReserveAssets(s.ctx, &types.MsgBurnReserveAssets{
			Authority:             s.authority,
			Amounts:               burn,
			MinimumReserveBalance: noahCoin(700),
		})
		s.Require().NoError(err)
	})

	s.Run("refuses to breach the proposal floor", func() {
		s.SetupTest()
		s.fundReserve(1_000)

		_, err := s.msgServer.BurnReserveAssets(s.ctx, &types.MsgBurnReserveAssets{
			Authority:             s.authority,
			Amounts:               sdk.NewCoins(noahCoin(301)),
			MinimumReserveBalance: noahCoin(700),
		})
		s.Require().ErrorContains(err, "cannot burn")
	})

	// Governance may take the fund to zero: the floor is a stale-state guard on
	// the proposal, not a protocol minimum, exactly as on the Buffer transfer.
	s.Run("zero floor permits burning the whole balance", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.expectBurn(sdk.NewCoins(noahCoin(1_000)))

		_, err := s.msgServer.BurnReserveAssets(s.ctx, &types.MsgBurnReserveAssets{
			Authority:             s.authority,
			Amounts:               sdk.NewCoins(noahCoin(1_000)),
			MinimumReserveBalance: noahCoin(0),
		})
		s.Require().NoError(err)
	})

	// Governance burns named coins without consulting recognition policy. External symbols cannot
	// enter Bank custody, so this tests the authority predicate without constructing an unreachable
	// balance.
	s.Run("burns credit-bearing custody", func() {
		s.SetupTest()
		s.setPolicy(eligibility(testAsset, "1", "0.01"))
		s.expectBurn(sdk.NewCoins(assetCoin(40)))

		_, err := s.msgServer.BurnReserveAssets(s.ctx, &types.MsgBurnReserveAssets{
			Authority:             s.authority,
			Amounts:               sdk.NewCoins(assetCoin(40)),
			MinimumReserveBalance: noahCoin(0),
		})
		s.Require().NoError(err)
	})

	s.Run("rejects invalid requests", func() {
		tests := []struct {
			name string
			msg  *types.MsgBurnReserveAssets
			want string
		}{
			{
				name: "wrong authority",
				msg: &types.MsgBurnReserveAssets{
					Authority:             testAddress(9),
					Amounts:               sdk.NewCoins(noahCoin(1)),
					MinimumReserveBalance: noahCoin(0),
				},
				want: "invalid authority",
			},
			{
				name: "no coins",
				msg: &types.MsgBurnReserveAssets{
					Authority:             s.authority,
					Amounts:               sdk.NewCoins(),
					MinimumReserveBalance: noahCoin(0),
				},
				want: "must name at least one coin",
			},
			{
				name: "zero coin",
				msg: &types.MsgBurnReserveAssets{
					Authority:             s.authority,
					Amounts:               sdk.Coins{noahCoin(0)},
					MinimumReserveBalance: noahCoin(0),
				},
				want: "amount is not positive",
			},
			{
				name: "negative coin",
				msg: &types.MsgBurnReserveAssets{
					Authority:             s.authority,
					Amounts:               sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}},
					MinimumReserveBalance: noahCoin(0),
				},
				want: "amount is not positive",
			},
			{
				name: "non-noah floor",
				msg: &types.MsgBurnReserveAssets{
					Authority:             s.authority,
					Amounts:               sdk.NewCoins(noahCoin(1)),
					MinimumReserveBalance: assetCoin(1),
				},
				want: "must be denominated in",
			},
		}

		for _, tc := range tests {
			s.Run(tc.name, func() {
				s.SetupTest()
				s.fundReserve(1_000)
				_, err := s.msgServer.BurnReserveAssets(s.ctx, tc.msg)
				s.Require().ErrorContains(err, tc.want)
			})
		}
	})

	s.Run("propagates a bank failure", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.bankKeeper.EXPECT().
			BurnCoins(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("bank rejected"))

		_, err := s.msgServer.BurnReserveAssets(s.ctx, &types.MsgBurnReserveAssets{
			Authority:             s.authority,
			Amounts:               sdk.NewCoins(noahCoin(10)),
			MinimumReserveBalance: noahCoin(0),
		})
		s.Require().ErrorContains(err, "bank rejected")
	})
}

// TestCommitteeBurnPaper checks both burn conditions: registry paper extinguishes protocol
// liability, and zero recognition credit prevents loss of recognised capital.
func (s *KeeperTestSuite) TestCommitteeBurnPaper() {
	committee := testAddress(1)
	destination := testAddress(2)

	// Written-off Ark-issued stablecoin routed here by Treasury settlement is
	// the flow this message exists for: unlisted, so it earns nothing, and a
	// registry member, so burning it extinguishes a consolidated liability.
	s.Run("burns unlisted Ark-issued residue", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 25)
		burn := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 25))
		s.expectBurn(burn)

		_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{
			Committee: committee, ExpectedTerm: 1, Amounts: burn,
		})
		s.Require().NoError(err)
	})

	// Paper bought back through a position is the other flow this message
	// serves, and it arrives by membership with no entry to consult — the
	// recognition policy cannot name a member at all.
	s.Run("burns Ark-issued paper bought back through a position", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 40)
		// An external asset listed beside it changes nothing: the burned coin
		// has no entry, because it cannot have one.
		s.setPolicy(eligibility(testAsset, "0.8", "0.2"))
		burn := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 40))
		s.expectBurn(burn)

		_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{
			Committee: committee, ExpectedTerm: 1, Amounts: burn,
		})
		s.Require().NoError(err)
	})

	// External custody has no protocol liability to extinguish. Its rejection precedes balance
	// reads; no external-symbol Bank balance is reachable.
	s.Run("refuses external custody, listed or not", func() {
		tests := []struct {
			name    string
			entries []types.EligibilityEntry
		}{
			{name: "unlisted"},
			{name: "listed", entries: []types.EligibilityEntry{eligibility(testAsset, "0.5", "0.01")}},
		}

		for _, tc := range tests {
			s.Run(tc.name, func() {
				s.SetupTest()
				s.appointCommittee(committee, 1_000, 0, destination)
				s.setBlockHeight(20)
				s.setPolicy(tc.entries...)

				// No burn expectation: the refusal precedes any coin movement.
				_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{
					Committee: committee, ExpectedTerm: 1, Amounts: sdk.NewCoins(assetCoin(40)),
				})
				s.Require().ErrorContains(err, "is not an Ark-issued asset")
			})
		}
	})

	// One external coin refuses the whole set, exactly as one credit-bearing
	// coin does: a burn is atomic and a mixed request must not partly execute.
	s.Run("refuses a set mixing Ark-issued and external custody", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 25)

		_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{
			Committee:    committee,
			ExpectedTerm: 1,
			Amounts:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 25), assetCoin(40)),
		})
		s.Require().ErrorContains(err, "is not an Ark-issued asset")
	})

	// Governance's burn carries neither the membership predicate nor the credit
	// one, which is the authority asymmetry pinned here. It is not a second
	// disposal path for external custody: no such balance can exist for Bank to
	// destroy, so what governance can actually reach this way is NOAH and paper.
	s.Run("governance may burn what the committee cannot", func() {
		s.SetupTest()
		s.setBlockHeight(20)
		s.expectBurn(sdk.NewCoins(assetCoin(40)))

		_, err := s.msgServer.BurnReserveAssets(s.ctx, &types.MsgBurnReserveAssets{
			Authority:             s.authority,
			Amounts:               sdk.NewCoins(assetCoin(40)),
			MinimumReserveBalance: noahCoin(0),
		})
		s.Require().NoError(err)
	})

	// The credit predicate still binds, and reaching it takes a denomination
	// listed for credit before it is ever registered as Ark-issued. This is
	// what stands between that state and a committee burning custody the
	// capital system counts.
	s.Run("refuses credit-bearing custody a later registration created", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.setPolicy(eligibility(testAsset, "0.5", "0.01"))
		// Registration after listing: the policy write saw nothing to refuse.
		s.registerAsset(testAsset)

		_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{
			Committee: committee, ExpectedTerm: 1, Amounts: sdk.NewCoins(assetCoin(40)),
		})
		s.Require().ErrorContains(err, "carries recognition credit")
	})

	// One credit-bearing coin refuses the whole set: a burn is atomic, and a
	// mixed request must not partially execute.
	s.Run("refuses a set containing credit-bearing custody", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 25)
		s.setPolicy(eligibility(testAsset, "0.5", "0.01"))
		s.registerAsset(testAsset)

		_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{
			Committee:    committee,
			ExpectedTerm: 1,
			Amounts: sdk.NewCoins(
				sdk.NewInt64Coin(chain.USDBaseDenom, 25),
				assetCoin(40),
			),
		})
		s.Require().ErrorContains(err, "carries recognition credit")
	})

	s.Run("refuses NOAH", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)

		_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{
			Committee: committee, ExpectedTerm: 1, Amounts: sdk.NewCoins(noahCoin(10)),
		})
		s.Require().ErrorContains(err, "must not include anoah")
	})

	s.Run("refuses a stale term", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)

		_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{
			Committee: committee, ExpectedTerm: 2,
			Amounts: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 25)),
		})
		s.Require().ErrorContains(err, "term")
	})
}

// TestCommitteeBurnSurplus pins the bound that makes a supply-moving committee
// act safe: the keeper derives it, and it stops exactly at the target line.
func (s *KeeperTestSuite) TestCommitteeBurnSurplus() {
	committee := testAddress(1)
	destination := testAddress(2)

	// activeCommittee appoints and funds a committee able to burn surplus.
	activeCommittee := func(balance, floor, required int64) {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, floor, destination)
		s.setBlockHeight(20)
		s.fundReserve(balance)
		s.treasuryReader.required = math.NewInt(required)
	}

	s.Run("burns up to the surplus", func() {
		activeCommittee(1_000, 0, 600)
		s.expectBurn(sdk.NewCoins(noahCoin(250)))

		resp, err := s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(250),
		})
		s.Require().NoError(err)
		// 1,000 recognised against a 600 requirement leaves 400 surplus; 150
		// remains after this burn.
		s.Require().Equal(noahCoin(150), resp.RemainingSurplus)
	})

	s.Run("refuses to burn past the requirement", func() {
		activeCommittee(1_000, 0, 600)

		_, err := s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(401),
		})
		s.Require().ErrorContains(err, "exceeds the burnable Reserve surplus")
	})

	s.Run("refuses when the fund is at or below its requirement", func() {
		activeCommittee(1_000, 0, 1_000)

		_, err := s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(1),
		})
		s.Require().ErrorContains(err, "exceeds the burnable Reserve surplus 0anoah")
	})

	// The mandate floor binds separately from the requirement, because
	// recognised capital counts attested holdings that cannot pay for anything.
	s.Run("the mandate floor bounds the burn", func() {
		activeCommittee(500, 400, 0)
		s.expectBurn(sdk.NewCoins(noahCoin(100)))

		resp, err := s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(100),
		})
		s.Require().NoError(err)
		s.Require().True(resp.RemainingSurplus.Amount.IsZero())

		_, err = s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(101),
		})
		s.Require().ErrorContains(err, "exceeds the burnable Reserve surplus")
	})

	// Recognised capital counts credited assets, so surplus can exist while the
	// account holds little NOAH — and the burn is still bounded by what the
	// account can actually part with.
	s.Run("attested credit cannot be burned", func() {
		activeCommittee(100, 0, 0)
		s.attest(testAsset, 900)
		s.setPolicy(eligibility(testAsset, "1", "0.01"))
		s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})

		// Recognised is 1,000 against a zero requirement, but only 100 NOAH is
		// spendable.
		surplus, err := s.keeper.BurnableSurplus(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(100), surplus)
	})

	// An unavailable Reserve requirement freezes disposal. Transfers into Buffer and Insurance
	// remain possible against their available shortfall bounds.
	s.Run("refuses while valuation is incomplete", func() {
		activeCommittee(1_000, 0, 0)
		s.treasuryReader.requiredErr = errors.New("aggregate liability valuation is incomplete")

		_, err := s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(1),
		})
		s.Require().ErrorContains(err, "incomplete")
	})

	s.Run("refuses when the requirement reader is unwired", func() {
		activeCommittee(1_000, 0, 0)
		s.keeper.SetTreasuryCapitalReader(nil)

		_, err := s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(1),
		})
		s.Require().ErrorContains(err, "not wired")
	})

	s.Run("rejects invalid amounts", func() {
		tests := []struct {
			name   string
			amount sdk.Coin
			want   string
		}{
			{name: "zero", amount: noahCoin(0), want: "burn amount must be positive"},
			{name: "non-noah", amount: assetCoin(1), want: "burn amount must be denominated in anoah"},
		}

		for _, tc := range tests {
			s.Run(tc.name, func() {
				activeCommittee(1_000, 0, 0)
				_, err := s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{
					Committee: committee, ExpectedTerm: 1, Amount: tc.amount,
				})
				s.Require().ErrorContains(err, tc.want)
			})
		}
	})
}

// TestRecognitionPolicyRefusesArkIssuedEntries pins that an Ark-issued
// denomination cannot appear in the recognition policy because it cannot be
// spelled as an entry at all: the two namespaces are disjoint by shape. No
// registry is consulted below, which is the point.
func (s *KeeperTestSuite) TestRecognitionPolicyRefusesArkIssuedEntries() {
	// Every credit level is refused, including the custody-only shapes.
	s.Run("refuses every entry naming a bare denomination", func() {
		tests := []struct {
			name  string
			entry types.EligibilityEntry
		}{
			{name: "credit bearing", entry: eligibility(chain.USDBaseDenom, "1", "0.1")},
			{name: "zero haircut", entry: eligibility(chain.USDBaseDenom, "0", "0.1")},
			{name: "zero cap", entry: eligibility(chain.USDBaseDenom, "1", "0")},
			{name: "zero both", entry: eligibility(chain.USDBaseDenom, "0", "0")},
		}

		for _, tc := range tests {
			s.Run(tc.name, func() {
				s.SetupTest()

				_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
					Authority: s.authority,
					Entries:   []types.EligibilityEntry{tc.entry},
				})
				s.Require().ErrorContains(err, "must name a feed and a tag")
				s.Require().Empty(s.policyDenoms())
			})
		}
	})

	// The refusal does not wait for the denomination to be registered, which is
	// the ordering the registry read could not cover: the name is refused while
	// no asset carries it, so registering one later changes nothing.
	s.Run("refuses a bare denomination no asset carries yet", func() {
		s.SetupTest()

		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries:   []types.EligibilityEntry{eligibility("afuture", "1", "0.1")},
		})
		s.Require().ErrorContains(err, "must name a feed and a tag")
		s.Require().Empty(s.policyDenoms())
	})

	// An external symbol beside a bare denomination fails with it: the policy is replaced
	// whole-for-whole, so a refused proposal must leave nothing behind.
	s.Run("refuses a set mixing a bare denomination with externals", func() {
		s.SetupTest()
		s.registerAsset(chain.USDBaseDenom)

		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries: []types.EligibilityEntry{
				eligibility(testAsset, "0.8", "0.2"),
				eligibility(chain.USDBaseDenom, "0.8", "0.2"),
			},
		})
		s.Require().ErrorContains(err, "must name a feed and a tag")
		s.Require().Empty(s.policyDenoms())
	})

	// An external symbol on the very series an Ark-issued asset is named after is
	// which is the capability the partition exists to give: the protocol may
	// issue ausd and the fund may hold actual USD, and the books say which.
	s.Run("admits an external symbol on a registered asset's series", func() {
		s.SetupTest()
		s.registerAsset(chain.USDBaseDenom)

		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries:   []types.EligibilityEntry{eligibility(usdExternal, "0.8", "0.2")},
		})
		s.Require().NoError(err)
		s.Require().Equal([]string{usdExternal}, s.policyDenoms())
	})

	s.Run("genesis refuses what a proposal would be refused", func() {
		s.SetupTest()
		s.registerAsset(chain.USDBaseDenom)

		data := types.DefaultGenesisState()
		data.RecognitionPolicy = []types.EligibilityEntry{
			eligibility(chain.USDBaseDenom, "0.8", "0.2"),
		}
		s.Require().ErrorContains(
			s.keeper.InitGenesis(s.ctx, data),
			"must name a feed and a tag",
		)
	})

	// Eligibility requires an active underlying feed. Adding feeds lack established observations;
	// removing feeds have already passed their referent check and cannot acquire new dependants
	// before promotion.
	s.Run("refuses an external symbol whose series is not an active feed", func() {
		for _, tc := range []struct {
			name  string
			phase oracletypes.FeedPhase
		}{
			{"off", oracletypes.FeedPhaseOff},
			{"adding", oracletypes.FeedPhaseAdding},
			{"removing", oracletypes.FeedPhaseRemoving},
		} {
			s.Run(tc.name, func() {
				s.SetupTest()
				s.feedPhases[testFeed] = tc.phase

				_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
					Authority: s.authority,
					Entries:   []types.EligibilityEntry{eligibility(testAsset, "1", "0.1")},
				})
				s.Require().ErrorContains(err, "not an active feed")
				s.Require().Empty(s.policyDenoms())

				// Genesis rejects policies that a live governance proposal cannot write, preserving
				// export/import symmetry.
				data := types.DefaultGenesisState()
				data.RecognitionPolicy = []types.EligibilityEntry{eligibility(testAsset, "1", "0.1")}
				s.Require().ErrorContains(
					s.keeper.InitGenesis(s.ctx, data),
					"not an active feed",
				)
			})
		}
	})
}
