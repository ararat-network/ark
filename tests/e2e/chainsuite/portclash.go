package chainsuite

import (
	"context"
	"strings"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/dockerutil"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/moby/moby/client"
	"github.com/moby/moby/errdefs"
)

// startAttempts bounds the starts retried after a port clash.
const startAttempts = 3

// isPortClash reports Docker refusing a reserved host port as already
// allocated, which concurrent container starts can provoke.
func isPortClash(err error) bool {
	return err != nil && strings.Contains(err.Error(), "port is already allocated")
}

// retryPortClash runs start again after a port clash, up to startAttempts
// times. Before each retry it removes the containers the failed attempt
// created, whose names the next attempt reuses; containers the test already
// had, such as a chain still running, are left alone.
func retryPortClash(ctx context.Context, testName interchaintest.TestName, start func() error) error {
	cli, _, err := GetDockerContext(ctx)
	if err != nil {
		return err
	}
	existing, err := testContainers(ctx, cli, testName)
	if err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		err := start()
		if !isPortClash(err) || attempt == startAttempts {
			return err
		}
		created, listErr := testContainers(ctx, cli, testName)
		if listErr != nil {
			return listErr
		}
		for id := range created {
			if _, kept := existing[id]; kept {
				continue
			}
			if err := cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
				return err
			}
		}
	}
}

// testContainers lists the containers interchaintest labelled for testName.
func testContainers(ctx context.Context, cli *client.Client, testName interchaintest.TestName) (map[string]struct{}, error) {
	list, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", dockerutil.CleanupLabel+"="+testName.Name())),
	})
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(list))
	for _, c := range list {
		ids[c.ID] = struct{}{}
	}
	return ids, nil
}
