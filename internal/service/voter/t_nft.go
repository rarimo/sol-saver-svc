package voter

import (
	"context"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/gogo/protobuf/proto"
	"github.com/near/borsh-go"
	"github.com/olegfomenko/solana-go"
	"github.com/olegfomenko/solana-go/rpc"
	"gitlab.com/distributed_lab/logan/v3/errors"
	rarimotypes "gitlab.com/rarimo/rarimo-core/x/rarimocore/types"
	tokentypes "gitlab.com/rarimo/rarimo-core/x/tokenmanager/types"
	"gitlab.com/rarimo/savers/saver-grpc-lib/voter/verifiers"
	"gitlab.com/rarimo/solana-program-go/contract"
	"gitlab.com/rarimo/solana-program-go/metaplex"
	"google.golang.org/grpc"
)

type nftOperator struct {
	chain  string
	solana *rpc.Client
	rarimo *grpc.ClientConn
}

func NewNFTOperator(chain string, solana *rpc.Client, rarimo *grpc.ClientConn) *nftOperator {
	return &nftOperator{
		chain:  chain,
		solana: solana,
		rarimo: rarimo,
	}
}

func (f *nftOperator) ParseTransaction(ctx context.Context, accounts []solana.PublicKey, instruction solana.CompiledInstruction, transfer *rarimotypes.Transfer) error {
	msg, err := f.GetMessage(ctx, accounts, instruction)
	if err != nil {
		return errors.Wrap(err, "error getting message")
	}

	msg.Tx = transfer.Tx
	msg.EventId = transfer.EventId

	transferResp, err := rarimotypes.NewQueryClient(f.rarimo).Transfer(ctx, &rarimotypes.QueryGetTransferRequest{Msg: *msg})
	if err != nil {
		return errors.Wrap(err, "error querying transfer from core")
	}

	// Disable meta check if item has been already created
	if transferResp.Transfer.Meta == nil {
		transfer.Meta = nil
	}

	if !proto.Equal(&transferResp.Transfer, transfer) {
		return verifiers.ErrWrongOperationContent
	}

	return nil
}

func (f *nftOperator) GetMessage(ctx context.Context, accounts []solana.PublicKey, instruction solana.CompiledInstruction) (*rarimotypes.MsgCreateTransferOp, error) {
	var args contract.DepositNFTArgs
	if err := borsh.Deserialize(&args, instruction.Data); err != nil {
		return nil, errors.Wrap(err, "error desser tx args")
	}

	tokenId := hexutil.Encode(accounts[contract.DepositNFTMintIndex].Bytes())
	address, err := f.getTokenCollectionAddress(accounts[contract.DepositNFTMintIndex])
	if err != nil {
		return nil, errors.Wrap(err, "error getting collection address")
	}

	if address == "" {
		address = tokenId
	}

	from := &tokentypes.OnChainItemIndex{
		Chain:   f.chain,
		Address: address,
		TokenID: tokenId,
	}

	to := &tokentypes.OnChainItemIndex{
		Chain:   args.NetworkTo,
		TokenID: tokenId,
	}

	to.Address, err = f.getTargetAddress(ctx, from, args.NetworkTo)
	if err != nil {
		return nil, err
	}

	meta, err := f.getItemMeta(from)
	if err != nil {
		return nil, err
	}

	msg := &rarimotypes.MsgCreateTransferOp{
		Receiver: args.ReceiverAddress,
		Amount:   "1",
		From:     from,
		To:       to,
		Meta:     meta,
	}

	if args.BundleData != nil && len(*args.BundleData) > 0 && args.BundleSeed != nil {
		msg.BundleData = hexutil.Encode(*args.BundleData)
		msg.BundleSalt = hexutil.Encode((*args.BundleSeed)[:])
	}

	return msg, nil
}

func (f *nftOperator) getTargetAddress(ctx context.Context, from *tokentypes.OnChainItemIndex, toChain string) (string, error) {
	dataResp, err := tokentypes.NewQueryClient(f.rarimo).CollectionData(ctx, &tokentypes.QueryGetCollectionDataRequest{Chain: from.Chain, Address: from.Address})
	if err != nil {
		return "", errors.Wrap(err, "error fetching collection data")
	}

	collectionResp, err := tokentypes.NewQueryClient(f.rarimo).Collection(ctx, &tokentypes.QueryGetCollectionRequest{Index: dataResp.Data.Collection})
	if err != nil {
		return "", errors.Wrap(err, "error fetching collection ")
	}

	for _, index := range collectionResp.Collection.Data {
		if index.Chain == toChain {
			return index.Address, nil
		}
	}

	return "", verifiers.ErrWrongOperationContent
}

func (f *nftOperator) getItemMeta(from *tokentypes.OnChainItemIndex) (*tokentypes.ItemMetadata, error) {
	metadata, err := f.getMetadata(solana.MustPublicKeyFromBase58(from.TokenID))
	if err != nil {
		return nil, errors.Wrap(err, "error fetching metadata from chain")
	}

	imageUrl, imageHash, err := verifiers.GetImage(metadata.Data.URI)
	if err != nil {
		return nil, errors.Wrap(err, "error fetching image")
	}

	return &tokentypes.ItemMetadata{
		ImageUri:  imageUrl,
		ImageHash: imageHash,
		Seed:      "", // Empty because we are creating item on Solana chain => Solana is a native chain for that token.
		Name:      metadata.Data.Name,
		Symbol:    metadata.Data.Symbol,
		Uri:       metadata.Data.URI,
	}, nil
}

func (f *nftOperator) getMetadata(mint solana.PublicKey) (*metaplex.Metadata, error) {
	metadataAddress, _, err := solana.FindTokenMetadataAddress(mint)
	if err != nil {
		return nil, errors.Wrap(err, "error generating metadata key")
	}

	metadataInfo, err := f.solana.GetAccountInfo(context.TODO(), metadataAddress)
	if err != nil {
		return nil, errors.Wrap(err, "error fetching metadata account info")
	}

	metadata := new(metaplex.Metadata)
	return metadata, borsh.Deserialize(metadata, metadataInfo.Value.Data.GetBinary())
}

func (f *nftOperator) getTokenCollectionAddress(mint solana.PublicKey) (string, error) {
	metadata, err := f.getMetadata(mint)
	if err != nil {
		return "", errors.Wrap(err, "error getting metadata")
	}

	// TODO maybe check for zero collection address instead of verified value
	if metadata.Collection == nil || !metadata.Collection.Verified {
		return "", nil
	}

	return hexutil.Encode(metadata.Collection.Address.Bytes()), nil
}
