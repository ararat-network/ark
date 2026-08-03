package types_test

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// testAddress builds a deterministic bech32 account address from one repeated
// byte. It lives here rather than beside a single test because the mandate and
// policy suites both need addresses that are valid but otherwise meaningless.
func testAddress(seed byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20)).String()
}
