package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"go.yaml.in/yaml/v3"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdktelemetry "github.com/cosmos/cosmos-sdk/telemetry"
)

// otelConfigFileEnv is the SDK's switch for initialising OpenTelemetry from
// a file at package load rather than from config/otel.yaml at start. The
// SDK keeps the name unexported.
const otelConfigFileEnv = "OTEL_EXPERIMENTAL_CONFIG_FILE"

// otelFile is what start needs from config/otel.yaml before the SDK builds
// OpenTelemetry from it: the claims that touch the node's own metrics. The
// SDK parses the file itself a moment later; this only reads its shape.
type otelFile struct {
	// meterProvider is a meter_provider block. While [prometheus] serves,
	// the endpoint's provider is installed before and after the SDK's own,
	// so the node's meters bind to it and the block's readers see none.
	meterProvider bool
	// pullReader is a pull reader under meter_provider. The otelconf this
	// build carries constructs none and fails the node's start.
	pullReader bool
	// baseappInstrument is extensions.instruments naming the SDK's baseapp
	// instrument, which the SDK then starts in its own init.
	baseappInstrument bool
}

// readOtelFile inspects configuration using SDK-compatible decoding. Missing/empty files enable no
// instruments; malformed extension blocks are ignored here as by the SDK. SDK validation remains
// authoritative.
func readOtelFile(path string) (otelFile, error) {
	bz, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return otelFile{}, nil
	}
	if err != nil {
		return otelFile{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(bz) == 0 {
		return otelFile{}, nil
	}

	var doc struct {
		MeterProvider *struct {
			Readers []struct {
				Pull *struct{} `yaml:"pull"`
			} `yaml:"readers"`
		} `yaml:"meter_provider"`
	}
	if err := yaml.Unmarshal(bz, &doc); err != nil {
		return otelFile{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	f := otelFile{meterProvider: doc.MeterProvider != nil}
	if doc.MeterProvider != nil {
		for _, reader := range doc.MeterProvider.Readers {
			f.pullReader = f.pullReader || reader.Pull != nil
		}
	}

	var supplemental struct {
		Extensions *sdktelemetry.ExtensionOptions `yaml:"extensions"`
	}
	if err := yaml.Unmarshal(bz, &supplemental); err == nil && supplemental.Extensions != nil {
		_, f.baseappInstrument = supplemental.Extensions.Instruments[baseapp.InstrumentName]
	}
	return f, nil
}
