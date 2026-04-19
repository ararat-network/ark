package keeper

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"noah/x/oracle/types"
)

// OrganizeBallotByDenom collects all oracle votes for the period, categorized by the votes' denom parameter.
// Inactive or jailed validators are filtered out, and abstain votes are given zero vote power.
func (k Keeper) OrganizeBallotByDenom(ctx context.Context, validatorClaimMap map[string]types.VoteScore) (map[string]types.DenomVotes, error) {
	votes := make(map[string]types.DenomVotes)

	// Organize aggregate votes
	iteratorHandler := func(voterAddr sdk.ValAddress, vote types.AggregateExchangeRateVote) (bool, error) {
		// organize ballot only for the active validators
		claim, ok := validatorClaimMap[vote.Voter]

		if ok {
			power := claim.Power
			for _, tuple := range vote.ExchangeRateTuples {
				tmpPower := power
				if !tuple.ExchangeRate.IsPositive() {
					// Make the power of abstain vote zero
					tmpPower = 0
				}

				votes[tuple.Denom] = append(votes[tuple.Denom],
					types.NewVote(
						tuple.ExchangeRate,
						tuple.Denom,
						voterAddr,
						tmpPower,
					),
				)
			}

		}

		return false, nil
	}

	if err := k.AggregateExchangeRateVote.Walk(ctx, nil, iteratorHandler); err != nil {
		return nil, fmt.Errorf("iterating: %w", err)
	}

	// sort created ballot
	for denom, ballot := range votes {
		sort.Sort(ballot)
		votes[denom] = ballot
	}

	return votes, nil
}

// ClearBallots clears all tallied prevotes and votes from the store
func (k Keeper) ClearBallots(ctx context.Context, votePeriod uint64) error {
	// Clear all aggregate prevotes
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.AggregateExchangeRatePrevote.Walk(
		ctx,
		nil,
		func(voterAddr sdk.ValAddress, aggregatePrevote types.AggregateExchangeRatePrevote) (bool, error) {
			if sdkCtx.BlockHeight() > int64(aggregatePrevote.SubmitBlock+votePeriod) {
				if err := k.AggregateExchangeRatePrevote.Remove(ctx, voterAddr); err != nil {
					return false, err
				}
			}
			return false, nil
		},
	); err != nil {
		return err
	}

	// Clear all aggregate votes
	if err := k.AggregateExchangeRateVote.Walk(
		ctx,
		nil,
		func(voterAddr sdk.ValAddress, _ types.AggregateExchangeRateVote) (bool, error) {
			if err := k.AggregateExchangeRateVote.Remove(ctx, voterAddr); err != nil {
				return false, err
			}
			return false, nil
		},
	); err != nil {
		return err
	}

	return nil
}

// ApplyWhitelist update vote target denom list and set tobin tax with params whitelist
func (k Keeper) ApplyWhitelist(ctx context.Context, whitelist types.DenomList, voteTargets map[string]math.LegacyDec) error {
	// check is there any update in whitelist params
	updateRequired := false
	if len(voteTargets) != len(whitelist) {
		updateRequired = true
	} else {
		for _, item := range whitelist {
			if tobinTax, ok := voteTargets[item.Name]; !ok || !tobinTax.Equal(item.TobinTax) {
				updateRequired = true
				break
			}
		}
	}

	if updateRequired {
		if err := k.TobinTax.Walk(
			ctx,
			nil,
			func(denom string, _ math.LegacyDec) (bool, error) {
				if err := k.TobinTax.Remove(ctx, denom); err != nil {
					return false, err
				}

				return false, nil
			},
		); err != nil {
			return err
		}

		for _, item := range whitelist {
			if err := k.TobinTax.Set(ctx, item.Name, item.TobinTax); err != nil {
				return fmt.Errorf("setting tobin tax: %w", err)
			}

			// Register meta data to bank module
			if _, ok := k.bankKeeper.GetDenomMetaData(ctx, item.Name); !ok {
				base := item.Name
				display := base[1:]

				k.bankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
					Description: "The native stable token of the Noah Icarus.",
					DenomUnits: []*banktypes.DenomUnit{
						{Denom: "u" + display, Exponent: uint32(0), Aliases: []string{"micro" + display}},
						{Denom: "m" + display, Exponent: uint32(3), Aliases: []string{"milli" + display}},
						{Denom: display, Exponent: uint32(6), Aliases: []string{}},
					},
					Base:    base,
					Display: display,
					Name:    fmt.Sprintf("%s NOAH", strings.ToUpper(display)),
					Symbol:  fmt.Sprintf("%sN", strings.ToUpper(display[:len(display)-1])),
				})
			}
		}
	}

	return nil
}
