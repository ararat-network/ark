package app

import abci "github.com/cometbft/cometbft/abci/types"

// ABCI entry points delegate to the mempool handler, which owns admission
// and its fencing against the embedded application's lifecycle.

func (app *ArkApp) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	return app.mempoolHandler.CheckTx(req)
}

func (app *ArkApp) InsertTx(req *abci.RequestInsertTx) (*abci.ResponseInsertTx, error) {
	return app.mempoolHandler.InsertTx(req)
}

func (app *ArkApp) ReapTxs(req *abci.RequestReapTxs) (*abci.ResponseReapTxs, error) {
	return app.mempoolHandler.ReapTxs(req)
}

func (app *ArkApp) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return app.mempoolHandler.InitChain(req)
}

func (app *ArkApp) FinalizeBlock(req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return app.mempoolHandler.FinalizeBlock(req)
}

func (app *ArkApp) PrepareProposal(req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	return app.mempoolHandler.PrepareProposal(req)
}

func (app *ArkApp) ProcessProposal(req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	return app.mempoolHandler.ProcessProposal(req)
}

func (app *ArkApp) ApplySnapshotChunk(req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	return app.mempoolHandler.ApplySnapshotChunk(req)
}

func (app *ArkApp) Commit() (*abci.ResponseCommit, error) {
	return app.mempoolHandler.Commit()
}
