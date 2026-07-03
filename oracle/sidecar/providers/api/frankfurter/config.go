package frankfurter

import (
	"time"

	"noah/oracle/sidecar/providers/base/api"
	"noah/oracle/sidecar/providers/types"
)

const (
	// Name is the name of the Frankfurter provider.
	Name = "frankfurter_api"

	// URL is the Frankfurter single-pair rate endpoint.
	URL = "https://api.frankfurter.dev/v2/rate/%s/%s"
)

// DefaultAPIConfig is the default configuration for the Frankfurter API.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          time.Minute,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         1,
	MaxBlockHeightAge: 0,
}
