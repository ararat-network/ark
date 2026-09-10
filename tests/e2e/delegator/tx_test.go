package delegator_test

import (
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/stretchr/testify/suite"
	"github.com/tidwall/gjson"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/delegator"
)

// TxSuite runs the account-level transaction surface through the CLI on one
// validator.
type TxSuite struct {
	*delegator.Suite
}

func (s *TxSuite) TestBankSend() {
	sender := s.Balance(s.Wallet.FormattedAddress())
	amount := chainsuite.NOAH(5)
	s.Send(s.Wallet, s.Wallet2.FormattedAddress(), amount)
	// The fee and the transfer tax come out of the sender beside the principal.
	s.Require().True(s.Balance(s.Wallet.FormattedAddress()).LT(sender.Sub(amount)), "sender paid no fee")
}

func (s *TxSuite) TestEncodeDecode() {
	ctx := s.GetContext()
	node := s.Node()
	txjson, err := s.Chain.GenerateTx(ctx, 0,
		"bank", "send", s.Wallet.FormattedAddress(), s.Wallet2.FormattedAddress(), chainsuite.NOAHCoin(1),
		"--gas", "200000", "--fees", chainsuite.NOAHCoin(1), "--note", "e2e encode",
	)
	s.Require().NoError(err)
	s.Require().NoError(node.WriteFile(ctx, []byte(txjson), "encode.json"))

	encoded, _, err := node.Exec(ctx, node.BinCommand("tx", "encode", path.Join(node.HomeDir(), "encode.json")), nil)
	s.Require().NoError(err)
	decoded, _, err := node.Exec(ctx, node.BinCommand("tx", "decode", strings.TrimSpace(string(encoded))), nil)
	s.Require().NoError(err)

	original := gjson.Parse(txjson)
	roundTrip := gjson.ParseBytes(decoded)
	for _, field := range []string{
		"body.messages.0.to_address",
		"body.messages.0.amount.0.amount",
		"body.memo",
		"auth_info.fee.amount.0.amount",
		"auth_info.fee.gas_limit",
	} {
		s.Require().Equal(original.Get(field).String(), roundTrip.Get(field).String(), field)
	}
}

func (s *TxSuite) TestMultisig() {
	ctx := s.GetContext()
	node := s.Node()
	const multisigName = "multisig"
	_, _, err := node.ExecBin(ctx, "keys", "add", multisigName,
		"--multisig", strings.Join([]string{s.Wallet.KeyName(), s.Wallet2.KeyName(), s.Wallet3.KeyName()}, ","),
		"--multisig-threshold", "2", "--keyring-backend", "test",
	)
	s.Require().NoError(err)
	multisigAddr, err := node.KeyBech32(ctx, multisigName, "")
	s.Require().NoError(err)
	s.Require().NoError(s.Chain.SendFunds(ctx, chainsuite.FaucetKeyName, ibc.WalletAmount{
		Denom: chainsuite.Denom, Amount: chainsuite.NOAH(100), Address: multisigAddr,
	}))
	bogus, err := s.Chain.BuildWallet(ctx, "bogus", "")
	s.Require().NoError(err)

	amount := chainsuite.NOAH(5)
	before := s.Balance(s.Wallet3.FormattedAddress())

	// A fixed gas limit: simulation needs a signer the multisig has not got
	// yet. The chain still prices the fee and the tax.
	txjson, err := s.Chain.GenerateTx(ctx, 0,
		"bank", "send", multisigName, s.Wallet3.FormattedAddress(), amount.String()+chainsuite.Denom,
		"--gas", "300000",
	)
	s.Require().NoError(err)
	s.Require().NoError(node.WriteFile(ctx, []byte(txjson), "tx.json"))
	txPath := path.Join(node.HomeDir(), "tx.json")

	sign := func(wallet ibc.Wallet) ([]byte, error) {
		stdout, _, err := node.Exec(ctx, node.TxCommand(wallet.KeyName(),
			"sign", txPath, "--multisig", multisigAddr, "--sign-mode", "amino-json",
		), nil)
		return stdout, err
	}
	signed0, err := sign(s.Wallet)
	s.Require().NoError(err)
	signed1, err := sign(s.Wallet2)
	s.Require().NoError(err)
	_, err = sign(bogus)
	s.Require().Error(err, "a key outside the multisig must not sign for it")

	s.Require().NoError(node.WriteFile(ctx, signed0, "signed0.json"))
	s.Require().NoError(node.WriteFile(ctx, signed1, "signed1.json"))
	multisigned, _, err := node.Exec(ctx, node.TxCommand(multisigName,
		"multi-sign", txPath, multisigName,
		path.Join(node.HomeDir(), "signed0.json"), path.Join(node.HomeDir(), "signed1.json"),
	), nil)
	s.Require().NoError(err)
	s.Require().NoError(node.WriteFile(ctx, multisigned, "multisigned.json"))

	_, err = node.ExecTx(ctx, multisigName, "broadcast", path.Join(node.HomeDir(), "multisigned.json"))
	s.Require().NoError(err)
	s.Require().Equal(before.Add(amount).String(), s.Balance(s.Wallet3.FormattedAddress()).String())
}

func (s *TxSuite) TestFeeGrant() {
	ctx := s.GetContext()
	granter, grantee, recipient := s.Wallet, s.Wallet2, s.Wallet3

	_, err := s.Node().ExecTx(ctx, granter.KeyName(), "feegrant", "grant", granter.FormattedAddress(), grantee.FormattedAddress())
	s.Require().NoError(err)

	granterBefore := s.Balance(granter.FormattedAddress())
	granteeBefore := s.Balance(grantee.FormattedAddress())
	amount := chainsuite.NOAH(1)
	s.Send(grantee, recipient.FormattedAddress(), amount, "--fee-granter", granter.FormattedAddress())
	// The granter carries the fee and the tax; the grantee loses the principal alone.
	s.Require().Equal(granteeBefore.Sub(amount).String(), s.Balance(grantee.FormattedAddress()).String(), "grantee paid a fee")
	s.Require().True(s.Balance(granter.FormattedAddress()).LT(granterBefore), "granter paid nothing")

	_, err = s.Node().ExecTx(ctx, granter.KeyName(), "feegrant", "revoke", granter.FormattedAddress(), grantee.FormattedAddress())
	s.Require().NoError(err)
	_, err = s.Node().ExecTx(ctx, grantee.KeyName(),
		"bank", "send", grantee.FormattedAddress(), recipient.FormattedAddress(), chainsuite.NOAHCoin(1),
		"--fee-granter", granter.FormattedAddress(),
	)
	s.Require().Error(err, "a revoked grant must not pay")
}

func (s *TxSuite) TestAuthzSend() {
	ctx := s.GetContext()
	node := s.Node()
	granter, grantee, recipient := s.Wallet, s.Wallet2, s.Wallet3

	_, err := node.ExecTx(ctx, granter.KeyName(),
		"authz", "grant", grantee.FormattedAddress(), "send", "--spend-limit", chainsuite.NOAHCoin(10),
	)
	s.Require().NoError(err)

	amount := chainsuite.NOAH(2)
	nested, err := s.Chain.GenerateTx(ctx, 0,
		"bank", "send", granter.FormattedAddress(), recipient.FormattedAddress(), amount.String()+chainsuite.Denom,
		"--gas", "200000", "--fees", chainsuite.NOAHCoin(1),
	)
	s.Require().NoError(err)
	s.Require().NoError(node.WriteFile(ctx, []byte(nested), "authz-send.json"))

	granterBefore := s.Balance(granter.FormattedAddress())
	recipientBefore := s.Balance(recipient.FormattedAddress())
	_, err = node.ExecTx(ctx, grantee.KeyName(), "authz", "exec", path.Join(node.HomeDir(), "authz-send.json"))
	s.Require().NoError(err)
	s.Require().Equal(recipientBefore.Add(amount).String(), s.Balance(recipient.FormattedAddress()).String())
	// The grantee signs and pays; the granter loses the principal alone.
	s.Require().Equal(granterBefore.Sub(amount).String(), s.Balance(granter.FormattedAddress()).String())

	_, err = node.ExecTx(ctx, granter.KeyName(),
		"authz", "revoke", grantee.FormattedAddress(), "/cosmos.bank.v1beta1.MsgSend",
	)
	s.Require().NoError(err)
	_, err = node.ExecTx(ctx, grantee.KeyName(), "authz", "exec", path.Join(node.HomeDir(), "authz-send.json"))
	s.Require().Error(err, "a revoked authorization must not execute")
}

func (s *TxSuite) TestDelayedVesting() {
	ctx := s.GetContext()
	node := s.Node()
	vester, err := s.Chain.BuildWallet(ctx, "vester", "")
	s.Require().NoError(err)

	// The account must not exist before it is created as vesting.
	end := time.Now().Add(90 * time.Second)
	_, err = node.ExecTx(ctx, s.Wallet.KeyName(),
		"vesting", "create-vesting-account", vester.FormattedAddress(), chainsuite.NOAHCoin(5),
		strconv.FormatInt(end.Unix(), 10), "--delayed",
	)
	s.Require().NoError(err)
	account, err := s.Chain.QueryJSON(ctx, "account", "auth", "account", vester.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Contains(account.Raw, "DelayedVestingAccount", account.Raw)

	// Liquid funds on top, for a send under the lock and its fee.
	s.Require().NoError(s.Chain.SendFunds(ctx, chainsuite.FaucetKeyName, ibc.WalletAmount{
		Denom: chainsuite.Denom, Amount: chainsuite.NOAH(3), Address: vester.FormattedAddress(),
	}))

	_, err = node.ExecTx(ctx, vester.KeyName(),
		"bank", "send", vester.FormattedAddress(), s.Wallet3.FormattedAddress(), chainsuite.NOAHCoin(4),
	)
	s.Require().Error(err, "locked coins must not leave before the end time")
	s.Send(vester, s.Wallet3.FormattedAddress(), chainsuite.NOAH(2))

	time.Sleep(time.Until(end) + 2*chainsuite.BlockTime)
	s.Send(vester, s.Wallet3.FormattedAddress(), chainsuite.NOAH(4))
}

func TestTransactions(t *testing.T) {
	s := &TxSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup: true,
	})}}
	suite.Run(t, s)
}
