package app

import (
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"github.com/ararat-network/ark/abci/preblock"
	"github.com/ararat-network/ark/abci/proposals"
	"github.com/ararat-network/ark/abci/voteextension"
	pricefeedclient "github.com/ararat-network/ark/pricefeed/client"
)

// setupOracleABCI constructs the cached price-feed client and installs oracle hooks. The start
// command runs it through RunPriceFeed; NewClient validates options for every app-construction
// path.
func (app *ArkApp) setupOracleABCI(logger log.Logger, appOpts servertypes.AppOptions) error {
	pricefeedCfg, err := pricefeedclient.ReadConfigFromAppOpts(appOpts)
	if err != nil {
		return fmt.Errorf("read price-feed client config: %w", err)
	}

	app.priceFeedClient, err = pricefeedclient.NewClient(logger, pricefeedCfg)
	if err != nil {
		return fmt.Errorf("create price-feed client: %w", err)
	}

	defaultProposalHandler := baseapp.NewDefaultProposalHandler(app.Mempool(), app)
	proposalHandler := proposals.NewHandler(
		app.lanePool.PrepareProposalHandler(app),
		defaultProposalHandler.ProcessProposalHandler(),
		app.StakingKeeper,
	)
	voteExtensionHandler := voteextension.NewHandler(
		logger,
		app.priceFeedClient,
		app.OracleKeeper,
		pricefeedCfg.ClientTimeout,
	)
	preBlockHandler := preblock.NewHandler(app.OracleKeeper)

	app.SetExtendVoteHandler(voteExtensionHandler.ExtendVoteHandler())
	app.SetVerifyVoteExtensionHandler(voteExtensionHandler.VerifyVoteExtensionHandler())
	app.SetPrepareProposal(proposalHandler.PrepareProposalHandler())
	app.SetProcessProposal(proposalHandler.ProcessProposalHandler())
	app.SetPreBlocker(preBlockHandler.WrappedPreBlocker(app.ModuleManager))

	return nil
}
