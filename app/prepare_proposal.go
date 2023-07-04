package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	abci "github.com/tendermint/tendermint/abci/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
)

// PrepareProposal fulfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. The square size is determined by first
// estimating it via the size of the passed block data. Then, this method
// generates the data root for the proposal block and passes it back to
// tendermint via the BlockData. Panics indicate a developer error and should
// immediately halt the node for visibility and so they can be quickly resolved.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	// create a context using a branch of the state and loaded using the
	// proposal height and chain-id
	sdkCtx := app.NewProposalContext(core.Header{ChainID: app.GetChainID(), Height: app.LastBlockHeight() + 1})
	// filter out invalid transactions.
	// TODO: we can remove all state independent checks from the ante handler here such as signature verification
	// and only check the state dependent checks like fees and nonces as all these transactions have already
	// passed CheckTx.
	handler := NewAnteHandler(
		app.AccountKeeper,
		app.BankKeeper,
		app.BlobKeeper,
		app.FeeGrantKeeper,
		app.GetTxConfig().SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		app.IBCKeeper,
	)
	fmt.Printf("num txs before filtering: %d\n", len(req.BlockData.Txs))
	txs := filterTxs(sdkCtx, handler, app.txConfig, req.BlockData.Txs)
	fmt.Printf("num txs after filtering: %d\n", len(txs))

	// build the square from the set of valid and prioritised transactions.
	// The txs returned are the ones used in the square and block
	dataSquare, txs, err := square.Build(txs, app.GetBaseApp().AppVersion(), app.GovSquareSizeUpperBound(sdkCtx))
	if err != nil {
		panic(err)
	}

	numTxs := len(txs)

	if numTxs > 0 {
		totalSize := 0
		for _, tx := range txs {
			totalSize += len(tx)
		}
		fmt.Printf("proposal block %d created with %d txs and %d bytes\n\n", sdkCtx.BlockHeight(), numTxs, totalSize)

	}

	// erasure the data square which we use to create the data root.
	// Note: uses the nmt wrapper to construct the tree.
	// checkout pkg/wrapper/nmt_wrapper.go for more information.
	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	if err != nil {
		app.Logger().Error(
			"failure to erasure the data square while creating a proposal block",
			"error",
			err.Error(),
		)
		panic(err)
	}

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah := da.NewDataAvailabilityHeader(eds)

	// tendermint doesn't need to use any of the erasure data, as only the
	// protobuf encoded version of the block data is gossiped.
	return abci.ResponsePrepareProposal{
		BlockData: &core.Data{
			Txs:        txs,
			SquareSize: uint64(dataSquare.Size()),
			Hash:       dah.Hash(),
		},
	}
}
