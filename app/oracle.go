package app

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"ark/abci/codec"
	"ark/abci/preblock"
	"ark/abci/proposals"
	"ark/abci/voteextension"
	oracleclient "ark/oracle/client"
)

// oracleRuntime owns the node-side oracle client and every ABCI hook that
// consumes or produces oracle protocol data.
type oracleRuntime struct {
	logger  log.Logger
	enabled bool
	client  *oracleclient.Client

	cancel context.CancelFunc
	done   chan error
}

func newOracleRuntime(app *ArkApp, appOpts servertypes.AppOptions, logger log.Logger) (*oracleRuntime, error) {
	cfg, err := oracleclient.ReadConfigFromAppOpts(appOpts)
	if err != nil {
		return nil, fmt.Errorf("read oracle client config: %w", err)
	}

	client, err := oracleclient.NewClient(logger.With(log.ModuleKey, "oracle-client"), cfg)
	if err != nil {
		return nil, fmt.Errorf("create oracle client: %w", err)
	}

	voteExtensionCodec := codec.NewVoteExtensionCodec()
	defaultProposalHandler := baseapp.NewDefaultProposalHandler(app.Mempool(), app)
	proposalHandler := proposals.NewHandler(
		defaultProposalHandler.PrepareProposalHandler(),
		defaultProposalHandler.ProcessProposalHandler(),
		app.StakingKeeper,
	)
	voteExtensionHandler := voteextension.NewHandler(
		logger,
		client,
		app.OracleKeeper,
		voteExtensionCodec,
		cfg.ClientTimeout,
	)
	preBlockHandler := preblock.NewHandler(
		app.OracleKeeper,
		app.TreasuryKeeper,
		voteExtensionCodec,
	)

	app.SetExtendVoteHandler(voteExtensionHandler.ExtendVoteHandler())
	app.SetVerifyVoteExtensionHandler(voteExtensionHandler.VerifyVoteExtensionHandler())
	app.SetPrepareProposal(proposalHandler.PrepareProposalHandler())
	app.SetProcessProposal(proposalHandler.ProcessProposalHandler())
	app.SetPreBlocker(preBlockHandler.WrappedPreBlocker(app.ModuleManager))

	return &oracleRuntime{
		logger:  logger.With(log.ModuleKey, "oracle-runtime"),
		enabled: cfg.Enabled,
		client:  client,
	}, nil
}

func (r *oracleRuntime) start() {
	if !r.enabled {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan error, 1)
	go func() {
		err := r.client.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("oracle client stopped unexpectedly", "err", err)
		}
		r.done <- err
		close(r.done)
	}()
}

func (r *oracleRuntime) close() error {
	if r.cancel == nil {
		return nil
	}

	r.cancel()
	err := <-r.done
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Close stops app-owned background services before closing the embedded app.
// It is safe to call multiple times as required by servertypes.Application.
func (app *ArkApp) Close() error {
	app.closeOnce.Do(func() {
		var oracleErr error
		if app.oracleRuntime != nil {
			oracleErr = app.oracleRuntime.close()
		}
		app.closeErr = errors.Join(oracleErr, app.App.Close())
	})

	return app.closeErr
}
