package metrics

const notImplemented = "not_implemented"

type ReportStatus int

const (
	Absent ReportStatus = iota
	MissingPrice
	WithPrice
)

func (rs ReportStatus) String() string {
	switch rs {
	case Absent:
		return "absent"
	case MissingPrice:
		return "missing_price"
	case WithPrice:
		return "with_price"
	default:
		return notImplemented
	}
}
