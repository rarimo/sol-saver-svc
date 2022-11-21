package parser

import (
	"context"
	"database/sql"
	"fmt"
	tokentypes "gitlab.com/rarify-protocol/rarimo-core/x/tokenmanager/types"
	"gitlab.com/rarify-protocol/saver-grpc-lib/transactor"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/near/borsh-go"
	"github.com/olegfomenko/solana-go"
	"github.com/olegfomenko/solana-go/rpc"
	"gitlab.com/distributed_lab/logan/v3"
	"gitlab.com/distributed_lab/logan/v3/errors"
	"gitlab.com/rarify-protocol/sol-saver-svc/internal/config"
	"gitlab.com/rarify-protocol/sol-saver-svc/internal/data"
	"gitlab.com/rarify-protocol/sol-saver-svc/internal/data/pg"
	"gitlab.com/rarify-protocol/solana-program-go/contract"
	"gitlab.com/rarify-protocol/solana-program-go/metaplex"
)

type nftParser struct {
	log     *logan.Entry
	storage *pg.Storage
	solana  *rpc.Client
	tx      transactor.Transactor
}

func NewNFTParser(cfg config.Config) *nftParser {
	return &nftParser{
		log:     cfg.Log(),
		storage: cfg.Storage(),
		solana:  cfg.SolanaRPC(),
		tx:      cfg.Transactor(),
	}
}

var _ Parser = &nftParser{}

func (f *nftParser) ParseTransaction(tx solana.Signature, accounts []solana.PublicKey, instruction solana.CompiledInstruction, instructionId int) error {
	f.log.Infof("Found new nft deposit in tx: %s id: %d", tx.String(), instructionId)
	var args contract.DepositNFTArgs

	err := borsh.Deserialize(&args, instruction.Data)
	if err != nil {
		return errors.Wrap(err, "error deserializing instruction data")
	}

	if _, err := hexutil.Decode(args.ReceiverAddress); err != nil {
		return errors.Wrap(err, "error parsing receiver address")
	}

	collection, err := f.getTokenCollectionAddress(accounts[contract.DepositNFTMintIndex])
	if err != nil {
		return errors.Wrap(err, "error getting token collection")
	}

	entry := &data.NftDeposit{
		Hash:          tx.String(),
		InstructionID: instructionId,
		TargetNetwork: args.NetworkTo,

		Receiver: args.ReceiverAddress,

		Mint:       hexutil.Encode(accounts[contract.DepositNFTMintIndex].Bytes()),
		Sender:     hexutil.Encode(accounts[contract.DepositNFTOwnerIndex].Bytes()),
		Collection: sql.NullString{String: collection, Valid: collection != ""},
	}

	if args.BundleData != nil && len(*args.BundleData) > 0 && args.BundleSeed != nil {
		entry.BundleData = sql.NullString{String: hexutil.Encode(*args.BundleData), Valid: true}
		entry.BundleData = sql.NullString{String: hexutil.Encode((*args.BundleSeed)[:]), Valid: true}
	}

	err = f.storage.NftDepositQ().Insert(entry)
	if err != nil {
		return errors.Wrap(err, "error inserting nft deposit", logan.F{
			"tx_hash": tx.String(),
		})
	}

	return f.tx.SubmitTransferOp(
		context.TODO(),
		hexutil.Encode(accounts[contract.DepositNFTOwnerIndex].Bytes()),
		tx.String(),
		fmt.Sprintf("%d", instructionId),
		args.NetworkTo,
		tokentypes.Type_METAPLEX_NFT,
	)
}

func (f *nftParser) getTokenCollectionAddress(mint solana.PublicKey) (string, error) {
	metadataAddress, _, err := solana.FindTokenMetadataAddress(mint)
	if err != nil {
		return "", err
	}

	metadataInfo, err := f.solana.GetAccountInfo(context.TODO(), metadataAddress)
	if err != nil {
		return "", err
	}

	var metadata metaplex.Metadata
	err = borsh.Deserialize(&metadata, metadataInfo.Value.Data.GetBinary())
	if err != nil {
		return "", err
	}

	// TODO maybe check for zero collection address instead of verified value
	if metadata.Collection == nil || metadata.Collection.Verified == false {
		return "", nil
	}

	return hexutil.Encode(metadata.Collection.Address.Bytes()), nil
}
