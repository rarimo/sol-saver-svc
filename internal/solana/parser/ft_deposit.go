package parser

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/near/borsh-go"
	"github.com/olegfomenko/solana-go"
	"gitlab.com/distributed_lab/logan/v3"
	rarimocore "gitlab.com/rarimo/rarimo-core/x/rarimocore/types"
	tokentypes "gitlab.com/rarimo/rarimo-core/x/tokenmanager/types"
	"gitlab.com/rarimo/savers/saver-grpc-lib/broadcaster"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"gitlab.com/distributed_lab/logan/v3/errors"
	"gitlab.com/rarimo/savers/sol-saver-svc/internal/config"
	"gitlab.com/rarimo/savers/sol-saver-svc/internal/data"
	"gitlab.com/rarimo/savers/sol-saver-svc/internal/data/pg"
	"gitlab.com/rarimo/solana-program-go/contract"
)

type ftParser struct {
	log     *logan.Entry
	storage *pg.Storage
	cli     broadcaster.Broadcaster
}

func NewFTParser(cfg config.Config) *ftParser {
	return &ftParser{
		log:     cfg.Log(),
		storage: cfg.Storage(),
		cli:     cfg.Broadcaster(),
	}
}

var _ Parser = &ftParser{}

func (f *ftParser) ParseTransaction(tx solana.Signature, accounts []solana.PublicKey, instruction solana.CompiledInstruction, instructionId int) error {
	f.log.Infof("Found new ft deposit in tx: %s id: %d", tx.String(), instructionId)
	var args contract.DepositFTArgs

	err := borsh.Deserialize(&args, instruction.Data)
	if err != nil {
		return errors.Wrap(err, "error deserializing instruction data")
	}

	entry := &data.FtDeposit{
		Hash:          tx.String(),
		InstructionID: instructionId,
		TargetNetwork: args.NetworkTo,

		Amount: int64(args.Amount),

		Receiver: args.ReceiverAddress,

		Sender: hexutil.Encode(accounts[contract.DepositFTOwnerIndex].Bytes()),
		Mint:   hexutil.Encode(accounts[contract.DepositFTMintIndex].Bytes()),
	}

	if args.BundleData != nil && len(*args.BundleData) > 0 && args.BundleSeed != nil {
		entry.BundleData = sql.NullString{String: hexutil.Encode(*args.BundleData), Valid: true}
		entry.BundleData = sql.NullString{String: hexutil.Encode((*args.BundleSeed)[:]), Valid: true}
	}

	err = f.storage.Clone().FtDepositQ().Insert(entry)
	if err != nil {
		return errors.Wrap(err, "error inserting ft deposit", logan.F{
			"tx_hash": tx.String(),
		})
	}

	return f.cli.BroadcastTx(
		context.TODO(),
		rarimocore.NewMsgCreateTransferOp(
			f.cli.Sender(),
			hexutil.Encode(accounts[contract.DepositFTOwnerIndex].Bytes()),
			fmt.Sprintf("%d", instructionId),
			args.NetworkTo,
			tokentypes.Type_METAPLEX_FT,
		),
	)
}