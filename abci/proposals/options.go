package proposals

// Option is a function that enables optional configuration of the Handler.
type Option func(*Handler)

// RetainOracleDataInWrappedProposalHandler returns an Option that configures the
// Handler to pass the injected extend-commit-info to the wrapped proposal handler.
func RetainOracleDataInWrappedProposalHandler() Option {
	return func(p *Handler) {
		p.retainOracleDataInWrappedHandler = true
	}
}
