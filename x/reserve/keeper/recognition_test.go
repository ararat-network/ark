package keeper_test

import (
	"context"
	"time"

	"go.uber.org/mock/gomock"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/types"
)

// testRateAge is the window test entries state. Rates the stub serves are age
// zero, so it admits them all; the cases that exercise the window state their
// own.
const testRateAge = time.Hour

// xdrExternal and usdExternal identify external claims; their prefixes identify the Oracle series.
// Reserve lists and holds claims, while Oracle prices series.
const (
	xdrExternal = chain.XDRBaseDenom + "-x"
	usdExternal = chain.USDBaseDenom + "-x"
)

// eligibility builds one policy entry. Both factors are ratios: the haircut
// scales the holding's own value, the cap ratio its permitted share of
// recognised capital itself.
func eligibility(denom, haircut, capRatio string) types.EligibilityEntry {
	return agedEligibility(denom, haircut, capRatio, testRateAge)
}

func agedEligibility(denom, haircut, capRatio string, maxRateAge time.Duration) types.EligibilityEntry {
	return types.EligibilityEntry{
		Denom:               denom,
		HaircutFactor:       math.LegacyMustNewDecFromStr(haircut),
		RecognitionCapRatio: math.LegacyMustNewDecFromStr(capRatio),
		MaxRateAge:          maxRateAge,
	}
}

// setPolicy replaces the recognition policy through the governance handler.
func (s *KeeperTestSuite) setPolicy(entries ...types.EligibilityEntry) {
	_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
		Authority: s.authority,
		Entries:   entries,
	})
	s.Require().NoError(err)
}

// policyDenoms collects the stored eligibility set's keys in stored order.
func (s *KeeperTestSuite) policyDenoms() []string {
	denoms := make([]string, 0)
	s.Require().NoError(s.keeper.RecognitionPolicy.Walk(s.ctx, nil, func(denom string, _ types.EligibilityEntry) (bool, error) {
		denoms = append(denoms, denom)
		return false, nil
	}))
	return denoms
}

// stubRates serves age-zero rates to both Oracle interfaces. Missing keys fail strict reads and are
// omitted from available reads; stubRatesAtAge exercises staleness.
func (s *KeeperTestSuite) stubRates(rates oracletypes.RateSet) {
	s.stubRatesAtAge(rates, 0)
}

func (s *KeeperTestSuite) stubRatesAtAge(rates oracletypes.RateSet, age time.Duration) {
	// An external symbol and its series are one price, so a fixture naming either answers
	// for both: the fold asks for the series, while a proven movement of that
	// custody asks for the coin. Mirroring here keeps a test that darkens a feed
	// darkening it for both reads, which is what a dark feed means.
	mirrored := func(denom string) (math.LegacyDec, bool) {
		if rate, known := rates[denom]; known {
			return rate, true
		}
		for symbol, rate := range rates {
			if feed, isExternal := chain.ExternalFeed(symbol); isExternal && feed == denom {
				return rate, true
			}
		}
		return math.LegacyDec{}, false
	}

	// The all-or-nothing read refuses the whole set on the first denomination
	// it cannot price, exactly as the Oracle's does: a movement stub that
	// quietly omitted a dark feed would let the keeper's own refusal go
	// untested.
	s.oracleKeeper.EXPECT().
		GetRateSet(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			answer := oracletypes.NewRateSet()
			for _, denom := range denoms {
				rate, known := mirrored(denom)
				if !known {
					return nil, errorsmod.Wrapf(
						oracletypes.ErrUnknownDenom,
						"getting exchange rate for denom %s",
						denom,
					)
				}
				answer[denom] = rate
			}
			return answer, nil
		}).
		AnyTimes()
	// The windowed read judges as the real one does: each request's window
	// against the fixture's one age, the series derived from the requested
	// name, the verdict answered under it.
	s.oracleKeeper.EXPECT().
		GetRateSetWithin(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, requests []oracletypes.RateRequest) (oracletypes.RateSet, error) {
			answer := oracletypes.NewRateSet()
			for _, request := range requests {
				if request.MaxAge <= 0 || age > request.MaxAge {
					continue
				}
				feed := request.Denom
				if derived, isExternal := chain.ExternalFeed(request.Denom); isExternal {
					feed = derived
				}
				if rate, known := mirrored(feed); known {
					answer[request.Denom] = rate
				}
			}
			return answer, nil
		}).
		AnyTimes()
}

// recognised reads RecognisedCapital, requiring success.
func (s *KeeperTestSuite) recognised() math.Int {
	recognised, err := s.keeper.RecognisedCapital(s.ctx)
	s.Require().NoError(err)
	return recognised
}

func (s *KeeperTestSuite) TestSetRecognitionPolicy() {
	s.Run("rejects invalid authority", func() {
		s.SetupTest()
		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: testAddress(9),
			Entries:   []types.EligibilityEntry{eligibility(testAsset, "1", "0.1")},
		})
		s.Require().ErrorContains(err, "invalid authority")
		s.Require().Empty(s.policyDenoms())
	})

	s.Run("replaces the set whole", func() {
		s.SetupTest()
		s.setPolicy(
			eligibility(xdrExternal, "1", "0.1"),
			eligibility(testAsset, "0.5", "0.2"),
		)
		s.Require().Equal([]string{testAsset, xdrExternal}, s.policyDenoms())

		s.setPolicy(eligibility(usdExternal, "1", "0.3"))
		s.Require().Equal([]string{usdExternal}, s.policyDenoms())

		s.setPolicy()
		s.Require().Empty(s.policyDenoms())
	})

	s.Run("emits the stored order rather than the proposal order", func() {
		s.SetupTest()
		s.setPolicy(
			eligibility(xdrExternal, "1", "0.1"),
			eligibility(testAsset, "0.5", "0.2"),
		)

		var denoms string
		for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
			if event.Type != "ark.reserve.v1.EventRecognitionPolicySet" {
				continue
			}
			for _, attribute := range event.Attributes {
				if attribute.Key == "denoms" {
					denoms = attribute.Value
				}
			}
		}
		s.Require().Equal(`["abill-x","axdr-x"]`, denoms)
	})

	s.Run("rejects duplicate entries", func() {
		s.SetupTest()
		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries: []types.EligibilityEntry{
				eligibility(testAsset, "1", "0.1"),
				eligibility(testAsset, "0.5", "0.2"),
			},
		})
		s.Require().ErrorContains(err, "duplicate eligibility entry")
	})

	// The numeraire is refused through the prefix rule, in the numeraire's own
	// words: an off-chain NOAH holding is deliberately unrecognisable, and naming
	// one is refused for the reason NOAH has no feed.
	s.Run("rejects the numeraire", func() {
		s.SetupTest()
		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries:   []types.EligibilityEntry{eligibility(chain.NoahBaseDenom+"-x", "1", "0.1")},
		})
		s.Require().ErrorContains(err, "never priced")
	})

	s.Run("rejects cap ratios summing to one", func() {
		s.SetupTest()
		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries: []types.EligibilityEntry{
				eligibility(xdrExternal, "1", "0.6"),
				eligibility(testAsset, "1", "0.4"),
			},
		})
		s.Require().ErrorContains(err, "must sum strictly below one")
		s.Require().Empty(s.policyDenoms())

		// A single entry claiming the whole certificate is the same refusal.
		_, err = s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries:   []types.EligibilityEntry{eligibility(testAsset, "1", "1")},
		})
		s.Require().ErrorContains(err, "must sum strictly below one")
	})

	s.Run("rejects invalid entries", func() {
		// Unset, negative, and zero are one rule and one message on each factor:
		// an entry exists to grant credit, so nothing short of positive is an
		// entry at all.
		const (
			wantHaircut  = "haircut factor must be greater than zero and at most one"
			wantCapRatio = "recognition cap ratio must be greater than zero and at most one"
		)
		tests := []struct {
			name  string
			entry types.EligibilityEntry
			want  string
		}{
			{
				name: "unset haircut",
				entry: types.EligibilityEntry{
					Denom:               testAsset,
					RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.1"),
				},
				want: wantHaircut,
			},
			{
				name:  "negative haircut",
				entry: eligibility(testAsset, "-0.1", "0.1"),
				want:  wantHaircut,
			},
			{
				// A zero factor either side grants no credit, which is what an
				// entry is for; such an entry is refused rather than stored inert,
				// so every stored entry raises a claim the feed guard can see.
				name:  "zero haircut",
				entry: eligibility(testAsset, "0", "0.1"),
				want:  wantHaircut,
			},
			{
				name:  "haircut above one",
				entry: eligibility(testAsset, "1.000000000000000001", "0.1"),
				want:  "haircut factor must be greater than zero and at most one",
			},
			{
				name: "unset cap ratio",
				entry: types.EligibilityEntry{
					Denom:         testAsset,
					HaircutFactor: math.LegacyOneDec(),
				},
				want: wantCapRatio,
			},
			{
				name:  "negative cap ratio",
				entry: eligibility(testAsset, "1", "-0.000000000000000001"),
				want:  wantCapRatio,
			},
			{
				name:  "zero cap ratio",
				entry: eligibility(testAsset, "1", "0"),
				want:  wantCapRatio,
			},
			{
				name:  "cap ratio above one",
				entry: eligibility(testAsset, "1", "1.000000000000000001"),
				want:  "recognition cap ratio must be greater than zero and at most one",
			},
		}

		for _, tc := range tests {
			s.Run(tc.name, func() {
				s.SetupTest()
				_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
					Authority: s.authority,
					Entries:   []types.EligibilityEntry{tc.entry},
				})
				s.Require().ErrorContains(err, tc.want)
			})
		}
	})
}

func (s *KeeperTestSuite) TestRecognisedCapitalPricesEligibleHoldings() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(1_000)
	s.attest(testAsset, 50)
	s.deploy(committee, destination, 400, 30, 0)
	s.setPolicy(eligibility(testAsset, "0.5", "0.1"))
	s.stubRates(oracletypes.RateSet{testAsset: math.LegacyNewDecWithPrec(5, 1)})

	// An oracle rate quotes NOAH per one unit of its asset, so one of this
	// asset is worth half a NOAH and the haircut quantity is multiplied by the
	// rate: credit = 0.5 × (50 + 30 attested across two positions) × 0.5 = 20
	// on top of par.
	s.Require().Equal(math.NewInt(1_020), s.recognised())

	response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
	s.Require().NoError(err)
	s.Require().Equal(noahCoin(1_020), response.Recognised)
	s.Require().Equal(noahCoin(1_000), response.NoahBalance)
	s.Require().Equal([]types.AssetRecognition{{
		Denom:               testAsset,
		Eligible:            true,
		AttestedQuantity:    math.NewInt(80),
		ImpairedQuantity:    math.ZeroInt(),
		Rate:                math.LegacyNewDecWithPrec(5, 1),
		HaircutFactor:       math.LegacyMustNewDecFromStr("0.5"),
		RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.1"),
		// A tenth of the solved certificate of 1_020: the ceiling the credit
		// was measured against, reported even when it does not bind.
		EffectiveCap: noahCoin(102),
		Credit:       noahCoin(20),
	}}, response.Assets)
}

// TestRecognitionJudgesEachEntryUnderItsOwnWindow pins the case the windowed
// read exists for: two entries on one series, each judged under its own
// tolerance, so a rate one entry may still credit is already too old for its
// neighbour — verdicts keyed by entry, never colliding on the shared feed.
func (s *KeeperTestSuite) TestRecognitionJudgesEachEntryUnderItsOwnWindow() {
	s.SetupTest()
	s.fundReserve(1_000)
	s.attest(chain.XDRBaseDenom+"-patient", 100)
	s.attest(chain.XDRBaseDenom+"-strict", 100)
	s.setPolicy(
		agedEligibility(chain.XDRBaseDenom+"-patient", "1", "0.4", 26*time.Hour),
		agedEligibility(chain.XDRBaseDenom+"-strict", "1", "0.4", time.Hour),
	)
	rates := oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)}

	// Fresh, both credit: two hundred anoah each at two NOAH to the XDR.
	s.stubRatesAtAge(rates, 0)
	s.Require().Equal(math.NewInt(1_000+200+200), s.recognised())

	// Two hours old: inside the patient window, past the strict one. The same
	// series backs one entry and refuses the other in the same block.
	s.SetupTest()
	s.fundReserve(1_000)
	s.attest(chain.XDRBaseDenom+"-patient", 100)
	s.attest(chain.XDRBaseDenom+"-strict", 100)
	s.setPolicy(
		agedEligibility(chain.XDRBaseDenom+"-patient", "1", "0.4", 26*time.Hour),
		agedEligibility(chain.XDRBaseDenom+"-strict", "1", "0.4", time.Hour),
	)
	s.stubRatesAtAge(rates, 2*time.Hour)
	s.Require().Equal(math.NewInt(1_000+200+0), s.recognised())
}

// TestRecognitionDegradesToZeroNeverToStale checks that an unavailable Oracle feed zeroes only its
// asset's credit. There is no governance-price fallback.
func (s *KeeperTestSuite) TestRecognitionDegradesToZeroNeverToStale() {
	s.SetupTest()
	s.fundReserve(1_000)
	s.attest(xdrExternal, 100)
	s.attest(usdExternal, 100)
	s.setPolicy(
		eligibility(xdrExternal, "1", "0.4"),
		eligibility(usdExternal, "1", "0.4"),
	)
	// Both rates sit above one, which is the ordinary case for an asset worth
	// more than a NOAH apiece: quoted in NOAH per unit, two NOAH to the XDR
	// reads as 2, and the hundred held is worth two hundred.
	rates := oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyNewDec(2),
		chain.USDBaseDenom: math.LegacyNewDec(4),
	}
	s.stubRates(rates)

	s.Require().Equal(math.NewInt(1_000+200+400), s.recognised())

	// One feed goes stale: that asset's credit vanishes, its neighbour is
	// untouched, and nothing stale survives anywhere.
	delete(rates, chain.USDBaseDenom)
	s.Require().Equal(math.NewInt(1_000+200+0), s.recognised())

	response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
	s.Require().NoError(err)
	// Rows come back keyed by denomination, so they are addressed by name here
	// rather than by position: the order is a consequence of the spelling.
	s.Require().Len(response.Assets, 2)
	byDenom := make(map[string]types.AssetRecognition, len(response.Assets))
	for _, asset := range response.Assets {
		byDenom[asset.Denom] = asset
	}
	s.Require().True(byDenom[xdrExternal].Rate.IsPositive())
	s.Require().Equal(noahCoin(200), byDenom[xdrExternal].Credit)
	// A dark feed leaves the rate at zero, which is the whole record of why
	// this row earns nothing: there is no other source it could have used.
	s.Require().Equal(math.LegacyZeroDec(), byDenom[usdExternal].Rate)
	s.Require().Equal(noahCoin(0), byDenom[usdExternal].Credit)

	// Both dark: the fund falls back to its NOAH balance alone.
	delete(rates, chain.XDRBaseDenom)
	s.Require().Equal(math.NewInt(1_000), s.recognised())
}

func (s *KeeperTestSuite) TestRecognitionCapClipsHoldingsJointly() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(900)
	s.attest(testAsset, 120)
	s.deploy(committee, destination, 400, 80, 0)
	s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})

	// The cap clips the joint holding: 120 + 80 attested = 200 raw,
	// but a tenth of the certificate solved over the 900 base is
	// T = 900 ÷ (1 − 0.1) = 1_000, so the asset carries exactly 100 — its
	// permitted share of the answer, to the anoah.
	s.setPolicy(eligibility(testAsset, "1", "0.1"))
	s.Require().Equal(math.NewInt(1_000), s.recognised())

	// Delisting removes the credit whole: there is no custody-only entry to
	// step down to — a zero factor is refused at the policy write — so listed
	// and credited are the same state, and unlisted is the other.
	s.setPolicy()
	s.Require().Equal(math.NewInt(900), s.recognised())
}

// TestRecognitionSolvesSharesJointly pins the shrinking-denominator solve on
// the worked fixtures from the design discussion: clipping one asset shrinks
// the total every other share is measured against, so the clipped set and the
// total are found together, not per asset.
func (s *KeeperTestSuite) TestRecognitionSolvesSharesJointly() {
	policy := func() {
		s.setPolicy(
			eligibility(usdExternal, "1", "0.3"),
			eligibility(xdrExternal, "1", "0.2"),
		)
	}
	rates := func() {
		s.stubRates(oracletypes.RateSet{
			chain.USDBaseDenom: math.LegacyOneDec(),
			chain.XDRBaseDenom: math.LegacyOneDec(),
		})
	}
	row := func(response *types.QueryRecognisedCapitalResponse, denom string) types.AssetRecognition {
		for _, asset := range response.Assets {
			if asset.Denom == denom {
				return asset
			}
		}
		s.T().Fatalf("no recognition row for %s", denom)
		return types.AssetRecognition{}
	}

	s.Run("clips both assets to shares of the joint answer", func() {
		s.SetupTest()
		s.fundReserve(5_000_000)
		s.attest(usdExternal, 4_000_000)
		s.attest(xdrExternal, 3_000_000)
		policy()
		rates()

		// T = 5_000_000 ÷ (1 − 0.3 − 0.2) = 10_000_000: both assets breach,
		// and at the solved total each holds exactly its ratio.
		s.Require().Equal(math.NewInt(10_000_000), s.recognised())
		response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(3_000_000), row(response, usdExternal).Credit)
		s.Require().Equal(noahCoin(3_000_000), row(response, usdExternal).EffectiveCap)
		s.Require().Equal(noahCoin(2_000_000), row(response, xdrExternal).Credit)
		s.Require().Equal(noahCoin(2_000_000), row(response, xdrExternal).EffectiveCap)
	})

	s.Run("moves an unbreached asset to the unclipped side", func() {
		s.SetupTest()
		s.fundReserve(5_000_000)
		s.attest(usdExternal, 4_000_000)
		s.attest(xdrExternal, 1_000_000)
		policy()
		rates()

		// Assuming both breach would put the smaller holding under its own
		// ceiling, so it solves unclipped at raw value and only the larger is
		// throttled: T = (5_000_000 + 1_000_000) ÷ (1 − 0.3) = 8_571_428.
		s.Require().Equal(math.NewInt(8_571_428), s.recognised())
		response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(2_571_428), row(response, usdExternal).Credit)
		s.Require().Equal(noahCoin(1_000_000), row(response, xdrExternal).Credit)
		s.Require().Equal(noahCoin(1_714_285), row(response, xdrExternal).EffectiveCap)
	})
}

// TestRecognitionCouplesCeilingsConservatively checks that loss of one feed reduces total
// recognition and can tighten other assets' share ceilings even when their feeds remain healthy.
func (s *KeeperTestSuite) TestRecognitionCouplesCeilingsConservatively() {
	s.SetupTest()
	s.fundReserve(1_000)
	s.attest(usdExternal, 10_000)
	s.attest(xdrExternal, 10_000)
	s.setPolicy(
		eligibility(usdExternal, "1", "0.3"),
		eligibility(xdrExternal, "1", "0.2"),
	)
	rates := oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom: math.LegacyOneDec(),
	}
	s.stubRates(rates)

	// Both clipped: T = 1_000 ÷ (1 − 0.5) = 2_000, credits 600 and 400.
	s.Require().Equal(math.NewInt(2_000), s.recognised())

	// The larger share's feed goes dark: its credit vanishes, the certificate
	// shrinks to 1_000 ÷ (1 − 0.2) = 1_250, and the survivor's ceiling falls
	// from 400 to 250 with its own feed untouched.
	delete(rates, chain.USDBaseDenom)
	s.Require().Equal(math.NewInt(1_250), s.recognised())
}

// TestRecognitionLeversOnlyTheProvableBase pins that asset credit levers the
// bank-verifiable NOAH balance and nothing else: no corrupted rate or
// attestation can push the certificate past honest ÷ (1 − ratio), and a fund
// with no provable base counts no asset at all.
func (s *KeeperTestSuite) TestRecognitionLeversOnlyTheProvableBase() {
	s.Run("an attested absurdity earns the share, never more", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.attest(testAsset, 1_000_000_000_000_000)
		s.setPolicy(eligibility(testAsset, "1", "0.5"))
		s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})

		s.Require().Equal(math.NewInt(2_000), s.recognised())
	})

	s.Run("no provable base, no asset credit", func() {
		s.SetupTest()
		s.attest(testAsset, 500)
		s.setPolicy(eligibility(testAsset, "1", "0.5"))
		s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})

		s.Require().Equal(math.ZeroInt(), s.recognised())

		entry, err := s.keeper.RecognitionPolicy.Get(s.ctx, testAsset)
		s.Require().NoError(err)
		s.Require().True(entry.GrantsCredit())
	})
}

// TestRecognitionSurvivesTheLargestAdmissibleAttestation exercises maximum accepted quantities and
// rates in the EndBlock recognition path, where arithmetic failure would halt the block.
func (s *KeeperTestSuite) TestRecognitionSurvivesTheLargestAdmissibleAttestation() {
	s.SetupTest()
	// Sum two maximum attestations and value them at MaxExchangeRate. NOAH valuation multiplies;
	// the quantity and rate caps leave headroom under LegacyDec's whole-number bound.
	s.stubRates(oracletypes.RateSet{testFeed: oracletypes.MaxExchangeRate})
	s.setPolicy(eligibility(testAsset, "1", "0.5"))
	s.attestQuantity(testAsset, types.MaxAttestedQuantity)
	s.attestQuantity(testAsset, types.MaxAttestedQuantity)
	s.fundReserve(1_000)

	// The credit still levers only the provable base, so the answer is the
	// same bound an ordinary absurdity earns: 1_000 ÷ (1 − 0.5).
	s.Require().Equal(math.NewInt(2_000), s.recognised())
}

func (s *KeeperTestSuite) TestRecognitionExcludesImpairedAndClosedPositions() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(1_000)
	first := s.deploy(committee, destination, 300, 30, 0)
	second := s.deploy(committee, destination, 200, 20, 0)
	s.setPolicy(eligibility(testAsset, "1", "0.5"))
	s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})

	s.Require().Equal(math.NewInt(1_050), s.recognised())

	// Impairment zeroes the position's credit without touching its quantity.
	_, err := s.msgServer.CommitteeMarkImpaired(s.ctx, &types.MsgCommitteeMarkImpaired{
		Committee:    committee,
		ExpectedTerm: 1,
		PositionId:   second,
		Reference:    "statement-2026-08",
	})
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_030), s.recognised())
	position, err := s.keeper.OpenPositions.Get(s.ctx, second)
	s.Require().NoError(err)
	s.Require().Equal(assetCoin(20), position.Quantity)

	// Closure removes the position from the fold entirely.
	_, err = s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{
		Committee:    committee,
		ExpectedTerm: 1,
		PositionId:   first,
	})
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000), s.recognised())

	// The impaired quantity stays visible in the decomposition: an exclusion
	// that vanished would hide the impairment it exists to surface.
	response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
	s.Require().NoError(err)
	s.Require().Len(response.Assets, 1)
	s.Require().Equal(math.ZeroInt(), response.Assets[0].AttestedQuantity)
	s.Require().Equal(math.NewInt(20), response.Assets[0].ImpairedQuantity)
	s.Require().Equal(noahCoin(0), response.Assets[0].Credit)
}

func (s *KeeperTestSuite) TestRecognitionTruncatesFractionalCredit() {
	s.SetupTest()
	s.fundReserve(1_000)
	s.attest(xdrExternal, 10)
	s.setPolicy(eligibility(xdrExternal, "0.333333333333333333", "0.9"))
	s.stubRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})

	// 10 × 0.333... = 3.33... truncates to 3: recognition rounds against
	// itself.
	s.Require().Equal(math.NewInt(1_003), s.recognised())
}

// TestRecognisedCapitalDecomposesAttestedCustodyOnly checks unlisted attestations appear with zero
// credit, while Bank-held registry paper produces no row. Neither path queries Oracle.
func (s *KeeperTestSuite) TestRecognisedCapitalDecomposesAttestedCustodyOnly() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(1_000)
	s.registerAsset(chain.USDBaseDenom)
	s.fundReserveAsset(chain.USDBaseDenom, 25)
	s.deploy(committee, destination, 700, 7, 0)

	s.Require().Equal(math.NewInt(1_000), s.recognised())

	response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
	s.Require().NoError(err)
	s.Require().Equal(noahCoin(1_000), response.Recognised)
	// One row, not two: the twenty-five paper in the account is absent entirely,
	// not present at zero.
	s.Require().Equal([]types.AssetRecognition{
		{
			Denom:               testAsset,
			Eligible:            false,
			AttestedQuantity:    math.NewInt(7),
			ImpairedQuantity:    math.ZeroInt(),
			Rate:                math.LegacyZeroDec(),
			HaircutFactor:       math.LegacyZeroDec(),
			RecognitionCapRatio: math.LegacyZeroDec(),
			EffectiveCap:        noahCoin(0),
			Credit:              noahCoin(0),
		},
	}, response.Assets)
}

func (s *KeeperTestSuite) TestRecognitionPolicyQueryReturnsStoredEntries() {
	s.SetupTest()
	response, err := s.queryServer.RecognitionPolicy(s.ctx, &types.QueryRecognitionPolicyRequest{})
	s.Require().NoError(err)
	s.Require().Empty(response.Entries)

	s.setPolicy(
		eligibility(xdrExternal, "1", "0.1"),
		eligibility(testAsset, "0.5", "0.2"),
	)
	response, err = s.queryServer.RecognitionPolicy(s.ctx, &types.QueryRecognitionPolicyRequest{})
	s.Require().NoError(err)
	s.Require().Equal([]types.EligibilityEntry{
		eligibility(testAsset, "0.5", "0.2"),
		eligibility(xdrExternal, "1", "0.1"),
	}, response.Entries)
}

func (s *KeeperTestSuite) TestGenesisRecognitionPolicyValidation() {
	valid := []types.EligibilityEntry{
		eligibility(testAsset, "0.5", "0.2"),
		eligibility(xdrExternal, "1", "0.1"),
	}

	tests := []struct {
		name   string
		mutate func(gs *types.GenesisState)
		want   string
	}{
		{
			name:   "valid sorted policy",
			mutate: func(gs *types.GenesisState) { gs.RecognitionPolicy = valid },
		},
		{
			name: "unsorted policy",
			mutate: func(gs *types.GenesisState) {
				gs.RecognitionPolicy = []types.EligibilityEntry{valid[1], valid[0]}
			},
			want: "sorted by asset denomination",
		},
		{
			name: "duplicate entry",
			mutate: func(gs *types.GenesisState) {
				gs.RecognitionPolicy = []types.EligibilityEntry{valid[0], valid[0]}
			},
			want: "duplicate eligibility entry",
		},
		{
			name: "invalid entry",
			mutate: func(gs *types.GenesisState) {
				gs.RecognitionPolicy = []types.EligibilityEntry{eligibility(testAsset, "1.5", "0.1")}
			},
			want: "haircut factor must be greater than zero and at most one",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			gs := types.DefaultGenesisState()
			tc.mutate(gs)
			err := gs.Validate()
			if tc.want == "" {
				s.Require().NoError(err)
				return
			}
			s.Require().ErrorContains(err, tc.want)
		})
	}
}
