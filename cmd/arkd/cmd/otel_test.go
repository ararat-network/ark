package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// otel.yaml documents in the shapes start tells apart.
const (
	otelTracesOnly = `file_format: "1.0-rc.3"
tracer_provider:
  processors:
    - batch:
        exporter:
          otlp_grpc:
            endpoint: http://localhost:4317
`
	otelPeriodicReader = `file_format: "1.0-rc.3"
meter_provider:
  readers:
    - periodic:
        exporter:
          otlp_grpc:
            endpoint: http://localhost:4317
`
	otelPullReader = `file_format: "1.0-rc.3"
meter_provider:
  readers:
    - pull:
        exporter:
          prometheus:
            host: 0.0.0.0
            port: 9464
`
	otelBaseappInstrument = `file_format: "1.0-rc.3"
extensions:
  instruments:
    baseapp: {}
    host: {}
`
	// otelIgnoredExtensions is a block the SDK's own decode drops without
	// error, so it names no instrument.
	otelIgnoredExtensions = `file_format: "1.0-rc.3"
meter_provider:
  readers: []
extensions: 3
`
)

func TestReadOtelFile(t *testing.T) {
	tests := []struct {
		name        string
		missing     bool
		doc         string
		want        otelFile
		errorSubstr string
	}{
		{
			name:    "missing",
			missing: true,
		},
		{
			name: "empty",
		},
		{
			name: "traces only",
			doc:  otelTracesOnly,
		},
		{
			name: "periodic reader",
			doc:  otelPeriodicReader,
			want: otelFile{meterProvider: true},
		},
		{
			name: "pull reader",
			doc:  otelPullReader,
			want: otelFile{meterProvider: true, pullReader: true},
		},
		{
			name: "baseapp instrument",
			doc:  otelBaseappInstrument,
			want: otelFile{baseappInstrument: true},
		},
		{
			name: "extensions the SDK ignores",
			doc:  otelIgnoredExtensions,
			want: otelFile{meterProvider: true},
		},
		{
			name:        "malformed",
			doc:         "meter_provider: [\n",
			errorSubstr: "parsing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if !tt.missing {
				writeOtelFile(t, root, tt.doc)
			}

			got, err := readOtelFile(filepath.Join(root, "config", "otel.yaml"))
			if tt.errorSubstr != "" {
				require.ErrorContains(t, err, tt.errorSubstr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
