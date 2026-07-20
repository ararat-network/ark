package metrics

const notImplemented = "not_implemented"

type ModuleMethod int

const (
	EndBlock ModuleMethod = iota
	BeginBlock
)

func (m ModuleMethod) String() string {
	switch m {
	case BeginBlock:
		return "begin_blocker"
	case EndBlock:
		return "end_blocker"
	default:
		return notImplemented
	}
}
