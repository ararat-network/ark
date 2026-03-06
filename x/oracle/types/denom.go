package types

import "strings"

// DenomList is a list of Denom.
type DenomList []Denom

// String implements fmt.Stringer interface
func (dl DenomList) String() (out string) {
	for _, d := range dl {
		out += d.String() + "\n"
	}
	return strings.TrimSpace(out)
}
