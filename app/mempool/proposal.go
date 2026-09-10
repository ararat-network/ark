package mempool

import (
	abci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ProposalVerifier is the SDK's proposal validation entry point.
type ProposalVerifier interface {
	TxEncode(sdk.Tx) ([]byte, error)
	PrepareProposalVerifyTx(sdk.Tx) ([]byte, error)
}

// PrepareProposalHandler adds only lane allocation to SDK transaction ordering
// and verification. The oracle wrapper has already reserved its injected bytes.
func (p *Pool) PrepareProposalHandler(app ProposalVerifier) sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
		if req.MaxTxBytes <= 0 {
			return &abci.ResponsePrepareProposal{}, nil
		}
		var gas uint64
		if block := ctx.ConsensusParams().Block; block != nil && block.MaxGas > 0 {
			gas = uint64(block.MaxGas)
		}
		txs := SelectEntries(p.Snapshot(), uint64(req.MaxTxBytes), gas, app.TxEncode, func(e Entry, _ int8) bool {
			// The selector defers verification for candidates that do not fit a
			// preferential phase. Ordinary service uses the SDK verification order.
			_, err := app.PrepareProposalVerifyTx(e.Tx)
			return err == nil
		})
		return &abci.ResponsePrepareProposal{Txs: txs}, nil
	}
}
