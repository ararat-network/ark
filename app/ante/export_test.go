package ante

// Test-only bridges: internals the external test package exercises directly
// without widening the package's real API.
var (
	GasPriority              = gasPriority
	SimulationSigGasConsumer = simulationSigGasConsumer
	ValidateVoterStake       = validateVoterStake
	WithTransferTax          = withTransferTax
)

const (
	MaxMultiSendOutputs = maxMultiSendOutputs
	MultiSendGasFactor  = multiSendGasFactor
)
