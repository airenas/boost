package storagemarket

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/filecoin-project/boost/api"
	"github.com/filecoin-project/boost/build"
	"github.com/filecoin-project/boost/db"
	"github.com/filecoin-project/boost/db/migrations"
	"github.com/filecoin-project/boost/fundmanager"
	"github.com/filecoin-project/boost/node/modules/dtypes"
	"github.com/filecoin-project/boost/sealingpipeline"
	"github.com/filecoin-project/boost/storagemanager"
	"github.com/filecoin-project/boost/storagemarket/logs"
	"github.com/filecoin-project/boost/storagemarket/types"
	smtypes "github.com/filecoin-project/boost/storagemarket/types"
	"github.com/filecoin-project/boost/storagemarket/types/dealcheckpoints"
	"github.com/filecoin-project/boost/transport"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-fil-markets/piecestore"
	"github.com/filecoin-project/go-fil-markets/shared"
	"github.com/filecoin-project/go-fil-markets/storagemarket"
	"github.com/filecoin-project/go-fil-markets/stores"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	ctypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/markets/utils"
	sealing "github.com/filecoin-project/lotus/storage/pipeline"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/peer"
)

var (
	ErrDealNotFound        = fmt.Errorf("deal not found")
	ErrDealHandlerNotFound = errors.New("deal handler not found")
)

var (
	addPieceRetryWait    = 5 * time.Minute
	addPieceRetryTimeout = 6 * time.Hour
)

type Config struct {
	// The maximum amount of time a transfer can take before it fails
	MaxTransferDuration time.Duration
	// Whether to do commp on the Boost node (local) or the sealing node (remote)
	RemoteCommp bool
	// The number of commp processes that can run in parallel
	MaxConcurrentLocalCommp uint64
	TransferLimiter         TransferLimiterConfig
}

var log = logging.Logger("boost-provider")

type Provider struct {
	config Config
	// Address of the provider on chain.
	Address address.Address

	ctx       context.Context
	cancel    context.CancelFunc
	closeSync sync.Once
	runWG     sync.WaitGroup

	newDealPS *newDealPS

	// channels used to pass messages to run loop
	acceptDealChan       chan acceptDealReq
	finishedDealChan     chan finishedDealReq
	publishedDealChan    chan publishDealReq
	updateRetryStateChan chan updateRetryStateReq
	storageSpaceChan     chan storageSpaceDealReq

	// Sealing Pipeline API
	sps sealingpipeline.API

	// Boost deal filter
	df dtypes.StorageDealFilter

	// Database API
	db        *sql.DB
	dealsDB   *db.DealsDB
	logsSqlDB *sql.DB
	logsDB    *db.LogsDB

	Transport      transport.Transport
	xferLimiter    *transferLimiter
	fundManager    *fundmanager.FundManager
	storageManager *storagemanager.StorageManager
	dealPublisher  types.DealPublisher
	transfers      *dealTransfers

	pieceAdder                  types.PieceAdder
	commpThrottle               chan struct{}
	commpCalc                   smtypes.CommpCalculator
	maxDealCollateralMultiplier uint64
	chainDealManager            types.ChainDealManager

	fullnodeApi v1api.FullNode

	dhsMu sync.RWMutex
	dhs   map[uuid.UUID]*dealHandler // Map of deal handlers indexed by deal uuid.

	dealLogger *logs.DealLogger

	dagst stores.DAGStoreWrapper
	ps    piecestore.PieceStore

	ip          types.IndexProvider
	askGetter   types.AskGetter
	sigVerifier types.SignatureVerifier
}

func NewProvider(cfg Config, sqldb *sql.DB, dealsDB *db.DealsDB, fundMgr *fundmanager.FundManager, storageMgr *storagemanager.StorageManager,
	fullnodeApi v1api.FullNode, dp types.DealPublisher, addr address.Address, pa types.PieceAdder, commpCalc smtypes.CommpCalculator,
	sps sealingpipeline.API, cm types.ChainDealManager, df dtypes.StorageDealFilter, logsSqlDB *sql.DB, logsDB *db.LogsDB,
	dagst stores.DAGStoreWrapper, ps piecestore.PieceStore, ip types.IndexProvider, askGetter types.AskGetter,
	sigVerifier types.SignatureVerifier, dl *logs.DealLogger, tspt transport.Transport) (*Provider, error) {

	xferLimiter, err := newTransferLimiter(cfg.TransferLimiter)
	if err != nil {
		return nil, err
	}

	newDealPS, err := newDealPubsub()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Make sure that max concurrent local commp is at least 1
	if cfg.MaxConcurrentLocalCommp == 0 {
		cfg.MaxConcurrentLocalCommp = 1
	}

	return &Provider{
		ctx:       ctx,
		cancel:    cancel,
		config:    cfg,
		Address:   addr,
		newDealPS: newDealPS,
		db:        sqldb,
		dealsDB:   dealsDB,
		logsSqlDB: logsSqlDB,
		sps:       sps,
		df:        df,

		acceptDealChan:       make(chan acceptDealReq),
		finishedDealChan:     make(chan finishedDealReq),
		publishedDealChan:    make(chan publishDealReq),
		updateRetryStateChan: make(chan updateRetryStateReq),
		storageSpaceChan:     make(chan storageSpaceDealReq),

		Transport:      tspt,
		xferLimiter:    xferLimiter,
		fundManager:    fundMgr,
		storageManager: storageMgr,

		dealPublisher:               dp,
		fullnodeApi:                 fullnodeApi,
		pieceAdder:                  pa,
		commpThrottle:               make(chan struct{}, cfg.MaxConcurrentLocalCommp),
		commpCalc:                   commpCalc,
		chainDealManager:            cm,
		maxDealCollateralMultiplier: 2,
		transfers:                   newDealTransfers(),

		dhs:        make(map[uuid.UUID]*dealHandler),
		dealLogger: dl,
		logsDB:     logsDB,

		dagst: dagst,
		ps:    ps,

		ip:          ip,
		askGetter:   askGetter,
		sigVerifier: sigVerifier,
	}, nil
}

func (p *Provider) Deal(ctx context.Context, dealUuid uuid.UUID) (*types.ProviderDealState, error) {
	deal, err := p.dealsDB.ByID(ctx, dealUuid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("getting deal %s: %w", dealUuid, ErrDealNotFound)
	}
	return deal, nil
}

func (p *Provider) DealBySignedProposalCid(ctx context.Context, propCid cid.Cid) (*types.ProviderDealState, error) {
	deal, err := p.dealsDB.BySignedProposalCID(ctx, propCid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("getting deal %s: %w", propCid, ErrDealNotFound)
	}
	return deal, nil
}

func (p *Provider) GetAsk() *storagemarket.SignedStorageAsk {
	return p.askGetter.GetAsk()
}

// ImportOfflineDealData is called when the Storage Provider imports data for
// an offline deal (the deal must already have been proposed by the client)
func (p *Provider) ImportOfflineDealData(dealUuid uuid.UUID, filePath string) (pi *api.ProviderDealRejectionInfo, err error) {
	p.dealLogger.Infow(dealUuid, "import data for offline deal", "filepath", filePath)

	// db should already have a deal with this uuid as the deal proposal should have been agreed before hand
	ds, err := p.dealsDB.ByID(p.ctx, dealUuid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no pre-existing deal proposal for offline deal %s: %w", dealUuid, err)
		}
		return nil, fmt.Errorf("getting offline deal %s: %w", dealUuid, err)
	}
	if !ds.IsOffline {
		return nil, fmt.Errorf("deal %s is not an offline deal", dealUuid)
	}
	if ds.Checkpoint > dealcheckpoints.Accepted {
		return nil, fmt.Errorf("deal %s has already been imported and reached checkpoint %s", dealUuid, ds.Checkpoint)
	}

	ds.InboundFilePath = filePath

	resp, err := p.checkForDealAcceptance(ds, true)
	if err != nil {
		p.dealLogger.LogError(dealUuid, "failed to send deal for acceptance", err)
		return nil, fmt.Errorf("failed to send deal for acceptance: %w", err)
	}

	// if there was an error, we don't return a rejection reason
	if resp.err != nil {
		return nil, fmt.Errorf("failed to accept deal: %w", resp.err)
	}

	// return rejection reason as provider has rejected the deal.
	if !resp.ri.Accepted {
		p.dealLogger.Infow(dealUuid, "deal execution rejected by provider", "reason", resp.ri.Reason)
		return resp.ri, nil
	}

	p.dealLogger.Infow(dealUuid, "offline deal data imported and deal scheduled for execution")
	return resp.ri, nil
}

// ExecuteDeal is called when the Storage Provider receives a deal proposal
// from the network
func (p *Provider) ExecuteDeal(dp *types.DealParams, clientPeer peer.ID) (*api.ProviderDealRejectionInfo, error) {
	p.dealLogger.Infow(dp.DealUUID, "executing deal proposal received from network", "peer", clientPeer)

	ds := types.ProviderDealState{
		DealUuid:           dp.DealUUID,
		ClientDealProposal: dp.ClientDealProposal,
		ClientPeerID:       clientPeer,
		DealDataRoot:       dp.DealDataRoot,
		Transfer:           dp.Transfer,
		IsOffline:          dp.IsOffline,
		Retry:              smtypes.DealRetryAuto,
	}
	// validate the deal proposal
	if err := p.validateDealProposal(ds); err != nil {
		reason := err.reason
		if reason == "" {
			reason = err.Error()
		}
		p.dealLogger.Infow(dp.DealUUID, "deal proposal failed validation", "err", err.Error(), "reason", reason)

		return &api.ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("failed validation: %s", reason),
		}, nil
	}

	return p.executeDeal(ds)
}

// executeDeal sends the deal to the main provider run loop for execution
func (p *Provider) executeDeal(ds smtypes.ProviderDealState) (*api.ProviderDealRejectionInfo, error) {
	ri, err := func() (*api.ProviderDealRejectionInfo, error) {
		// send the deal to the main provider loop for execution
		resp, err := p.checkForDealAcceptance(&ds, false)
		if err != nil {
			p.dealLogger.LogError(ds.DealUuid, "failed to send deal for acceptance", err)
			return nil, fmt.Errorf("failed to send deal for acceptance: %w", err)
		}

		// if there was an error, we don't return a rejection reason, just the error.
		if resp.err != nil {
			return nil, fmt.Errorf("failed to accept deal: %w", resp.err)
		}

		// log rejection reason as provider has rejected the deal.
		if !resp.ri.Accepted {
			p.dealLogger.Infow(ds.DealUuid, "deal rejected by provider", "reason", resp.ri.Reason)
		}

		return resp.ri, nil
	}()
	if err != nil || ri == nil || !ri.Accepted {
		// if there was an error processing the deal, or the deal was rejected, return
		return ri, err
	}

	if ds.IsOffline {
		p.dealLogger.Infow(ds.DealUuid, "offline deal accepted, waiting for data import")
	} else {
		p.dealLogger.Infow(ds.DealUuid, "deal accepted and scheduled for execution")
	}

	return ri, nil
}

func (p *Provider) checkForDealAcceptance(ds *types.ProviderDealState, isImport bool) (acceptDealResp, error) {
	// send message to run loop to run the deal through the acceptance filter and reserve the required resources
	// then wait for a response and return the response to the client.
	respChan := make(chan acceptDealResp, 1)
	select {
	case p.acceptDealChan <- acceptDealReq{rsp: respChan, deal: ds, isImport: isImport}:
	case <-p.ctx.Done():
		return acceptDealResp{}, p.ctx.Err()
	}

	var resp acceptDealResp
	select {
	case resp = <-respChan:
	case <-p.ctx.Done():
		return acceptDealResp{}, p.ctx.Err()
	}

	return resp, nil
}

func (p *Provider) Start() error {
	log.Infow("storage provider: starting")

	// initialize the database
	log.Infow("db: creating tables")
	err := db.CreateAllBoostTables(p.ctx, p.db, p.logsSqlDB)
	if err != nil {
		return fmt.Errorf("failed to init db: %w", err)
	}

	log.Infow("db: performing migrations")
	err = migrations.Migrate(p.db)
	if err != nil {
		return fmt.Errorf("failed to migrate db: %w", err)
	}

	log.Infow("db: initialized")

	// cleanup all completed deals in case Boost resumed before they were cleanedup
	finished, err := p.dealsDB.ListCompleted(p.ctx)
	if err != nil {
		return fmt.Errorf("failed to list completed deals: %w", err)
	}
	if len(finished) > 0 {
		log.Infof("cleaning up %d completed deals", len(finished))
	}
	for i := range finished {
		p.cleanupDealOnRestart(finished[i])
	}
	if len(finished) > 0 {
		log.Infof("finished cleaning up %d completed deals", len(finished))
	}

	// restart all active deals
	activeDeals, err := p.dealsDB.ListActive(p.ctx)
	if err != nil {
		return fmt.Errorf("failed to list active deals: %w", err)
	}

	// cleanup all deals that have finished successfully
	for _, deal := range activeDeals {
		// Make sure that deals that have reached the index and announce stage
		// have their resources untagged
		// TODO Update this once we start listening for expired/slashed deals etc
		if deal.Checkpoint >= dealcheckpoints.IndexedAndAnnounced {
			// cleanup if cleanup didn't finish before we restarted
			p.cleanupDealOnRestart(deal)
		}
	}

	// Restart active deals
	for _, deal := range activeDeals {
		// Check if deal is already proving
		if deal.Checkpoint >= dealcheckpoints.IndexedAndAnnounced {
			si, err := p.sps.SectorsStatus(p.ctx, deal.SectorID, false)
			if err != nil || isFinalSealingState(si.State) {
				continue
			}
		}

		// Set up a deal handler so that clients can subscribe to update
		// events about the deal
		dh, err := p.mkAndInsertDealHandler(deal.DealUuid)
		if err != nil {
			p.dealLogger.LogError(deal.DealUuid, "failed to restart deal", err)
			continue
		}

		// If it's an offline deal, and the deal data hasn't yet been
		// imported, just wait for the SP operator to import the data
		if deal.IsOffline && deal.InboundFilePath == "" {
			p.dealLogger.Infow(deal.DealUuid, "restarted deal: waiting for offline deal data import")
			continue
		}

		// Check if the deal can be restarted automatically.
		// Note that if the retry type is "fatal" then the deal should already
		// have been marked as complete (and therefore not returned by ListActive).
		if deal.Retry != smtypes.DealRetryAuto {
			p.dealLogger.Infow(deal.DealUuid, "deal must be manually restarted: waiting for manual restart")
			continue
		}

		// Restart deal
		p.dealLogger.Infow(deal.DealUuid, "resuming deal on boost restart", "checkpoint", deal.Checkpoint.String())
		_, err = p.startDealThread(dh, deal)
		if err != nil {
			p.dealLogger.LogError(deal.DealUuid, "failed to restart deal", err)
		}
	}

	// Start provider run loop
	go p.run()

	// Start sampling transfer data rate
	go p.transfers.start(p.ctx)

	// Start the transfer limiter
	go p.xferLimiter.run(p.ctx)

	log.Infow("storage provider: started")
	return nil
}

func (p *Provider) cleanupDealOnRestart(deal *types.ProviderDealState) {
	// remove the temp file created for inbound deal data if it is not an offline deal
	if !deal.IsOffline {
		_ = os.Remove(deal.InboundFilePath)
	}

	// untag storage space
	errs := p.storageManager.Untag(p.ctx, deal.DealUuid)
	if errs == nil {
		p.dealLogger.Infow(deal.DealUuid, "untagged storage space")
	}

	// untag funds
	collat, pub, errf := p.fundManager.UntagFunds(p.ctx, deal.DealUuid)
	if errf == nil {
		p.dealLogger.Infow(deal.DealUuid, "untagged funds for deal as deal has finished", "untagged publish", pub, "untagged collateral", collat)
	}
}

func (p *Provider) Stop() {
	p.closeSync.Do(func() {
		log.Infow("storage provider: shutdown")

		deals, err := p.dealsDB.ListActive(p.ctx)
		if err == nil {
			for i := range deals {
				dl := deals[i]
				if dl.Checkpoint < dealcheckpoints.AddedPiece {
					log.Infow("shutting down running deal", "id", dl.DealUuid.String(), "ckp", dl.Checkpoint.String())
				}
			}
		}

		log.Infow("storage provider: stop run loop")
		p.cancel()
		p.runWG.Wait()
		log.Info("storage provider: shutdown complete")
	})
}

// SubscribeNewDeals subscribes to "new deal" events
func (p *Provider) SubscribeNewDeals() (event.Subscription, error) {
	return p.newDealPS.subscribe()
}

// SubscribeDealUpdates subscribes to updates to a deal
func (p *Provider) SubscribeDealUpdates(dealUuid uuid.UUID) (event.Subscription, error) {
	dh := p.getDealHandler(dealUuid)
	if dh == nil {
		return nil, ErrDealHandlerNotFound
	}

	return dh.subscribeUpdates()
}

// RetryPausedDeal starts execution of a deal from the point at which it stopped
func (p *Provider) RetryPausedDeal(dealUuid uuid.UUID) error {
	return p.updateRetryState(dealUuid, true)
}

// FailPausedDeal moves a deal from the paused state to the failed state
func (p *Provider) FailPausedDeal(dealUuid uuid.UUID) error {
	return p.updateRetryState(dealUuid, false)
}

// updateRetryState either retries the deal or terminates the deal
// (depending on the value of retry)
func (p *Provider) updateRetryState(dealUuid uuid.UUID, retry bool) error {
	resp := make(chan error, 1)
	select {
	case p.updateRetryStateChan <- updateRetryStateReq{dealUuid: dealUuid, retry: retry, done: resp}:
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
	select {
	case err := <-resp:
		return err
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

func (p *Provider) CancelDealDataTransfer(dealUuid uuid.UUID) error {
	// Ideally, the UI should never show the cancel data transfer button for an offline deal
	pds, err := p.dealsDB.ByID(p.ctx, dealUuid)
	if err != nil {
		return fmt.Errorf("failed to lookup deal in DB: %w", err)
	}
	if pds.IsOffline {
		return errors.New("cannot cancel data transfer for an offline deal")
	}

	dh := p.getDealHandler(dealUuid)
	if dh == nil {
		return ErrDealHandlerNotFound
	}

	err = dh.cancelTransfer()
	if err == nil {
		p.dealLogger.Infow(dealUuid, "deal data transfer cancelled by user")
	} else {
		p.dealLogger.Warnw(dealUuid, "error when user tried to cancel deal data transfer", "err", err)
	}
	return err
}

func (p *Provider) AddPieceToSector(ctx context.Context, deal smtypes.ProviderDealState, pieceData io.Reader) (*storagemarket.PackingResult, error) {
	// Sanity check - we must have published the deal before handing it off
	// to the sealing subsystem
	if deal.PublishCID == nil {
		return nil, fmt.Errorf("deal.PublishCid can't be nil")
	}

	sdInfo := lapi.PieceDealInfo{
		DealID:       deal.ChainDealID,
		DealProposal: &deal.ClientDealProposal.Proposal,
		PublishCid:   deal.PublishCID,
		DealSchedule: lapi.DealSchedule{
			StartEpoch: deal.ClientDealProposal.Proposal.StartEpoch,
			EndEpoch:   deal.ClientDealProposal.Proposal.EndEpoch,
		},
		// Assume that it doesn't make sense for a miner not to keep an
		// unsealed copy. TODO: Check that's a valid assumption.
		//KeepUnsealed: deal.FastRetrieval,
		KeepUnsealed: true,
	}

	// Attempt to add the piece to a sector (repeatedly if necessary)
	pieceSize := deal.ClientDealProposal.Proposal.PieceSize.Unpadded()
	sectorNum, offset, err := p.pieceAdder.AddPiece(ctx, pieceSize, pieceData, sdInfo)
	curTime := build.Clock.Now()

	for build.Clock.Since(curTime) < addPieceRetryTimeout {
		if !errors.Is(err, sealing.ErrTooManySectorsSealing) {
			if err != nil {
				p.dealLogger.Warnw(deal.DealUuid, "failed to addPiece for deal, will-retry", "err", err.Error())
			}
			break
		}
		select {
		case <-build.Clock.After(addPieceRetryWait):
			sectorNum, offset, err = p.pieceAdder.AddPiece(ctx, pieceSize, pieceData, sdInfo)
		case <-ctx.Done():
			return nil, fmt.Errorf("error while waiting to retry AddPiece: %w", ctx.Err())
		}
	}

	if err != nil {
		return nil, fmt.Errorf("AddPiece failed: %w", err)
	}
	p.dealLogger.Infow(deal.DealUuid, "added new deal to sector", "sector", sectorNum.String())

	return &storagemarket.PackingResult{
		SectorNumber: sectorNum,
		Offset:       offset,
		Size:         pieceSize.Padded(),
	}, nil
}

func (p *Provider) GetBalance(ctx context.Context, addr address.Address, encodedTs shared.TipSetToken) (storagemarket.Balance, error) {
	tsk, err := ctypes.TipSetKeyFromBytes(encodedTs)
	if err != nil {
		return storagemarket.Balance{}, err
	}

	bal, err := p.fullnodeApi.StateMarketBalance(ctx, addr, tsk)
	if err != nil {
		return storagemarket.Balance{}, err
	}

	return utils.ToSharedBalance(bal), nil
}
