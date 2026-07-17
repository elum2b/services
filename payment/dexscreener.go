package payment

import (
	"context"
	"errors"
	"fmt"
	json "github.com/goccy/go-json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	serviceerrors "github.com/elum2b/services/errors"
	"github.com/elum2b/services/payment/repository"
)

const (
	maxDexScreenerResponseSize  = 4 << 20
	tonNativeDexScreenerAddress = "EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"
)

type dexScreenerPair struct {
	BaseToken struct {
		Address string `json:"address"`
	} `json:"baseToken"`
	QuoteToken struct {
		Address string `json:"address"`
	} `json:"quoteToken"`
	PriceNative string `json:"priceNative"`
	PriceUSD    string `json:"priceUsd"`
	Liquidity   *struct {
		USD float64 `json:"usd"`
	} `json:"liquidity"`
}

func fetchDexScreenerPrices(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	chainID string,
	updates []repository.DueAssetRateUpdate,
) (map[string]uint64, error) {
	if client == nil {
		return nil, ErrDexScreenerClientRequired
	}
	addresses := make([]string, 0, len(updates))
	seen := make(map[string]struct{}, len(updates))
	requested := make(map[string]struct{}, len(updates))
	priceAddresses := make(map[string]string, len(updates))
	for _, update := range updates {
		assetCode := strings.TrimSpace(update.AssetCode)
		address := strings.TrimSpace(update.SourceTokenAddress)
		priceAddress := dexScreenerPriceTokenAddress(update)
		if assetCode == "" || address == "" || priceAddress == "" {
			continue
		}
		requested[priceAddress] = struct{}{}
		priceAddresses[assetCode] = priceAddress
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	if len(addresses) == 0 {
		return nil, ErrDexScreenerAddressesRequired
	}
	if len(addresses) > 30 {
		return nil, ErrDexScreenerBatchTooLarge
	}

	endpoint := strings.TrimRight(baseURL, "/") +
		"/tokens/v1/" + url.PathEscape(chainID) + "/" + strings.Join(addresses, ",")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return nil, serviceerrors.Wrap(serviceerrors.CodeUnavailable, fmt.Sprintf("payment dexscreener request failed with status %d", response.StatusCode), errors.New(strings.TrimSpace(string(body))))
	}

	var pairs []dexScreenerPair
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxDexScreenerResponseSize))
	if err := decoder.Decode(&pairs); err != nil {
		return nil, err
	}
	selected := selectDexScreenerPrices(pairs, requested)
	result := make(map[string]uint64, len(priceAddresses))
	for assetCode, priceAddress := range priceAddresses {
		if price, ok := selected[priceAddress]; ok {
			result[assetCode] = price
		}
	}
	return result, nil
}

func dexScreenerPriceTokenAddress(update repository.DueAssetRateUpdate) string {
	if update.AssetKind != "crypto_native" {
		return strings.TrimSpace(update.SourceTokenAddress)
	}

	switch strings.ToLower(strings.TrimSpace(update.SourceChainID)) {
	case "ton":
		return tonNativeDexScreenerAddress
	default:
		return ""
	}
}

func selectDexScreenerPrices(
	pairs []dexScreenerPair,
	requested map[string]struct{},
) map[string]uint64 {
	type candidate struct {
		price     uint64
		liquidity float64
	}
	best := make(map[string]candidate, len(requested))
	for _, pair := range pairs {
		address, price, ok := dexScreenerRequestedPriceMinor(pair, requested)
		if !ok {
			continue
		}
		liquidity := float64(0)
		if pair.Liquidity != nil {
			liquidity = pair.Liquidity.USD
		}
		current, exists := best[address]
		if !exists || liquidity > current.liquidity {
			best[address] = candidate{price: price, liquidity: liquidity}
		}
	}

	result := make(map[string]uint64, len(best))
	for address, candidate := range best {
		result[address] = candidate.price
	}
	return result
}

func dexScreenerRequestedPriceMinor(
	pair dexScreenerPair,
	requested map[string]struct{},
) (string, uint64, bool) {
	baseAddress := strings.TrimSpace(pair.BaseToken.Address)
	if _, ok := requested[baseAddress]; ok {
		price, err := usdStringToMinor(pair.PriceUSD)
		return baseAddress, price, err == nil
	}

	quoteAddress := strings.TrimSpace(pair.QuoteToken.Address)
	if _, ok := requested[quoteAddress]; !ok {
		return "", 0, false
	}
	baseUSD, parsed := new(big.Rat).SetString(strings.TrimSpace(pair.PriceUSD))
	if !parsed || baseUSD.Sign() <= 0 {
		return "", 0, false
	}
	baseInQuote, parsed := new(big.Rat).SetString(strings.TrimSpace(pair.PriceNative))
	if !parsed || baseInQuote.Sign() <= 0 {
		return "", 0, false
	}
	quoteUSD := new(big.Rat).Quo(baseUSD, baseInQuote)
	price, err := ratToUSDTMinor(quoteUSD)
	return quoteAddress, price, err == nil
}

func usdStringToMinor(value string) (uint64, error) {
	rat, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok || rat.Sign() <= 0 {
		return 0, ErrUSDPriceInvalid
	}
	return ratToUSDTMinor(rat)
}

func ratToUSDTMinor(value *big.Rat) (uint64, error) {
	if value == nil || value.Sign() <= 0 {
		return 0, ErrUSDPriceInvalid
	}
	scaled := new(big.Rat).Mul(value, big.NewRat(1_000_000, 1))
	minor, remainder := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	if remainder.Sign() > 0 {
		minor.Add(minor, big.NewInt(1))
	}
	if !minor.IsUint64() || minor.Sign() <= 0 {
		return 0, ErrUSDPriceOverflow
	}
	return minor.Uint64(), nil
}
