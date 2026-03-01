package types

// Denom is a struct that represents a whitelisted oracle denomination.
type Denom struct {
	Name string
}

// DenomList is a list of Denom.
type DenomList []Denom
