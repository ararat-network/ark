package keeper_test

import (
	"github.com/ararat-network/ark/x/reserve/types"
)

// TestCommitteeAuthorisationIsSharedAcrossHandlers runs every committee handler past a stranger, a
// stale term, and an expired window. Authorisation comes first, so each call carries nothing but
// its credentials, and a refusal moves no coins and changes no state.
func (s *KeeperTestSuite) TestCommitteeAuthorisationIsSharedAcrossHandlers() {
	committee := testAddress(1)
	stranger := testAddress(9)

	calls := []struct {
		name string
		call func(committee string, term uint64) error
	}{
		{"deploy", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"record update", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeRecordUpdate(s.ctx, &types.MsgCommitteeRecordUpdate{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"attribute return", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"mark impaired", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeMarkImpaired(s.ctx, &types.MsgCommitteeMarkImpaired{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"clear impairment", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeClearImpairment(s.ctx, &types.MsgCommitteeClearImpairment{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"close position", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"burn paper", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeBurnPaper(s.ctx, &types.MsgCommitteeBurnPaper{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"burn surplus", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeBurnSurplus(s.ctx, &types.MsgCommitteeBurnSurplus{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"fund buffer", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"fund insurance", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeFundInsurance(s.ctx, &types.MsgCommitteeFundInsurance{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"correct position", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{Committee: c, ExpectedTerm: term})
			return err
		}},
		{"reverse return", func(c string, term uint64) error {
			_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{Committee: c, ExpectedTerm: term})
			return err
		}},
	}

	for _, entry := range calls {
		s.Run(entry.name, func() {
			s.appointCommittee(committee, 1_000, 0, testAddress(2))
			before, err := s.keeper.ExportGenesis(s.ctx)
			s.Require().NoError(err)

			s.setBlockHeight(100)
			s.Require().ErrorContains(entry.call(stranger, 1), types.ReserveMandateLabel+": signer is not the exact appointed account")
			s.Require().ErrorContains(entry.call(committee, 2), types.ReserveMandateLabel+": term mismatch")
			// The window closes at block 1,000's time, the first instant refused.
			s.setBlockHeight(1_000)
			s.Require().ErrorContains(entry.call(committee, 1), types.ReserveMandateLabel+": mandate is not active")

			after, err := s.keeper.ExportGenesis(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(before, after)
		})
	}
}
