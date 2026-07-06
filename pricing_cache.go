package yuri

import (
	"context"
	"fmt"
	"sync"
	"time"

	"strings"
)

const DefaultPriceCacheTTL = 3 * time.Minute

type cachedPriceProvider struct {
	inner PriceProvider
	ttl   time.Duration

	mu    sync.Mutex
	cache map[priceCacheKey]cachedPrice
}

type priceCacheKey struct {
	currency string
	chain    string
	token    string
}

type cachedPrice struct {
	value  int64
	expiry time.Time
}

func NewCachedPriceProvider(inner PriceProvider) PriceProvider {
	return NewCachedPriceProviderWithTTL(inner, DefaultPriceCacheTTL)
}

func NewCachedPriceProviderWithTTL(inner PriceProvider, ttl time.Duration) PriceProvider {
	if ttl <= 0 {
		ttl = DefaultPriceCacheTTL
	}

	return &cachedPriceProvider{
		inner: inner,
		ttl:   ttl,
		cache: make(map[priceCacheKey]cachedPrice),
	}
}

func (p *cachedPriceProvider) WantsFullChainName() bool {
	return p.inner.WantsFullChainName()
}

func (p *cachedPriceProvider) Get(ctx context.Context, currency Currency, chain string, token Token) (int64, error) {
	if p == nil || p.inner == nil {
		return -1, fmt.Errorf("cached price provider is not initialized")
	}

	key := priceCacheKey{
		currency: stringsKey(currency.Code),
		chain:    stringsKey(chain),
		token:    tokenKey(token),
	}

	now := time.Now()

	p.mu.Lock()
	if item, ok := p.cache[key]; ok && now.Before(item.expiry) {
		value := item.value
		p.mu.Unlock()
		return value, nil
	}
	p.mu.Unlock()

	value, err := p.inner.Get(ctx, currency, chain, token)
	if err != nil {
		return value, err
	}

	p.mu.Lock()
	p.cache[key] = cachedPrice{
		value:  value,
		expiry: now.Add(p.ttl),
	}
	p.mu.Unlock()

	return value, nil
}

func stringsKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func tokenKey(token Token) string {
	if token == (Token{}) {
		return ""
	}

	return stringsKey(token.Symbol) + "|" + stringsKey(token.Contract) + "|" + fmt.Sprint(token.Decimals)
}
