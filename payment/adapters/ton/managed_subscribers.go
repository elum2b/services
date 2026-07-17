package ton

import (
	"context"
	"log"
	"strings"
	"time"

	services "github.com/elum2b/services"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

const defaultWalletSyncInterval = 30 * time.Second

type managedSubscriber struct {
	params SubscriberParams
	sub    *Sub
}

func (a *TON) StartManagedSubscribers(ctx context.Context, interval time.Duration) <-chan struct{} {
	if a == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	if interval <= 0 {
		interval = defaultWalletSyncInterval
	}
	runCtx, cancel := a.bindContext(ctx)

	a.mu.Lock()
	if a.managedStarted {
		cancel()
		done := a.managedDone
		a.mu.Unlock()
		return done
	}
	a.managedStarted = true
	a.managedCancel = cancel
	a.managedDone = make(chan struct{})
	done := a.managedDone
	a.mu.Unlock()

	a.goroutines.Go("payment.ton.managed_subscribers", func() {
		defer close(done)
		a.managedSubscriberLoop(runCtx, interval)
	})
	return done
}

func (a *TON) managedSubscriberLoop(ctx context.Context, interval time.Duration) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			a.closeManagedSubscribers()
			return
		case <-timer.C:
			a.syncManagedSubscribersSafely(ctx)
			timer.Reset(interval)
		}
	}
}

func (a *TON) syncManagedSubscribersSafely(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil && ctx.Err() == nil {
			log.Printf("payment ton wallet sync recovered from panic: %v", recovered)
		}
	}()
	if err := a.SyncManagedSubscribers(ctx); err != nil && ctx.Err() == nil {
		log.Printf("payment ton wallet sync: %v", err)
	}
}

func (a *TON) SyncManagedSubscribers(ctx context.Context) error {
	if a == nil || a.repository == nil {
		return ErrSubscriberNotInitialized
	}
	rows, err := a.repository.ListEnabledTONWallets(ctx)
	if err != nil {
		return err
	}

	desired := make(map[string]SubscriberParams, len(rows))
	for _, row := range rows {
		params, ok, err := subscriberParamsFromWallet(row)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		desired[managedSubscriberKey(params)] = params
	}

	a.mu.Lock()
	current := make(map[string]managedSubscriber, len(a.managed))
	for key, managed := range a.managed {
		current[key] = managed
	}
	a.mu.Unlock()

	for key, managed := range current {
		want, ok := desired[key]
		if !ok || !sameSubscriberParams(managed.params, want) || managed.sub == nil || managed.sub.Err() != nil {
			_ = managed.sub.Close()
			a.mu.Lock()
			if existing, exists := a.managed[key]; exists && existing.sub == managed.sub {
				delete(a.managed, key)
			}
			a.mu.Unlock()
		}
	}

	for key, params := range desired {
		a.mu.Lock()
		managed, exists := a.managed[key]
		a.mu.Unlock()
		if exists && sameSubscriberParams(managed.params, params) && managed.sub != nil && managed.sub.Err() == nil {
			continue
		}
		sub, err := a.StartSubscriber(ctx, params)
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.managed[key] = managedSubscriber{params: params, sub: sub}
		a.mu.Unlock()
	}
	return nil
}

func (a *TON) closeManagedSubscribers() {
	if a == nil {
		return
	}
	a.mu.Lock()
	managed := make([]managedSubscriber, 0, len(a.managed))
	for _, item := range a.managed {
		managed = append(managed, item)
	}
	a.managed = make(map[string]managedSubscriber)
	a.mu.Unlock()
	for _, item := range managed {
		_ = item.sub.Close()
	}
}

func subscriberParamsFromWallet(row paymentsqlc.PaymentTonWallet) (SubscriberParams, bool, error) {
	if !row.IsEnabled {
		return SubscriberParams{}, false, nil
	}
	network, err := validateNetwork(row.Network)
	if err != nil {
		return SubscriberParams{}, false, err
	}
	workspaceID := row.WorkspaceID
	walletAddress := strings.TrimSpace(row.WalletAddress)
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return SubscriberParams{}, false, err
	}
	if walletAddress == "" {
		return SubscriberParams{}, false, nil
	}
	walletAddress, err = NormalizeWalletAddress(walletAddress, network)
	if err != nil {
		return SubscriberParams{}, false, err
	}
	networkConfigURL := ""
	if row.NetworkConfigUrl.Valid {
		networkConfigURL = strings.TrimSpace(row.NetworkConfigUrl.String)
	}
	if networkConfigURL == "" {
		networkConfigURL = defaultNetworkConfigURL(network)
	}
	return SubscriberParams{
		WorkspaceID:      workspaceID,
		Network:          network,
		NetworkConfigURL: networkConfigURL,
		WalletAddress:    walletAddress,
	}, true, nil
}

func managedSubscriberKey(params SubscriberParams) string {
	return params.WorkspaceID + "\x00" +
		normalizeNetwork(params.Network) + "\x00" +
		strings.TrimSpace(params.WalletAddress)
}

func sameSubscriberParams(left, right SubscriberParams) bool {
	return left.WorkspaceID == right.WorkspaceID &&
		normalizeNetwork(left.Network) == normalizeNetwork(right.Network) &&
		strings.TrimSpace(left.WalletAddress) == strings.TrimSpace(right.WalletAddress) &&
		strings.TrimSpace(left.NetworkConfigURL) == strings.TrimSpace(right.NetworkConfigURL)
}
