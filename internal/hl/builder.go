// HIP-3 builder perp DEX trading support (hip3-trade, docs/CAPABILITY-MAP.md).
//
// The SDK resolves coin names to asset IDs through the meta snapshot its
// Exchange was built with (Info.CoinToAsset); the default exchange knows the
// main DEX only, so builder orders/cancels ("xyz:CL", asset 110011) fail with
// "coin not found in info". The fix routes such calls through an Exchange
// built from that DEX's meta with ExchangeOptPerpDex, which maps dex-prefixed
// names to builder asset IDs (100000 + perpDexIndex*10000 + universeIndex).
// All signing and wire formatting stays inside the SDK (locked decision);
// this file only chooses which Exchange instance signs.

package hl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sonirico/go-hyperliquid"
)

// BuilderPerpAssetBase is the HIP-3 builder asset-ID base:
// asset = BuilderPerpAssetBase + perpDexIndex*10000 + universeIndex.
// perpDexIndex 0 is the (null) main-DEX slot — main-DEX assets keep bare
// universe indices — so real builder IDs start at 110000.
const BuilderPerpAssetBase = 100000

// IsBuilderAssetID reports whether asset is in the HIP-3 builder ID range
// rather than a main-DEX universe index.
func IsBuilderAssetID(asset int64) bool { return asset >= BuilderPerpAssetBase }

// ResolveAsset validates an asset parameter against fresh meta and returns
// the coin name plus an Exchange scoped to the asset's perp DEX. Main-DEX
// indices go through CoinForAsset and the default exchange, unchanged;
// builder IDs resolve via a fresh perpDexs + dex-meta fetch (same freshness
// posture as D-5).
func (c *Client) ResolveAsset(ctx context.Context, asset int64) (string, *hyperliquid.Exchange, error) {
	if !IsBuilderAssetID(asset) {
		coin, err := c.CoinForAsset(ctx, asset)
		if err != nil {
			return "", nil, err
		}
		return coin, c.Exchange, nil
	}

	perpDexIndex := (asset - BuilderPerpAssetBase) / 10000
	universeIndex := asset - BuilderPerpAssetBase - perpDexIndex*10000
	if perpDexIndex < 1 {
		return "", nil, fmt.Errorf(
			"invalid asset index %d: IDs %d-%d are the unassigned main-DEX slot; builder DEX asset IDs start at %d",
			asset, BuilderPerpAssetBase, BuilderPerpAssetBase+9999, BuilderPerpAssetBase+10000)
	}

	dexs, err := c.Info.PerpDexs(ctx)
	if err != nil {
		return "", nil, err
	}
	if perpDexIndex >= int64(len(dexs)) {
		return "", nil, fmt.Errorf(
			"invalid asset index %d: no perp dex at index %d (perpDexs has %d entries)",
			asset, perpDexIndex, len(dexs))
	}
	var pd hyperliquid.PerpDex
	if err := dexs[perpDexIndex].Parse(&pd); err != nil || pd.Name == "" {
		return "", nil, fmt.Errorf("invalid asset index %d: perp dex at index %d is not deployed", asset, perpDexIndex)
	}

	meta, err := c.Info.Meta(ctx, pd.Name)
	if err != nil {
		return "", nil, err
	}
	if universeIndex >= int64(len(meta.Universe)) {
		return "", nil, fmt.Errorf(
			"invalid asset index %d: %s dex meta universe has %d assets",
			asset, pd.Name, len(meta.Universe))
	}

	ex, err := c.builderExchange(ctx, pd.Name, meta, &dexs)
	if err != nil {
		return "", nil, err
	}
	return meta.Universe[universeIndex].Name, ex, nil
}

// ExchangeForCoin routes a coin name to the right exchange: HIP-3 builder
// coins carry the dex prefix in their meta names ("xyz:CL" → the xyz
// exchange); anything else uses the main-DEX exchange, unchanged.
func (c *Client) ExchangeForCoin(ctx context.Context, coin string) (*hyperliquid.Exchange, error) {
	if dex, _, ok := strings.Cut(coin, ":"); ok && dex != "" {
		return c.ExchangeForDex(ctx, dex)
	}
	return c.Exchange, nil
}

// ExchangeForDex returns the Exchange for a perp DEX name: the default
// exchange for "" (main DEX), otherwise a builder-scoped exchange.
func (c *Client) ExchangeForDex(ctx context.Context, dex string) (*hyperliquid.Exchange, error) {
	if dex == "" {
		return c.Exchange, nil
	}
	dexs, err := c.Info.PerpDexs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range dexs {
		var pd hyperliquid.PerpDex
		if err := dexs[i].Parse(&pd); err != nil || pd.Name == "" {
			continue
		}
		if pd.Name == dex {
			meta, err := c.Info.Meta(ctx, dex)
			if err != nil {
				return nil, err
			}
			return c.builderExchange(ctx, dex, meta, &dexs)
		}
	}
	// Same wording as the hip3-read resolver (tools/hip3.go).
	return nil, fmt.Errorf("unknown perp dex %q (not in perpDexs)", dex)
}

// builderExchange constructs an Exchange scoped to one builder perp DEX from
// an already-fetched dex meta and perpDexs list, so construction costs no
// extra round trips. Panics from the SDK constructor (it fetches eagerly when
// passed nils) are recovered into errors, mirroring New. A synthetic empty
// SpotMeta satisfies the SDK's eager spotMeta load — the scoped exchange
// never trades spot, so its empty spot coin map is never consulted.
func (c *Client) builderExchange(
	ctx context.Context,
	dexName string,
	meta *hyperliquid.Meta,
	dexs *hyperliquid.MixedArray,
) (ex *hyperliquid.Exchange, err error) {
	defer func() {
		if r := recover(); r != nil {
			ex = nil
			err = fmt.Errorf("failed to initialize %s dex exchange: %v", dexName, r)
		}
	}()
	ex = hyperliquid.NewExchange(
		ctx, c.key, c.BaseURL, meta, c.VaultAddress, c.AccountAddress,
		&hyperliquid.SpotMeta{}, dexs,
		hyperliquid.ExchangeOptPerpDex(dexName),
		hyperliquid.ExchangeOptClientOptions(c.clientOpt),
		hyperliquid.ExchangeOptInfoOptions(hyperliquid.InfoOptClientOptions(c.clientOpt)),
	)
	// Builder exchanges are built per call, so their nonce counters start
	// cold; seed from the process-wide floor to keep nonces strictly
	// increasing across instances (the API rejects reused nonces). Each
	// scoped exchange signs exactly one action, so the seed+1 allocation
	// cannot collide.
	ex.SetLastNonce(c.nextNonceSeed())
	return ex, nil
}

// nextNonceSeed allocates a process-wide nonce floor for a freshly built
// builder exchange (mirrors the SDK's nextNonce: max(now, last+1)).
func (c *Client) nextNonceSeed() int64 {
	for {
		last := c.lastNonce.Load()
		cand := time.Now().UnixMilli()
		if cand <= last {
			cand = last + 1
		}
		if c.lastNonce.CompareAndSwap(last, cand) {
			return cand
		}
	}
}
