package chainsuite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/testreporter"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type (
	testReporterKey        struct{}
	relayerExecReporterKey struct{}
	loggerKey              struct{}
	dockerKey              struct{}
)

type dockerContext struct {
	NetworkID string
	Client    *client.Client
}

func WithDockerContext(ctx context.Context, d *dockerContext) context.Context {
	return context.WithValue(ctx, dockerKey{}, d)
}

// GetDockerContext is the Docker client and network NewSuiteContext set up.
func GetDockerContext(ctx context.Context) (*client.Client, string, error) {
	d, ok := ctx.Value(dockerKey{}).(*dockerContext)
	if !ok {
		return nil, "", errors.New("context carries no Docker setup; it was not made by NewSuiteContext")
	}
	return d.Client, d.NetworkID, nil
}

func WithTestReporter(ctx context.Context, r *testreporter.Reporter) context.Context {
	return context.WithValue(ctx, testReporterKey{}, r)
}

func GetTestReporter(ctx context.Context) *testreporter.Reporter {
	r, _ := ctx.Value(testReporterKey{}).(*testreporter.Reporter)
	return r
}

func WithRelayerExecReporter(ctx context.Context, r *testreporter.RelayerExecReporter) context.Context {
	return context.WithValue(ctx, relayerExecReporterKey{}, r)
}

func GetRelayerExecReporter(ctx context.Context) *testreporter.RelayerExecReporter {
	r, _ := ctx.Value(relayerExecReporterKey{}).(*testreporter.RelayerExecReporter)
	return r
}

func WithLogger(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

func GetLogger(ctx context.Context) *zap.Logger {
	l, _ := ctx.Value(loggerKey{}).(*zap.Logger)
	return l
}

// NewSuiteContext sets up Docker for one suite and carries the client,
// network, logger, and reporters on the context every helper reads.
func NewSuiteContext(s *suite.Suite) (context.Context, error) {
	ctx := context.Background()

	dockerClient, dockerNetwork := interchaintest.DockerSetup(s.T())
	ctx = WithDockerContext(ctx, &dockerContext{NetworkID: dockerNetwork, Client: dockerClient})
	ctx = WithLogger(ctx, zaptest.NewLogger(s.T()))

	f, err := interchaintest.CreateLogFile(fmt.Sprintf("%d.json", time.Now().Unix()))
	if err != nil {
		return nil, err
	}
	reporter := testreporter.NewReporter(f)
	ctx = WithTestReporter(ctx, reporter)
	ctx = WithRelayerExecReporter(ctx, reporter.RelayerExecReporter(s.T()))
	return ctx, nil
}
