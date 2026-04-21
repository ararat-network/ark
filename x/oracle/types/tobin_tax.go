package types

import "strings"

// TobinTaxes is a list of TobinTax
type TobinTaxes []TobinTax

// String implements fmt.Stringer interface
func (dl TobinTaxes) String() (out string) {
	for _, d := range dl {
		out += d.String() + "\n"
	}
	return strings.TrimSpace(out)
}
