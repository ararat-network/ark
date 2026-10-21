package keeper_test

import (
	"context"

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
const testRateAge = uint64(3_600)

// testAttestationAge is the attestation window test entries state: a quarter,
// the cadence a custodian statement typically arrives on.
const testAttestationAge = uint64(90 * 86_400)

// xdrExternal and usdExternal identify external claims; their prefixes identify the Oracle series.
// Reserve lists and holds claims, while Oracle prices series.
const (
	xdrExternal = chain.XDRBaseDenom + "-x"
	usdExternal = chain.USDBaseDenom + "-x"
)

// eligibility builds one policy entry. Both factors are ratios: the haircut
// scales the holding's own value, max credit per NOAH caps the credit as a
// multiple of the Reserve's NOAH balance.
func eligibility(denom, haircut, perNoah string) types.EligibilityEntry {
	return agedEligibility(denom, haircut, perNoah, testRateAge)
}

func agedEligibility(denom, haircut, perNoah string, maxRateAge uint64) types.EligibilityEntry {
	return types.EligibilityEntry{
		Denom:             denom,
		HaircutFactor:     math.LegacyMustNewDecFromStr(haircut),
		MaxCreditPerNoah:  math.LegacyMustNewDecFromStr(perNoah),
		MaxRateAge:        maxRateAge,
		MaxAttestationAge: testAttestationAge,
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

func (s *KeeperTestSuite) stubRatesAtAge(rates oracletypes.RateSet, age uint64) {
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
				if request.MaxAge == 0 || age > request.MaxAge {
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

	s.Run("rejects max credit per NOAH summing above one", func() {
		s.SetupTest()
		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries: []types.EligibilityEntry{
				eligibility(xdrExternal, "1", "0.6"),
				eligibility(testAsset, "1", "0.5"),
			},
		})
		s.Require().ErrorContains(err, "must sum to at most one")
		s.Require().Empty(s.policyDenoms())
	})

	// At one, external credit may equal the NOAH balance: recognised capital
	// doubles and goes no further.
	s.Run("admits max credit per NOAH summing to one", func() {
		s.SetupTest()
		_, err := s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries: []types.EligibilityEntry{
				eligibility(xdrExternal, "1", "0.6"),
				eligibility(testAsset, "1", "0.4"),
			},
		})
		s.Require().NoError(err)

		_, err = s.msgServer.SetRecognitionPolicy(s.ctx, &types.MsgSetRecognitionPolicy{
			Authority: s.authority,
			Entries:   []types.EligibilityEntry{eligibility(testAsset, "1", "1")},
		})
		s.Require().NoError(err)
	})

	s.Run("rejects invalid entries", func() {
		// Unset, negative, and zero are one rule and one message on each factor:
		// an entry exists to grant credit, so nothing short of positive is an
		// entry at all.
		const (
			wantHaircut = "haircut factor must be greater than zero and at most one"
			wantPerNoah = "max credit per NOAH must be greater than zero and at most one"
		)
		tests := []struct {
			name  string
			entry types.EligibilityEntry
			want  string
		}{
			{
				name: "unset haircut",
				entry: types.EligibilityEntry{
					Denom:            testAsset,
					MaxCreditPerNoah: math.LegacyMustNewDecFromStr("0.1"),
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
				name: "unset max credit per NOAH",
				entry: types.EligibilityEntry{
					Denom:         testAsset,
					HaircutFactor: math.LegacyOneDec(),
				},
				want: wantPerNoah,
			},
			{
				name:  "negative max credit per NOAH",
				entry: eligibility(testAsset, "1", "-0.000000000000000001"),
				want:  wantPerNoah,
			},
			{
				name:  "zero max credit per NOAH",
				entry: eligibility(testAsset, "1", "0"),
				want:  wantPerNoah,
			},
			{
				name:  "max credit per NOAH above one",
				entry: eligibility(testAsset, "1", "1.000000000000000001"),
				want:  wantPerNoah,
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
		Denom:            testAsset,
		Eligible:         true,
		AttestedQuantity: math.NewInt(80),
		ImpairedQuantity: math.ZeroInt(),
		StaleQuantity:    math.ZeroInt(),
		Rate:             math.LegacyNewDecWithPrec(5, 1),
		HaircutFactor:    math.LegacyMustNewDecFromStr("0.5"),
		MaxCreditPerNoah: math.LegacyMustNewDecFromStr("0.1"),
		// A tenth of the 1_000 NOAH balance: the ceiling the credit was
		// measured against, reported even when it does not bind.
		Cap:               noahCoin(100),
		Credit:            noahCoin(20),
		MaxRateAge:        testRateAge,
		MaxAttestationAge: testAttestationAge,
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
		agedEligibility(chain.XDRBaseDenom+"-patient", "1", "0.4", 26*3_600),
		agedEligibility(chain.XDRBaseDenom+"-strict", "1", "0.4", 3_600),
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
		agedEligibility(chain.XDRBaseDenom+"-patient", "1", "0.4", 26*3_600),
		agedEligibility(chain.XDRBaseDenom+"-strict", "1", "0.4", 3_600),
	)
	s.stubRatesAtAge(rates, 2*3_600)
	s.Require().Equal(math.NewInt(1_000+200+0), s.recognised())

	// Each row reports the window it was judged under, so the refused row's
	// zero rate reads against its own hour rather than its neighbour's day.
	response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
	s.Require().NoError(err)
	s.Require().Len(response.Assets, 2)
	patient, strict := response.Assets[0], response.Assets[1]
	s.Require().Equal(chain.XDRBaseDenom+"-patient", patient.Denom)
	s.Require().Equal(uint64(26*3_600), patient.MaxRateAge)
	s.Require().True(patient.Rate.IsPositive())
	s.Require().Equal(chain.XDRBaseDenom+"-strict", strict.Denom)
	s.Require().Equal(uint64(3_600), strict.MaxRateAge)
	s.Require().True(strict.Rate.IsZero())
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

	// The cap clips the joint holding: 120 + 80 attested = 200 raw, but a
	// tenth of the 900 NOAH balance is 90, so the asset carries exactly that.
	s.setPolicy(eligibility(testAsset, "1", "0.1"))
	s.Require().Equal(math.NewInt(990), s.recognised())

	// Delisting removes the credit whole: there is no custody-only entry to
	// step down to — a zero factor is refused at the policy write — so listed
	// and credited are the same state, and unlisted is the other.
	s.setPolicy()
	s.Require().Equal(math.NewInt(900), s.recognised())
}

// TestRecognitionCapsEachAssetIndependently pins the property the cap is
// measured against the NOAH balance for: a misreported holding earns its own
// cap and moves no other asset's credit. Capped against recognised capital
// instead, the fake entry below would raise the honest asset's ceiling with
// the total and carry 200 to 1,000.
func (s *KeeperTestSuite) TestRecognitionCapsEachAssetIndependently() {
	row := func(denom string) types.AssetRecognition {
		response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
		s.Require().NoError(err)
		for _, asset := range response.Assets {
			if asset.Denom == denom {
				return asset
			}
		}
		s.T().Fatalf("no recognition row for %s", denom)
		return types.AssetRecognition{}
	}

	s.SetupTest()
	s.fundReserve(100)
	s.attest(usdExternal, 1_000)
	s.stubRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom: math.LegacyOneDec(),
	})
	s.setPolicy(eligibility(usdExternal, "1", "0.5"), eligibility(xdrExternal, "1", "0.4"))

	// Half the 100 NOAH balance caps the honest holding at 50.
	s.Require().Equal(math.NewInt(150), s.recognised())

	// A holding that does not exist is attested: it earns its own cap, 40,
	// and the honest asset's ceiling and credit stay where they were.
	s.attest(xdrExternal, 1_000_000_000)
	s.Require().Equal(math.NewInt(190), s.recognised())
	s.Require().Equal(noahCoin(50), row(usdExternal).Credit)
	s.Require().Equal(noahCoin(50), row(usdExternal).Cap)
	s.Require().Equal(noahCoin(40), row(xdrExternal).Credit)
}

// TestRecognitionLapsesUnattestedHoldings pins that a holding not re-attested
// within its entry's window earns nothing until the committee states it again,
// and that only a statement of the whole holding restarts the window.
func (s *KeeperTestSuite) TestRecognitionLapsesUnattestedHoldings() {
	committee := testAddress(1)
	destination := testAddress(2)
	const window = uint64(600)
	row := func() types.AssetRecognition {
		response, err := s.queryServer.RecognisedCapital(s.ctx, &types.QueryRecognisedCapitalRequest{})
		s.Require().NoError(err)
		for _, asset := range response.Assets {
			if asset.Denom == testAsset {
				return asset
			}
		}
		s.T().Fatalf("no recognition row for %s", testAsset)
		return types.AssetRecognition{}
	}
	// The stubbed balance is 10,000 NOAH whatever is deployed; 100 attested
	// units worth one NOAH each earn the full 100, well under half of it.
	setup := func() uint64 {
		s.SetupTest()
		s.appointCommittee(committee, 10_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(10_000)
		positionID := s.deploy(committee, destination, 400, 100, 0)
		entry := eligibility(testAsset, "1", "0.5")
		entry.MaxAttestationAge = window
		s.setPolicy(entry)
		s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})
		return positionID
	}
	attested := blockTime(20)

	s.Run("credit lapses past the window and returns on re-attestation", func() {
		positionID := setup()
		s.Require().Equal(math.NewInt(10_100), s.recognised())

		// The window is inclusive: at exactly its age the holding still counts.
		s.atClock(140, attested+window)
		s.Require().Equal(math.NewInt(10_100), s.recognised())

		s.atClock(141, attested+window+1)
		s.Require().Equal(math.NewInt(10_000), s.recognised())
		s.Require().Equal(math.ZeroInt(), row().AttestedQuantity)
		s.Require().Equal(math.NewInt(100), row().StaleQuantity)
		s.Require().Equal(noahCoin(0), row().Credit)
		s.Require().Equal(window, row().MaxAttestationAge)

		// Restating the same quantity is a re-attestation, and it must cite its
		// evidence.
		_, err := s.msgServer.CommitteeRecordUpdate(s.ctx, &types.MsgCommitteeRecordUpdate{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Quantity: assetCoin(100),
		})
		s.Require().ErrorContains(err, "entry reference must not be empty")
		_, err = s.msgServer.CommitteeRecordUpdate(s.ctx, &types.MsgCommitteeRecordUpdate{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Quantity:  assetCoin(100),
			Reference: "statement-2026-10",
		})
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(10_100), s.recognised())
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(attested+window+1, position.AttestedTime)
	})

	// Funding states only what was just acquired, not the holding already
	// there, so it leaves the window where it was.
	s.Run("funding an existing position does not re-attest it", func() {
		positionID := setup()
		s.atClock(141, attested+window+1)
		s.deploy(committee, destination, 100, 50, positionID)

		s.Require().Equal(math.NewInt(10_000), s.recognised())
		s.Require().Equal(math.NewInt(150), row().StaleQuantity)
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(attested, position.AttestedTime)
	})

	// A correction restates a recorded entry, not the custodian's current
	// holding, so it leaves the window where it was too.
	s.Run("a correction does not re-attest", func() {
		positionID := setup()
		entries := s.ledgerFor(positionID)
		s.Require().Len(entries, 1)
		s.atClock(141, attested+window+1)
		_, err := s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Corrects:  entries[0].EntryId,
			Quantity:  assetCoin(90),
			Reference: "decimal-error",
		})
		s.Require().NoError(err)

		s.Require().Equal(math.NewInt(10_000), s.recognised())
		s.Require().Equal(math.NewInt(90), row().StaleQuantity)
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(attested, position.AttestedTime)
	})

	// A return attribution states the remaining quantity, which is the whole
	// holding, so it restarts the window.
	s.Run("a return attribution re-attests the remainder", func() {
		positionID := setup()
		s.atClock(141, attested+window+1)
		_, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			ReturnedCoin:      noahCoin(250),
			RemainingQuantity: assetCoin(60),
			Reference:         "tx-0x02",
		})
		s.Require().NoError(err)

		s.Require().Equal(math.NewInt(10_060), s.recognised())
		s.Require().Equal(math.NewInt(60), row().AttestedQuantity)
		s.Require().Equal(math.ZeroInt(), row().StaleQuantity)
	})

	// Only an edited genesis could carry a time ahead of the block; it earns
	// nothing rather than a window that starts in the future.
	s.Run("an attestation dated ahead of the block earns nothing", func() {
		positionID := setup()
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		position.AttestedTime = attested + 1
		s.Require().NoError(s.keeper.OpenPositions.Set(s.ctx, positionID, position))

		s.Require().Equal(math.NewInt(10_000), s.recognised())
		s.Require().Equal(math.NewInt(100), row().StaleQuantity)
	})
}

// TestRecognitionDarkFeedZeroesOnlyItsOwnAsset checks that losing one feed
// removes that asset's credit and leaves every other asset's untouched.
func (s *KeeperTestSuite) TestRecognitionDarkFeedZeroesOnlyItsOwnAsset() {
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

	// Both clipped at their ratios of the balance: 300 and 200.
	s.Require().Equal(math.NewInt(1_500), s.recognised())

	// The larger asset's feed goes dark: its credit vanishes and the
	// survivor keeps its 200.
	delete(rates, chain.USDBaseDenom)
	s.Require().Equal(math.NewInt(1_200), s.recognised())
}

// TestRecognitionLeversOnlyTheProvableBase pins that asset credit is capped
// against the bank-verifiable NOAH balance and nothing else: no corrupted rate
// or attestation earns more than its ratio of that balance, and a fund with no
// provable base counts no asset at all.
func (s *KeeperTestSuite) TestRecognitionLeversOnlyTheProvableBase() {
	s.Run("an attested absurdity earns the cap, never more", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.attest(testAsset, 1_000_000_000_000_000)
		s.setPolicy(eligibility(testAsset, "1", "0.5"))
		s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})

		s.Require().Equal(math.NewInt(1_500), s.recognised())
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

	// The credit is still capped by the provable base, so the answer is the
	// same bound an ordinary absurdity earns: 1_000 + 0.5 × 1_000.
	s.Require().Equal(math.NewInt(1_500), s.recognised())
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
			Denom:            testAsset,
			Eligible:         false,
			AttestedQuantity: math.NewInt(7),
			ImpairedQuantity: math.ZeroInt(),
			StaleQuantity:    math.ZeroInt(),
			Rate:             math.LegacyZeroDec(),
			HaircutFactor:    math.LegacyZeroDec(),
			MaxCreditPerNoah: math.LegacyZeroDec(),
			Cap:              noahCoin(0),
			Credit:           noahCoin(0),
			MaxRateAge:       0,
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

// TestCreditToTarget pins the expansion credit to the balance that reaches the
// target: the mirror of the burn bound, searched on recognition at the
// post-credit balance because a clipped stake's credit rises with it.
func (s *KeeperTestSuite) TestCreditToTarget() {
	credit := func(target int64) math.Int {
		found, err := s.keeper.CreditToTarget(s.ctx, math.NewInt(target))
		s.Require().NoError(err)
		return found
	}

	s.Run("NOAH-only custody asks for its nominal shortfall", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.Require().Equal(math.NewInt(200), credit(1_200))
	})

	s.Run("nothing at or above the target", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.Require().True(credit(1_000).IsZero())
		s.Require().True(credit(800).IsZero())
	})

	// With 800 attested against a 0.3 cap, 1,000 NOAH recognises 1,300 and
	// every unit credited counts 1.3: 231 reaches 1,600 (1,231 + 369), where
	// the nominal shortfall of 300 would land the fund at 1,690.
	s.Run("a clipped stake lowers the credit", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.attest(testAsset, 800)
		s.setPolicy(eligibility(testAsset, "1", "0.3"))
		s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})
		s.Require().Equal(math.NewInt(231), credit(1_600))
	})

	// The cap releases once the balance passes the holding's value over its
	// ratio, so the search crosses from the levered slope to one for one:
	// 2,800 NOAH recognises 2,800 + 800, which no smaller balance does.
	s.Run("the search crosses the cap's release", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.attest(testAsset, 800)
		s.setPolicy(eligibility(testAsset, "1", "0.3"))
		s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})
		s.Require().Equal(math.NewInt(1_800), credit(3_600))
	})

	s.Run("a dark feed earns no lever", func() {
		s.SetupTest()
		s.fundReserve(1_000)
		s.attest(testAsset, 800)
		s.setPolicy(eligibility(testAsset, "1", "0.3"))
		s.stubRates(oracletypes.RateSet{})
		s.Require().Equal(math.NewInt(600), credit(1_600))
	})
}
