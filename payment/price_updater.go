package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/elum2b/services/payment/repository"
)

const (
	defaultPriceUpdateInterval    = 10 * time.Minute
	defaultPriceUpdateHTTPTimeout = 10 * time.Second
	defaultPriceUpdateLease       = 30 * time.Second
	defaultDexScreenerBaseURL     = "https://api.dexscreener.com"
)

func (a *Payment) startPriceUpdater() {
	if a == nil || a.pricing == nil || a.rootCtx == nil {
		return
	}
	if a.pricingHTTPClient == nil {
		a.pricingHTTPClient = &http.Client{Timeout: defaultPriceUpdateHTTPTimeout}
	}
	if a.pricingInterval <= 0 {
		a.pricingInterval = defaultPriceUpdateInterval
	}
	if strings.TrimSpace(a.pricingBaseURL) == "" {
		a.pricingBaseURL = defaultDexScreenerBaseURL
	}

	workerID := newPriceUpdaterWorkerID()
	a.goroutines.GoRestart(a.rootCtx, "payment.price_updater", time.Second, func() {
		a.priceUpdaterLoop(workerID)
	})
}

func (a *Payment) priceUpdaterLoop(workerID string) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-a.rootCtx.Done():
			return
		case <-timer.C:
			if err := a.runDuePriceUpdates(a.rootCtx, workerID); err != nil &&
				!errors.Is(err, context.Canceled) &&
				!errors.Is(err, context.DeadlineExceeded) {
				log.Printf("payment price updater: %v", err)
			}
			timer.Reset(a.pricingInterval)
		}
	}
}

func (a *Payment) runDuePriceUpdates(ctx context.Context, workerID string) error {
	if _, err := a.pricing.SyncAutomaticAssetRates(ctx); err != nil {
		return err
	}
	updates, err := a.pricing.ClaimDueAssetRateUpdates(ctx, workerID, 300, defaultPriceUpdateLease)
	if err != nil {
		return err
	}
	groups := groupDuePriceUpdates(updates)
	for _, group := range groups {
		if err := a.updateDexScreenerGroup(ctx, workerID, group); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			log.Printf("payment price updater source=%s chain=%s: %v", group.source, group.chainID, err)
		}
	}
	return nil
}

type duePriceUpdateGroup struct {
	source  string
	chainID string
	updates []repository.DueAssetRateUpdate
}

func groupDuePriceUpdates(updates []repository.DueAssetRateUpdate) []duePriceUpdateGroup {
	index := make(map[string]int)
	groups := make([]duePriceUpdateGroup, 0)
	for _, update := range updates {
		key := update.Source + "\x00" + update.SourceChainID
		position, ok := index[key]
		if !ok {
			position = len(groups)
			index[key] = position
			groups = append(groups, duePriceUpdateGroup{
				source:  update.Source,
				chainID: update.SourceChainID,
			})
		}
		groups[position].updates = append(groups[position].updates, update)
	}
	return groups
}

func (a *Payment) updateDexScreenerGroup(
	ctx context.Context,
	workerID string,
	group duePriceUpdateGroup,
) error {
	if group.source != repository.AssetRateSourceDexScreener {
		return a.failPriceUpdateGroup(ctx, workerID, group.updates,
			fmt.Errorf("unsupported price source %q", group.source))
	}

	var groupErrors []error
	for start := 0; start < len(group.updates); start += 30 {
		end := min(start+30, len(group.updates))
		batch := group.updates[start:end]
		prices, err := fetchDexScreenerPrices(
			ctx,
			a.pricingHTTPClient,
			a.pricingBaseURL,
			group.chainID,
			batch,
		)
		if err != nil {
			groupErrors = append(groupErrors, err)
			if failErr := a.failPriceUpdateGroup(ctx, workerID, batch, err); failErr != nil {
				groupErrors = append(groupErrors, failErr)
			}
			continue
		}
		for _, update := range batch {
			price, ok := prices[update.AssetCode]
			if !ok {
				updateErr := fmt.Errorf(
					"dexscreener price not found for asset %s via %s",
					update.AssetCode,
					update.SourceTokenAddress,
				)
				groupErrors = append(groupErrors, updateErr)
				if err := a.pricing.FailAssetRateAutoUpdate(ctx, workerID, update, updateErr); err != nil {
					groupErrors = append(groupErrors, err)
				}
				continue
			}
			_, updateErr := a.pricing.UpdateAssetRate(ctx, repository.AssetRateUpdateParams{
				AssetCode:              update.AssetCode,
				ReferenceAssetCode:     update.ReferenceAssetCode,
				ReferencePerAssetMinor: price,
				Source:                 repository.AssetRateSourceDexScreener,
				ObservedAt:             time.Now().UTC(),
			})
			if updateErr != nil {
				groupErrors = append(groupErrors, updateErr)
				if err := a.pricing.FailAssetRateAutoUpdate(ctx, workerID, update, updateErr); err != nil {
					groupErrors = append(groupErrors, err)
				}
				continue
			}
			if err := a.pricing.CompleteAssetRateAutoUpdate(ctx, workerID, update); err != nil {
				groupErrors = append(groupErrors, err)
			}
		}
	}
	return errors.Join(groupErrors...)
}

func (a *Payment) failPriceUpdateGroup(
	ctx context.Context,
	workerID string,
	updates []repository.DueAssetRateUpdate,
	updateErr error,
) error {
	var result error
	for _, update := range updates {
		result = errors.Join(result, a.pricing.FailAssetRateAutoUpdate(ctx, workerID, update, updateErr))
	}
	return result
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func newPriceUpdaterWorkerID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return "payment-price-" + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("payment-price-%d", time.Now().UnixNano())
}
