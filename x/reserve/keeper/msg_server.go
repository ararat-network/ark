package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/pkg/mandate"
	claimstypes "ark/x/claims/types"
	"ark/x/reserve/types"
	treasurytypes "ark/x/treasury/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns the Reserve message server.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

// UpdateParams replaces the complete governance-owned Reserve parameter set.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update params message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting Reserve params: %w", err)
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

// SetReserveMandate appoints, replaces, or disables the Reserve committee. A
// replacement resets allowance usage and clears the destination list with it.
func (m msgServer) SetReserveMandate(ctx context.Context, msg *types.MsgSetReserveMandate) (*types.MsgSetReserveMandateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set reserve mandate message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	current, err := m.k.Mandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Reserve mandate: %w", err)
	}
	envelope, err := mandate.Next(
		current.Envelope,
		msg.Committee,
		msg.ActivationHeight,
		msg.ExpiryHeight,
	)
	if err != nil {
		return nil, err
	}
	reserveMandate := types.NewDisabledReserveMandate(envelope.Term)
	reserveMandate.Envelope = envelope
	if msg.Committee != "" {
		reserveMandate.DeploymentAllowance = msg.DeploymentAllowance
		reserveMandate.MinimumNoahBalance = msg.MinimumNoahBalance
		reserveMandate.Destinations = msg.Destinations
	}
	if err := reserveMandate.Validate(); err != nil {
		return nil, err
	}
	if reserveMandate.Committee == m.k.authority || reserveMandate.Committee == msg.Authority {
		return nil, errors.New("reserve committee must be distinct from the Reserve authority")
	}
	if err := m.k.Mandate.Set(ctx, reserveMandate); err != nil {
		return nil, fmt.Errorf("setting Reserve mandate: %w", err)
	}
	if err := m.k.AllowanceUsed.Set(ctx, math.ZeroInt()); err != nil {
		return nil, fmt.Errorf("resetting Reserve allowance usage: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventReserveMandateSet{
		Term:             reserveMandate.Term,
		Committee:        reserveMandate.Committee,
		ActivationHeight: reserveMandate.ActivationHeight,
		ExpiryHeight:     reserveMandate.ExpiryHeight,
	}); err != nil {
		return nil, fmt.Errorf("emitting Reserve mandate: %w", err)
	}
	return &types.MsgSetReserveMandateResponse{}, nil
}

// SetRecognitionPolicy replaces the complete asset eligibility set as the
// governance authority. The stored policy is cleared before the new entries are
// written, so a proposal states the full policy it wants.
func (m msgServer) SetRecognitionPolicy(ctx context.Context, msg *types.MsgSetRecognitionPolicy) (*types.MsgSetRecognitionPolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set recognition policy message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := types.ValidateRecognitionPolicy(msg.Entries); err != nil {
		return nil, err
	}
	if err := m.k.validateExternalFeeds(ctx, msg.Entries); err != nil {
		return nil, err
	}

	if err := m.k.RecognitionPolicy.Clear(ctx, nil); err != nil {
		return nil, fmt.Errorf("clearing Reserve recognition policy: %w", err)
	}
	denoms := make([]string, 0, len(msg.Entries))
	for _, entry := range msg.Entries {
		if err := m.k.RecognitionPolicy.Set(ctx, entry.Denom, entry); err != nil {
			return nil, fmt.Errorf("setting eligibility entry for %s: %w", entry.Denom, err)
		}
		denoms = append(denoms, entry.Denom)
	}
	sort.Strings(denoms)
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventRecognitionPolicySet{
		Denoms: denoms,
	}); err != nil {
		return nil, fmt.Errorf("emitting Reserve recognition policy: %w", err)
	}
	return &types.MsgSetRecognitionPolicyResponse{}, nil
}

// FundBuffer transfers Reserve NOAH to the Redemption Buffer as the governance
// authority; see transferToFund for what it reads and what it does not.
func (m msgServer) FundBuffer(ctx context.Context, msg *types.MsgFundBuffer) (*types.MsgFundBufferResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil reserve transfer message")
	}
	if err := m.k.transferToFund(
		ctx,
		msg.Authority,
		msg.Amount,
		msg.MinimumReserveBalance,
		treasurytypes.RedemptionBufferName,
	); err != nil {
		return nil, err
	}

	return &types.MsgFundBufferResponse{}, nil
}

// FundInsurance transfers Reserve NOAH to Insurance as the governance authority.
// It is FundBuffer's twin; see transferToFund for what both guarantee.
func (m msgServer) FundInsurance(ctx context.Context, msg *types.MsgFundInsurance) (*types.MsgFundInsuranceResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil reserve transfer message")
	}
	if err := m.k.transferToFund(
		ctx,
		msg.Authority,
		msg.Amount,
		msg.MinimumReserveBalance,
		claimstypes.InsuranceName,
	); err != nil {
		return nil, err
	}

	return &types.MsgFundInsuranceResponse{}, nil
}

// CommitteeDeploy pays a mandate destination and records the holding the
// committee attests it bought. Any denomination the account holds may go out,
// but only NOAH outflows consume the allowance and honour the mandate's NOAH
// floor.
func (m msgServer) CommitteeDeploy(ctx context.Context, msg *types.MsgCommitteeDeploy) (*types.MsgCommitteeDeployResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee deploy message")
	}
	reserveMandate, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	if err := msg.Amount.Validate(); err != nil {
		return nil, fmt.Errorf("invalid deployment amount: %w", err)
	}
	if !msg.Amount.Amount.IsPositive() {
		return nil, errors.New("deployment amount must be positive")
	}
	if err := msg.Acquired.Validate(); err != nil {
		return nil, fmt.Errorf("invalid acquired holding: %w", err)
	}
	if !msg.Acquired.Amount.IsPositive() {
		return nil, errors.New("acquired holding must be positive")
	}
	destination, err := chain.ParseCanonicalAccountAddress("deployment destination", msg.Destination)
	if err != nil {
		return nil, err
	}
	if !reserveMandate.AllowsDestination(msg.Destination) {
		return nil, fmt.Errorf("destination %s is not named by the Reserve mandate", msg.Destination)
	}

	positionID, entryID, err := m.k.executeDeployment(ctx, reserveMandate, msg, destination)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeDeployResponse{PositionId: positionID, EntryId: entryID}, nil
}

// CommitteeRecordUpdate restates a position's attested holding.
func (m msgServer) CommitteeRecordUpdate(ctx context.Context, msg *types.MsgCommitteeRecordUpdate) (*types.MsgCommitteeRecordUpdateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee record update message")
	}
	reserveMandate, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	entryID, err := m.k.restateQuantity(ctx, reserveMandate, msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeRecordUpdateResponse{EntryId: entryID}, nil
}

// CommitteeAttributeReturn matches an inflow already sitting in the Reserve
// account to the position it settles. It moves no coins, and computes the anoah
// valuation here rather than taking it from the message.
func (m msgServer) CommitteeAttributeReturn(ctx context.Context, msg *types.MsgCommitteeAttributeReturn) (*types.MsgCommitteeAttributeReturnResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee attribute return message")
	}
	reserveMandate, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	if err := msg.ReturnedCoin.Validate(); err != nil {
		return nil, fmt.Errorf("invalid returned coin: %w", err)
	}
	if !msg.ReturnedCoin.Amount.IsPositive() {
		return nil, errors.New("returned coin must be positive")
	}
	entryID, err := m.k.attributeReturn(ctx, reserveMandate, msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeAttributeReturnResponse{EntryId: entryID}, nil
}

// CommitteeMarkImpaired zeroes a position's recognition credit without erasing
// the claim. The committee may clear it again through CommitteeClearImpairment.
func (m msgServer) CommitteeMarkImpaired(ctx context.Context, msg *types.MsgCommitteeMarkImpaired) (*types.MsgCommitteeMarkImpairedResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee mark impaired message")
	}
	reserveMandate, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	entryID, err := m.k.setImpaired(
		ctx,
		msg.PositionId,
		true,
		msg.Reference,
		msg.Committee,
		reserveMandate.Term,
	)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeMarkImpairedResponse{EntryId: entryID}, nil
}

// MarkImpaired zeroes a position's recognition credit as the governance
// authority, recording term zero for the standing authority rather than an
// appointment.
func (m msgServer) MarkImpaired(ctx context.Context, msg *types.MsgMarkImpaired) (*types.MsgMarkImpairedResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil mark impaired message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	entryID, err := m.k.setImpaired(
		ctx,
		msg.PositionId,
		true,
		msg.Reference,
		msg.Authority,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &types.MsgMarkImpairedResponse{EntryId: entryID}, nil
}

// ClearImpairment restores a position's recognition credit as the governance
// authority, recording term zero for the standing authority rather than an
// appointment.
func (m msgServer) ClearImpairment(ctx context.Context, msg *types.MsgClearImpairment) (*types.MsgClearImpairmentResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil clear impairment message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	entryID, err := m.k.setImpaired(
		ctx,
		msg.PositionId,
		false,
		msg.Reference,
		msg.Authority,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &types.MsgClearImpairmentResponse{EntryId: entryID}, nil
}

// CommitteeClearImpairment restores a position's recognition credit as the
// committee that marked it down.
func (m msgServer) CommitteeClearImpairment(ctx context.Context, msg *types.MsgCommitteeClearImpairment) (*types.MsgCommitteeClearImpairmentResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee clear impairment message")
	}
	reserveMandate, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	entryID, err := m.k.setImpaired(
		ctx,
		msg.PositionId,
		false,
		msg.Reference,
		msg.Committee,
		reserveMandate.Term,
	)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeClearImpairmentResponse{EntryId: entryID}, nil
}

// CommitteeClosePosition closes a position, crystallising realised profit or
// loss as the difference between what came back and what went out.
func (m msgServer) CommitteeClosePosition(ctx context.Context, msg *types.MsgCommitteeClosePosition) (*types.MsgCommitteeClosePositionResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee close position message")
	}
	reserveMandate, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	entryID, err := m.k.closePosition(ctx, msg.PositionId, msg.Reference, msg.Committee, reserveMandate.Term)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeClosePositionResponse{EntryId: entryID}, nil
}

// ClosePosition closes a position as the governance authority, recording term
// zero for the standing authority rather than an appointment.
func (m msgServer) ClosePosition(ctx context.Context, msg *types.MsgClosePosition) (*types.MsgClosePositionResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil close position message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	entryID, err := m.k.closePosition(ctx, msg.PositionId, msg.Reference, msg.Authority, 0)
	if err != nil {
		return nil, err
	}
	return &types.MsgClosePositionResponse{EntryId: entryID}, nil
}

// BurnReserveAssets destroys Reserve custody as the governance authority.
// Anything the account holds may be burned, credit-bearing assets and NOAH
// alike; the only guard is the caller's own stale-state floor on the NOAH
// component.
func (m msgServer) BurnReserveAssets(ctx context.Context, msg *types.MsgBurnReserveAssets) (*types.MsgBurnReserveAssetsResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil burn reserve assets message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	// Coins.Validate accepts the empty set, which would make a burn a no-op that
	// still looked like one happened.
	if msg.Amounts.Empty() {
		return nil, errors.New("burn must name at least one coin")
	}
	if err := msg.Amounts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid burn amounts: %w", err)
	}
	if err := chain.ValidateNoahCoin("minimum reserve balance", msg.MinimumReserveBalance); err != nil {
		return nil, err
	}

	if burnedNoah := msg.Amounts.AmountOf(chain.NoahBaseDenom); burnedNoah.IsPositive() {
		balance := m.k.balance(ctx)
		remaining, err := balance.SafeSub(burnedNoah)
		if err != nil || remaining.LT(msg.MinimumReserveBalance.Amount) {
			return nil, fmt.Errorf(
				"reserve balance %s cannot burn %s while retaining %s",
				balance,
				chain.NoahCoin(burnedNoah),
				msg.MinimumReserveBalance,
			)
		}
	}
	if err := m.k.bankKeeper.BurnCoins(ctx, types.StrategicReserveName, msg.Amounts); err != nil {
		return nil, fmt.Errorf("burning reserve assets: %w", err)
	}

	// No custom event: the signed message and Bank's canonical burn event are
	// the audit trail.
	return &types.MsgBurnReserveAssetsResponse{}, nil
}

// CommitteeBurnPaper destroys Ark-issued paper held in Reserve custody.
// requirePaperBurnable refuses NOAH, non-registry denominations, and anything
// carrying recognition credit, so a committee burn only reduces the protocol's
// own liability.
func (m msgServer) CommitteeBurnPaper(ctx context.Context, msg *types.MsgCommitteeBurnPaper) (*types.MsgCommitteeBurnPaperResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee burn paper message")
	}
	if _, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm); err != nil {
		return nil, err
	}
	// As in BurnReserveAssets: Coins.Validate accepts the empty set.
	if msg.Amounts.Empty() {
		return nil, errors.New("burn must name at least one coin")
	}
	if err := msg.Amounts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid burn amounts: %w", err)
	}
	if err := m.k.requirePaperBurnable(ctx, msg.Amounts); err != nil {
		return nil, err
	}
	if err := m.k.bankKeeper.BurnCoins(ctx, types.StrategicReserveName, msg.Amounts); err != nil {
		return nil, fmt.Errorf("burning reserve paper: %w", err)
	}
	return &types.MsgCommitteeBurnPaperResponse{}, nil
}

// CommitteeBurnSurplus destroys Reserve NOAH above the fund's capital
// requirement. The keeper derives the bound rather than trusting the message,
// and refuses the burn when the requirement cannot be computed.
func (m msgServer) CommitteeBurnSurplus(ctx context.Context, msg *types.MsgCommitteeBurnSurplus) (*types.MsgCommitteeBurnSurplusResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee burn surplus message")
	}
	if _, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm); err != nil {
		return nil, err
	}
	if err := chain.ValidateNoahCoin("burn amount", msg.Amount); err != nil {
		return nil, err
	}
	if !msg.Amount.IsPositive() {
		return nil, errors.New("burn amount must be positive")
	}

	surplus, err := m.k.BurnableSurplus(ctx)
	if err != nil {
		return nil, err
	}
	if msg.Amount.Amount.GT(surplus) {
		return nil, fmt.Errorf(
			"burn %s exceeds the burnable Reserve surplus %s",
			msg.Amount,
			chain.NoahCoin(surplus),
		)
	}
	if err := m.k.bankKeeper.BurnCoins(
		ctx,
		types.StrategicReserveName,
		chain.NoahCoins(msg.Amount.Amount),
	); err != nil {
		return nil, fmt.Errorf("burning reserve surplus: %w", err)
	}

	return &types.MsgCommitteeBurnSurplusResponse{
		RemainingSurplus: chain.NoahCoin(surplus.Sub(msg.Amount.Amount)),
	}, nil
}

// CommitteeFundBuffer tops the Redemption Buffer up toward its target as the
// committee. The keeper derives the ceiling from the shortfall, so the committee
// chooses timing and not size.
func (m msgServer) CommitteeFundBuffer(ctx context.Context, msg *types.MsgCommitteeFundBuffer) (*types.MsgCommitteeFundBufferResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee transfer message")
	}
	remaining, err := m.k.committeeTransferToFund(
		ctx,
		msg.Committee,
		msg.ExpectedTerm,
		msg.Amount,
		treasurytypes.RedemptionBufferName,
		types.TreasuryCapitalReader.RedemptionBufferShortfall,
	)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeFundBufferResponse{
		RemainingShortfall: chain.NoahCoin(remaining),
	}, nil
}

// CommitteeFundInsurance tops Insurance up toward its target as the committee.
// It is CommitteeFundBuffer's twin; see committeeTransferToFund for what both
// guarantee.
func (m msgServer) CommitteeFundInsurance(ctx context.Context, msg *types.MsgCommitteeFundInsurance) (*types.MsgCommitteeFundInsuranceResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee transfer message")
	}
	remaining, err := m.k.committeeTransferToFund(
		ctx,
		msg.Committee,
		msg.ExpectedTerm,
		msg.Amount,
		claimstypes.InsuranceName,
		types.TreasuryCapitalReader.InsuranceShortfall,
	)
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeFundInsuranceResponse{
		RemainingShortfall: chain.NoahCoin(remaining),
	}, nil
}

// CorrectPosition restates a position as the governance authority. Every field
// is restatable, including the held asset. Term zero records the standing
// authority acting rather than an appointment.
func (m msgServer) CorrectPosition(ctx context.Context, msg *types.MsgCorrectPosition) (*types.MsgCorrectPositionResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil correct position message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	entryID, err := m.k.correctPosition(ctx, positionCorrection{
		PositionID:     msg.PositionId,
		Corrects:       msg.Corrects,
		Quantity:       msg.Quantity,
		VenueReference: msg.VenueReference,
		Reference:      msg.Reference,
		RecordedBy:     msg.Authority,
	})
	if err != nil {
		return nil, err
	}
	return &types.MsgCorrectPositionResponse{EntryId: entryID}, nil
}

// CommitteeCorrectPosition restates a position as the committee that recorded
// it, on the governance correction's exact terms.
func (m msgServer) CommitteeCorrectPosition(ctx context.Context, msg *types.MsgCommitteeCorrectPosition) (*types.MsgCommitteeCorrectPositionResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee correct position message")
	}
	reserveMandate, err := m.k.authoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	entryID, err := m.k.correctPosition(ctx, positionCorrection{
		PositionID:     msg.PositionId,
		Corrects:       msg.Corrects,
		Quantity:       msg.Quantity,
		VenueReference: msg.VenueReference,
		Reference:      msg.Reference,
		RecordedBy:     msg.Committee,
		Term:           reserveMandate.Term,
	})
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeCorrectPositionResponse{EntryId: entryID}, nil
}
