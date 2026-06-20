package metrics

const notImplemented = "not_implemented"

type ModuleMethod int

const (
	EndBlock ModuleMethod = iota
)

func (m ModuleMethod) String() string {
	switch m {
	case EndBlock:
		return "end_blocker"
	default:
		return notImplemented
	}
}
