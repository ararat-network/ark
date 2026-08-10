package types

// DefaultParams returns the safe launch defaults for Reserve. There are none to
// set: the module's governed policy lives in the mandate and the recognition
// policy, not in params.
func DefaultParams() Params {
	return Params{}
}

// Validate performs context-free validation of Reserve parameters. Nothing to
// check while the message carries no fields.
func (p Params) Validate() error {
	return nil
}
