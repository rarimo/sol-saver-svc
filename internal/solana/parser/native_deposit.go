package parser

import (
	"database/sql"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/near/borsh-go"
	"github.com/olegfomenko/solana-go"
	"gitlab.com/distributed_lab/logan/v3"
	"gitlab.com/distributed_lab/logan/v3/errors"
	"gitlab.com/rarify-protocol/sol-saver-svc/internal/config"
	"gitlab.com/rarify-protocol/sol-saver-svc/internal/data"
	"gitlab.com/rarify-protocol/sol-saver-svc/internal/data/pg"
	"gitlab.com/rarify-protocol/solana-program-go/contract"
)

type nativeParser struct {
	log     *logan.Entry
	storage *pg.Storage
}

func NewNativeParser(cfg config.Config) *nativeParser {
	return &nativeParser{
		log:     cfg.Log(),
		storage: cfg.Storage(),
	}
}

var _ Parser = &nativeParser{}

func (n *nativeParser) ParseTransaction(tx solana.Signature, accounts []solana.PublicKey, instruction solana.CompiledInstruction, instructionId int) error {
	n.log.Infof("Found new native deposit in tx: %s id: %d", tx.String(), instructionId)
	var args contract.DepositNativeArgs

	err := borsh.Deserialize(&args, instruction.Data)
	if err != nil {
		return errors.Wrap(err, "error deserializing instruction data")
	}

	if _, err := hexutil.Decode(args.ReceiverAddress); err != nil {
		return errors.Wrap(err, "error parsing receiver address")
	}

	entry := &data.NativeDeposit{
		Hash:          tx.String(),
		InstructionID: instructionId,
		Sender:        accounts[contract.DepositNativeOwnerIndex].String(),
		Receiver:      args.ReceiverAddress,
		TargetNetwork: args.NetworkTo,
		Amount:        int64(args.Amount),
	}

	if args.BundleData != nil && args.BundleSeed != nil {
		entry.BundleData = sql.NullString{String: hexutil.Encode(*args.BundleData), Valid: true}
		entry.BundleData = sql.NullString{String: hexutil.Encode((*args.BundleSeed)[:]), Valid: true}
	}

	return n.storage.Clone().NativeDepositQ().Insert(entry)
}
