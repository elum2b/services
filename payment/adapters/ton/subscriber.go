package ton

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	serviceerrors "github.com/elum2b/services/errors"
	goroutinemanager "github.com/elum2b/services/internal/utils/goroutine"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	tonclient "github.com/xssnick/tonutils-go/ton"
)

type CallbackJetton func(t *RootJetton) error
type CallbackTON func(t *RootTON) error

type Sub struct {
	Context context.Context
	Block   *tonclient.BlockIDExt
	Api     tonclient.APIClientWrapped
	lt      uint64
	stop    context.CancelFunc
	pool    *liteclient.ConnectionPool
	done    chan struct{}
	onClose func()
	wallet  *address.Address
	manager *goroutinemanager.Manager

	closeOnce sync.Once
	startOnce sync.Once
	doneOnce  sync.Once
	mu        sync.RWMutex
	retryWait time.Duration
	started   bool
	lastErr   error

	clbJetton CallbackJetton
	clbTon    CallbackTON
}

func (s *Sub) OnJetton(clb CallbackJetton) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clbJetton = clb
}

func (s *Sub) OnTON(clb CallbackTON) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clbTon = clb
}

func (s *Sub) LastLT() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lt
}

func (s *Sub) Err() error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastErr
}

func (s *Sub) JettonMasterAddress(ctx context.Context, jettonWalletAddress string) (string, error) {
	if s == nil || s.Api == nil {
		return "", ErrSubscriberNotInitialized
	}
	wallet, err := address.ParseAddr(jettonWalletAddress)
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeInvalidFields, "ton jetton wallet address is invalid", err)
	}
	block, err := s.Api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeUnavailable, "ton masterchain info request failed", err)
	}
	result, err := s.Api.WaitForBlock(block.SeqNo).RunGetMethod(ctx, block, wallet, "get_wallet_data")
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeUnavailable, "ton jetton wallet data request failed", err)
	}
	masterSlice, err := result.Slice(2)
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeInternalError, "ton jetton master result read failed", err)
	}
	master, err := masterSlice.LoadAddr()
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeInternalError, "ton jetton master address read failed", err)
	}
	if master == nil || master.IsAddrNone() {
		return "", ErrJettonMasterAddressEmpty
	}
	return master.StringRaw(), nil
}

func NewSub(ctx context.Context, stop context.CancelFunc, addr string, networkConfigURL string, lt ...uint64) (*Sub, error) {
	client := liteclient.NewConnectionPool()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = client.StickyContext(ctx)

	cfg, err := liteclient.GetConfigFromUrl(ctx, networkConfigURL)
	if err != nil {
		return nil, err
	}
	if err := client.AddConnectionsFromConfig(ctx, cfg); err != nil {
		return nil, err
	}

	api := tonclient.NewAPIClient(client, tonclient.ProofCheckPolicyFast).WithRetryTimeout(0, 5*time.Second)
	api.SetTrustedBlockFromConfig(cfg)

	master, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, err
	}

	treasuryAddress := address.MustParseAddr(addr)
	acc, err := api.GetAccount(ctx, master, treasuryAddress)
	if err != nil {
		return nil, err
	}

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, err
	}

	lastTxLT := acc.LastTxLT
	if len(lt) > 0 && lt[0] > 0 {
		lastTxLT = lt[0]
	}

	sub := &Sub{
		Context: ctx,
		Block:   block,
		Api:     api,
		lt:      lastTxLT,
		stop:    stop,
		pool:    client,
		done:    make(chan struct{}),
		wallet:  treasuryAddress,
		manager: goroutinemanager.New(),
	}

	return sub, nil
}

func (s *Sub) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		s.mu.Lock()
		s.started = true
		s.mu.Unlock()

		transactions := make(chan *tlb.Transaction)
		s.manager.Go("payment.ton.subscriber.dispatch", func() {
			defer s.finish()
			s.subscribe(transactions)
		})
		s.manager.Go("payment.ton.subscriber.stop_pool", func() {
			<-s.Context.Done()
			s.pool.Stop()
		})
		s.manager.Go("payment.ton.subscriber.transactions", func() {
			s.Api.SubscribeOnTransactions(s.Context, s.wallet, s.LastLT(), transactions)
		})
	})
}

func (s *Sub) finish() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
}

func (s *Sub) fail(err error) {
	s.mu.Lock()
	if s.lastErr == nil {
		s.lastErr = err
	}
	s.mu.Unlock()
	if s.stop != nil {
		s.stop()
	}
	if s.pool != nil {
		s.pool.Stop()
	}
}

func (s *Sub) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.stop != nil {
			s.stop()
		}
		if s.pool != nil {
			s.pool.Stop()
		}
		s.mu.RLock()
		started := s.started
		s.mu.RUnlock()
		if !started {
			s.finish()
		}
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
		}
		if s.manager != nil {
			s.manager.Close()
		}
		if s.onClose != nil {
			s.onClose()
		}
	})
	return nil
}

func (s *Sub) subscribe(channel chan *tlb.Transaction) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from TON subscribe panic:", r)
			s.subscribe(channel)
		}
	}()

	for tx := range channel {
		if tx.IO.In == nil || tx.IO.In.MsgType != tlb.MsgTypeInternal {
			continue
		}
		if dsc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary); ok && dsc.BouncePhase != nil {
			if _, ok = dsc.BouncePhase.Phase.(tlb.BouncePhaseOk); ok {
				continue
			}
		}

		ti := tx.IO.In.AsInternal()
		bodyCell, err := ti.Body.BeginParse()
		if err != nil || bodyCell == nil {
			continue
		}

		opCode, err := bodyCell.LoadUInt(32)
		if err != nil {
			opCode = 0x00000000
		}

		switch opCode {
		case 0x7362d09c:
			s.mu.RLock()
			callback := s.clbJetton
			s.mu.RUnlock()
			if callback == nil {
				s.fail(errors.New("ton: jetton callback is not configured"))
				return
			}
			body, err := s.JettonBody(ti, tx.Hash)
			if err != nil {
				s.fail(fmt.Errorf("ton: parse jetton transfer: %w", err))
				return
			}
			if !s.runCallback(func() error { return callback(body) }) {
				return
			}
			s.setLastLT(body.CreatedLT)
		case 0x00000000:
			s.mu.RLock()
			callback := s.clbTon
			s.mu.RUnlock()
			if callback == nil {
				s.fail(errors.New("ton: TON callback is not configured"))
				return
			}
			body, err := s.TonBody(ti, tx.Hash)
			if err != nil {
				s.fail(fmt.Errorf("ton: parse TON transfer: %w", err))
				return
			}
			if !s.runCallback(func() error { return callback(body) }) {
				return
			}
			s.setLastLT(body.CreatedLT)
		}
	}
}

func (s *Sub) runCallback(callback func() error) bool {
	delay := s.retryWait
	if delay <= 0 {
		delay = time.Second
	}
	for {
		if err := callback(); err == nil {
			return true
		}
		timer := time.NewTimer(delay)
		select {
		case <-s.Context.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
		if delay < time.Minute {
			delay *= 2
			if delay > time.Minute {
				delay = time.Minute
			}
		}
	}
}

func (s *Sub) setLastLT(value uint64) {
	s.mu.Lock()
	s.lt = value
	s.mu.Unlock()
}
