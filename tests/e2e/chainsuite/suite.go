package chainsuite

import (
	"context"

	"github.com/stretchr/testify/suite"
)

// Suite creates a chain for its tests, upgrading it first when asked.
type Suite struct {
	suite.Suite
	Config  SuiteConfig
	Env     Environment
	Chain   *Chain
	Relayer *Relayer
	ctx     context.Context
	// name is the test the Docker resources are labelled with, which is what
	// the cleanup at the end of that test removes. A chain a subtest creates
	// must carry it too, or its containers outlive the suite.
	name string
}

// DockerTestName is the label for Docker resources created outside the
// suite's own setup, so the suite's cleanup removes them. Not named Test…:
// testify would run it.
func (s *Suite) DockerTestName() TestName {
	return TestName(s.name)
}

// TestName names Docker resources; interchaintest takes any Name() string.
type TestName string

func (n TestName) Name() string { return string(n) }

func NewSuite(config SuiteConfig) *Suite {
	env := GetEnvironment()
	return &Suite{Config: DefaultSuiteConfig(env).Merge(config), Env: env}
}

func (s *Suite) createChain() {
	ctx, err := NewSuiteContext(&s.Suite)
	s.Require().NoError(err)
	s.ctx = ctx
	s.name = s.T().Name()
	s.Chain, err = CreateChain(s.GetContext(), s.T(), s.Config.ChainSpec)
	s.Require().NoError(err)
	s.Require().NoError(s.Chain.VerifyGasPrices(s.GetContext()))
	if s.Config.UpgradeOnSetup {
		s.UpgradeChain()
	}
}

func (s *Suite) SetupTest() {
	if s.Config.Scope == ChainScopeTest {
		s.createChain()
	}
}

func (s *Suite) SetupSuite() {
	if s.Config.Scope == ChainScopeSuite {
		s.createChain()
	}
}

func (s *Suite) GetContext() context.Context {
	s.Require().NotNil(s.ctx, "GetContext before SetupSuite ran")
	return s.ctx
}

// UpgradeChain moves the chain from the old image to the one under test:
// through governance when a plan is named, as a coordinated binary swap
// when not. Without an old image there is nothing to move from.
func (s *Suite) UpgradeChain() {
	if s.Env.OldImageVersion == "" {
		s.T().Log("no TEST_OLD_IMAGE_VERSION; running on the image under test without an upgrade")
		return
	}
	log := GetLogger(s.GetContext()).Sugar()
	if s.Env.UpgradeName == "" {
		log.Infof("Swapping %s for %s without a plan", s.Env.OldImageVersion, s.Env.ImageVersion)
		s.Require().NoError(s.Chain.ReplaceImagesAndRestart(s.GetContext(), s.Env.ImageVersion))
	} else {
		log.Infof("Upgrade %s from %s to %s", s.Env.UpgradeName, s.Env.OldImageVersion, s.Env.ImageVersion)
		s.Require().NoError(s.Chain.Upgrade(s.GetContext(), s.Env.UpgradeName, s.Env.ImageVersion))
		applied, err := s.Chain.UpgradeQueryAppliedPlan(s.GetContext(), s.Env.UpgradeName)
		s.Require().NoError(err)
		s.Require().Positive(applied.Height, "plan %s was not applied", s.Env.UpgradeName)
	}
	if s.Relayer != nil {
		rep := GetRelayerExecReporter(s.GetContext())
		s.Require().NoError(s.Relayer.StopRelayer(s.GetContext(), rep))
		s.Require().NoError(s.Relayer.StartRelayer(s.GetContext(), rep))
	}
}
