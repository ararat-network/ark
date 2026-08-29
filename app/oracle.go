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

// setupOracleABCI builds the app-owned price-feed client and installs the
// oracle protocol's ABCI hooks over it: the vote-extension handler polls the
// client for prices under the configured timeout. The client owns its enabled
// gate; the start command owns running it through RunPriceFeed.
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
		defaultProposalHandler.PrepareProposalHandler(),
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
