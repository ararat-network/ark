package chainsuite

import "github.com/kelseyhightower/envconfig"

const envPrefix = "TEST"

// Environment is what the suites read from TEST_* variables: which image to
// run, and whether a suite should start on an older image and upgrade.
type Environment struct {
	// DockerRegistry prefixes ImageName; empty selects a local image.
	DockerRegistry string `envconfig:"DOCKER_REGISTRY"`
	// ImageName is the repository `make localnet-build-env` tags.
	ImageName string `envconfig:"IMAGE_NAME" default:"ark/arkd"`
	// ImageVersion is the tag under test; the local build tags latest.
	ImageVersion string `envconfig:"IMAGE_VERSION" default:"latest"`
	// OldImageVersion, when set, is the tag a suite with UpgradeOnSetup
	// starts on before upgrading to ImageVersion.
	OldImageVersion string `envconfig:"OLD_IMAGE_VERSION"`
	// UpgradeName is the plan the new binary registers. Empty with
	// OldImageVersion set means a coordinated binary swap with no plan.
	UpgradeName string `envconfig:"UPGRADE_NAME"`
	// PriceFeed runs a price-feed sidecar beside every validator, the shape
	// a production validator has. Off, validators vote empty extensions and
	// the chain holds no exchange rate.
	PriceFeed bool `envconfig:"PRICEFEED" default:"true"`
}

func GetEnvironment() Environment {
	var env Environment
	envconfig.MustProcess(envPrefix, &env)
	return env
}

// Repository is the image reference without a tag.
func (e Environment) Repository() string {
	if e.DockerRegistry == "" {
		return e.ImageName
	}
	return e.DockerRegistry + "/" + e.ImageName
}

// StartVersion is the tag a chain boots on: the old one when a suite
// upgrades, else the one under test.
func (e Environment) StartVersion() string {
	if e.OldImageVersion != "" {
		return e.OldImageVersion
	}
	return e.ImageVersion
}
