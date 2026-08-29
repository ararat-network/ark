package app

import (
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	gmptypes "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/types"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	packetforwardtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ratelimittypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"

	runtimev1alpha1 "cosmossdk.io/api/cosmos/app/runtime/v1alpha1"
	appv1alpha1 "cosmossdk.io/api/cosmos/app/v1alpha1"
	authmodulev1 "cosmossdk.io/api/cosmos/auth/module/v1"
	authzmodulev1 "cosmossdk.io/api/cosmos/authz/module/v1"
	bankmodulev1 "cosmossdk.io/api/cosmos/bank/module/v1"
	consensusmodulev1 "cosmossdk.io/api/cosmos/consensus/module/v1"
	distrmodulev1 "cosmossdk.io/api/cosmos/distribution/module/v1"
	evidencemodulev1 "cosmossdk.io/api/cosmos/evidence/module/v1"
	feegrantmodulev1 "cosmossdk.io/api/cosmos/feegrant/module/v1"
	genutilmodulev1 "cosmossdk.io/api/cosmos/genutil/module/v1"
	govmodulev1 "cosmossdk.io/api/cosmos/gov/module/v1"
	slashingmodulev1 "cosmossdk.io/api/cosmos/slashing/module/v1"
	stakingmodulev1 "cosmossdk.io/api/cosmos/staking/module/v1"
	txconfigv1 "cosmossdk.io/api/cosmos/tx/config/v1"
	upgrademodulev1 "cosmossdk.io/api/cosmos/upgrade/module/v1"
	vestingmodulev1 "cosmossdk.io/api/cosmos/vesting/module/v1"
	"cosmossdk.io/core/appconfig"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/module"
	_ "github.com/cosmos/cosmos-sdk/x/auth"           // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/auth/tx/config" // import for side-effects
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	_ "github.com/cosmos/cosmos-sdk/x/auth/vesting" // import for side-effects
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	_ "github.com/cosmos/cosmos-sdk/x/authz/module" // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/bank"         // import for side-effects
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	_ "github.com/cosmos/cosmos-sdk/x/consensus" // import for side-effects
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	_ "github.com/cosmos/cosmos-sdk/x/distribution" // import for side-effects
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	_ "github.com/cosmos/cosmos-sdk/x/evidence" // import for side-effects
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	_ "github.com/cosmos/cosmos-sdk/x/feegrant/module" // import for side-effects
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	_ "github.com/cosmos/cosmos-sdk/x/slashing" // import for side-effects
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	_ "github.com/cosmos/cosmos-sdk/x/staking" // import for side-effects
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	_ "github.com/cosmos/cosmos-sdk/x/upgrade" // import for side-effects
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	assetmodulev1 "github.com/ararat-network/ark/api/ark/asset/module/v1"
	claimsmodulev1 "github.com/ararat-network/ark/api/ark/claims/module/v1"
	marketmodulev1 "github.com/ararat-network/ark/api/ark/market/module/v1"
	oraclemodulev1 "github.com/ararat-network/ark/api/ark/oracle/module/v1"
	reservemodulev1 "github.com/ararat-network/ark/api/ark/reserve/module/v1"
	securitymodulev1 "github.com/ararat-network/ark/api/ark/security/module/v1"
	treasurymodulev1 "github.com/ararat-network/ark/api/ark/treasury/module/v1"
	"github.com/ararat-network/ark/app/params"
	_ "github.com/ararat-network/ark/x/asset/module"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	_ "github.com/ararat-network/ark/x/claims/module"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	_ "github.com/ararat-network/ark/x/market/module"
	markettypes "github.com/ararat-network/ark/x/market/types"
	_ "github.com/ararat-network/ark/x/oracle/module"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	_ "github.com/ararat-network/ark/x/reserve/module"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	_ "github.com/ararat-network/ark/x/security/module"
	securitytypes "github.com/ararat-network/ark/x/security/types"
	_ "github.com/ararat-network/ark/x/treasury/module"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

var (
	// module account permissions
	moduleAccPerms = []*authmodulev1.ModuleAccountPermission{
		{Account: authtypes.FeeCollectorName},
		{Account: distrtypes.ModuleName},
		{Account: stakingtypes.BondedPoolName, Permissions: []string{authtypes.Burner, stakingtypes.ModuleName}},
		{Account: stakingtypes.NotBondedPoolName, Permissions: []string{authtypes.Burner, stakingtypes.ModuleName}},
		{Account: govtypes.ModuleName, Permissions: []string{authtypes.Burner}},
		{Account: markettypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
		{Account: ibctransfertypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
		{Account: icatypes.ModuleName},
		// Contracts may burn their own funds; they never mint. Only Market does.
		{Account: wasmtypes.ModuleName, Permissions: []string{authtypes.Burner}},
		{Account: treasurytypes.SubsidyPoolName},
		{Account: treasurytypes.RedemptionBufferName},
		// The strategic Reserve is the one fund account that may burn.
		{Account: reservetypes.StrategicReserveName, Permissions: []string{authtypes.Burner}},
		{Account: claimstypes.InsuranceName},
		{Account: treasurytypes.StabilityTaxCollectorName},
		{Account: oracletypes.ModuleName},
	}

	// blocked account addresses
	blockAccAddrs = []string{
		authtypes.FeeCollectorName,
		distrtypes.ModuleName,
		stakingtypes.BondedPoolName,
		stakingtypes.NotBondedPoolName,
		markettypes.ModuleName,
		ibctransfertypes.ModuleName,
		icatypes.ModuleName,
		wasmtypes.ModuleName,
		treasurytypes.StabilityTaxCollectorName,
		oracletypes.ModuleName,
		// We allow the following module accounts to receive funds:
		// govtypes.ModuleName,
		// treasurytypes.SubsidyPoolName,
		// treasurytypes.RedemptionBufferName,
		// reservetypes.StrategicReserveName,
		// claimstypes.InsuranceName,
	}

	ModuleConfig = []*appv1alpha1.ModuleConfig{
		{
			Name: runtime.ModuleName,
			Config: appconfig.WrapAny(&runtimev1alpha1.Module{
				AppName: AppName,
				// NOTE: upgrade module is required to be prioritised
				PreBlockers: []string{
					upgradetypes.ModuleName,
					authtypes.ModuleName,
				},
				// During begin block slashing happens after distr.BeginBlocker so that
				// there is nothing left over in the validator fee pool, so as to keep the
				// CanWithdrawInvariant invariant.
				// NOTE: staking module is required if HistoricalEntries param > 0
				// NOTE: treasury leads because its reward-funding window must advance
				// before distribution consumes the previous block's fees. A module
				// added ahead of it must not touch that window.
				BeginBlockers: []string{
					treasurytypes.ModuleName,
					distrtypes.ModuleName,
					slashingtypes.ModuleName,
					evidencetypes.ModuleName,
					stakingtypes.ModuleName,
					ibcexported.ModuleName,
					ratelimittypes.ModuleName,
					authz.ModuleName,
				},
				// NOTE: market leads because its EndBlocker executes the block's
				// conversion settlement through Treasury before any other EndBlock
				// actor moves state. Settlement must value liability under the
				// lifecycle regime the block's swaps quoted under, and every later
				// actor — governance enacting transitions, Claims paying due
				// claims — must see settled funds, which is what per-conversion
				// settlement gave them. A module added ahead of it must move
				// neither member supply, fund balances, nor asset lifecycle state.
				EndBlockers: []string{
					markettypes.ModuleName,
					// Bank's EndBlocker only flushes virtual-account credits, which
					// need an object store key this app never sets. It is listed
					// because the module implements the hook; its slot carries no
					// ordering intent.
					banktypes.ModuleName,
					govtypes.ModuleName,
					// Oracle jails ahead of staking so an attendance jail is in
					// this block's validator-set update rather than the next
					// block's. It stays after gov so a live attendance-ratio
					// change applies at the same settlement.
					oracletypes.ModuleName,
					stakingtypes.ModuleName,
					feegrant.ModuleName,
					// Claims settles last so a governance cancellation executed
					// by the gov EndBlocker above is already recorded. The
					// ordering is a readability choice, not a correctness one:
					// cancellation is refused from the executable height on, so
					// the veto window and the sweep cannot both take a claim.
					claimstypes.ModuleName,
				},
				OverrideStoreKeys: []*runtimev1alpha1.StoreKeyConfig{
					{
						ModuleName: authtypes.ModuleName,
						KvStoreKey: "acc",
					},
				},
				// NOTE: The genutils module must occur after staking so that pools are
				// properly initialised with tokens from genesis accounts.
				// NOTE: The genutils module must also occur after auth so that it can access the params from auth.
				InitGenesis: []string{
					authtypes.ModuleName,
					banktypes.ModuleName,
					distrtypes.ModuleName,
					stakingtypes.ModuleName,
					slashingtypes.ModuleName,
					govtypes.ModuleName,
					ibcexported.ModuleName,
					genutiltypes.ModuleName,
					evidencetypes.ModuleName,
					authz.ModuleName,
					feegrant.ModuleName,
					upgradetypes.ModuleName,
					vestingtypes.ModuleName,
					ibctransfertypes.ModuleName,
					ratelimittypes.ModuleName,
					packetforwardtypes.ModuleName,
					icatypes.ModuleName,
					wasmtypes.ModuleName,
					gmptypes.ModuleName,
					ibcwasmtypes.ModuleName,
					// Oracle initialises before asset: asset genesis validates
					// every asset's feed against the registry.
					oracletypes.ModuleName,
					assettypes.ModuleName,
					markettypes.ModuleName,
					treasurytypes.ModuleName,
					claimstypes.ModuleName,
					reservetypes.ModuleName,
					securitytypes.ModuleName,
				},
				// ExportGenesis is left unset so the runtime mirrors InitGenesis.
				// Module exports run concurrently and are pure reads, so there
				// is no export order to pin.
			}),
		},
		{
			Name: authtypes.ModuleName,
			Config: appconfig.WrapAny(&authmodulev1.Module{
				Bech32Prefix:             params.Bech32PrefixAccAddr,
				ModuleAccountPermissions: moduleAccPerms,
				// By default modules authority is the governance module. This is configurable with the following:
				// Authority: "group", // A custom module authority can be set using a module name
				// Authority: "ark10d07y265gmmuvt4z0w9aw880jnsr700j2cu5hn", // or a specific address
				EnableUnorderedTransactions: true,
			}),
		},
		{
			Name:   vestingtypes.ModuleName,
			Config: appconfig.WrapAny(&vestingmodulev1.Module{}),
		},
		{
			Name: banktypes.ModuleName,
			Config: appconfig.WrapAny(&bankmodulev1.Module{
				BlockedModuleAccountsOverride: blockAccAddrs,
				RestrictionsOrder: []string{
					treasurytypes.ModuleName,
					claimstypes.ModuleName,
					reservetypes.ModuleName,
				},
			}),
		},
		{
			Name:   stakingtypes.ModuleName,
			Config: appconfig.WrapAny(&stakingmodulev1.Module{}),
		},
		{
			Name:   slashingtypes.ModuleName,
			Config: appconfig.WrapAny(&slashingmodulev1.Module{}),
		},
		{
			Name: "tx",
			Config: appconfig.WrapAny(&txconfigv1.Config{
				SkipAnteHandler: true, // Enable this to skip the default antehandlers and set custom ante handlers.
			}),
		},
		{
			Name:   genutiltypes.ModuleName,
			Config: appconfig.WrapAny(&genutilmodulev1.Module{}),
		},
		{
			Name:   authz.ModuleName,
			Config: appconfig.WrapAny(&authzmodulev1.Module{}),
		},
		{
			Name:   upgradetypes.ModuleName,
			Config: appconfig.WrapAny(&upgrademodulev1.Module{}),
		},
		{
			Name:   distrtypes.ModuleName,
			Config: appconfig.WrapAny(&distrmodulev1.Module{}),
		},
		{
			Name:   govtypes.ModuleName,
			Config: appconfig.WrapAny(&govmodulev1.Module{}),
		},
		{
			Name:   evidencetypes.ModuleName,
			Config: appconfig.WrapAny(&evidencemodulev1.Module{}),
		},
		{
			Name:   feegrant.ModuleName,
			Config: appconfig.WrapAny(&feegrantmodulev1.Module{}),
		},
		{
			Name:   consensustypes.ModuleName,
			Config: appconfig.WrapAny(&consensusmodulev1.Module{}),
		},
		{
			Name:   markettypes.ModuleName,
			Config: appconfig.WrapAny(&marketmodulev1.Module{}),
		},
		{
			Name:   treasurytypes.ModuleName,
			Config: appconfig.WrapAny(&treasurymodulev1.Module{}),
		},
		{
			Name:   claimstypes.ModuleName,
			Config: appconfig.WrapAny(&claimsmodulev1.Module{}),
		},
		{
			Name:   reservetypes.ModuleName,
			Config: appconfig.WrapAny(&reservemodulev1.Module{}),
		},
		{
			Name:   oracletypes.ModuleName,
			Config: appconfig.WrapAny(&oraclemodulev1.Module{}),
		},
		{
			Name:   assettypes.ModuleName,
			Config: appconfig.WrapAny(&assetmodulev1.Module{}),
		},
		{
			Name:   securitytypes.ModuleName,
			Config: appconfig.WrapAny(&securitymodulev1.Module{}),
		},
	}

	// AppConfig is application configuration (used by depinject)
	AppConfig = depinject.Configs(appconfig.Compose(&appv1alpha1.Config{
		Modules: ModuleConfig,
	}),
		// x/claims and x/reserve both satisfy the recognised-capital method set
		// Treasury expects, so depinject cannot resolve its two inputs by
		// scanning provided types. These bindings are what disambiguate them;
		// the named interfaces cannot do it alone, because Go interfaces are
		// structural.
		//
		// They belong here rather than beside NewArkApp's own Configs call:
		// anything that injects from AppConfig hits the same ambiguity, so the
		// resolution has to travel with the module set.
		//
		// depinject names a type as "<pkgPath>/<Go type string>", which is why
		// the interface carries its package twice and the pointer keeps its
		// star inside the name.
		depinject.BindInterface(
			"github.com/ararat-network/ark/x/treasury/types/types.ClaimsKeeper",
			"github.com/ararat-network/ark/x/claims/keeper/*keeper.Keeper",
		),
		depinject.BindInterface(
			"github.com/ararat-network/ark/x/treasury/types/types.ReserveKeeper",
			"github.com/ararat-network/ark/x/reserve/keeper/*keeper.Keeper",
		),
		depinject.Supply(map[string]module.AppModuleBasic{
			genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
			govtypes.ModuleName: gov.NewAppModuleBasic(
				[]govclient.ProposalHandler{},
			),
		}),
	)
)

// GetMaccPerms returns a copy of the module account permissions
func GetMaccPerms() map[string][]string {
	dup := make(map[string][]string)
	for _, perms := range moduleAccPerms {
		dup[perms.Account] = perms.Permissions
	}

	return dup
}

// BlockedAddresses returns all the app's blocked account addresses.
func BlockedAddresses() map[string]bool {
	result := make(map[string]bool)

	if len(blockAccAddrs) > 0 {
		for _, addr := range blockAccAddrs {
			result[addr] = true
		}
	} else {
		for addr := range GetMaccPerms() {
			result[addr] = true
		}
	}

	return result
}
