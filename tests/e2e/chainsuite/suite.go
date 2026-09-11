// SPDX-License-Identifier: Apache-2.0
// Adapted from Gaia, tests/interchain/chainsuite/suite.go.
// Modified for Ark: chain lifecycle and test setup.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package chainsuite

import (
	"context"

	"github.com/stretchr/testify/suite"
)

// Suite creates a chain for its tests, upgrading it first when asked.
type Suite struct {
	suite.Suite
	Config SuiteConfig
	Env    Environment
	Chain  *Chain
	ctx    context.Context
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

// UpgradeChain moves the chain to the image under test when the environment
// names an old one to start from.
func (s *Suite) UpgradeChain() {
	s.Require().NoError(s.Chain.UpgradeToImageUnderTest(s.GetContext(), s.Env))
}
