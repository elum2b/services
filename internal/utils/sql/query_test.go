package sql

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memCache struct {
	data map[string][]byte
}

func (m *memCache) GetWithTTL(key string) ([]byte, time.Duration, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, 0, errors.New("miss")
	}
	return v, 0, nil
}

func (m *memCache) Set(key string, val []byte, _ time.Duration) error {
	if m.data == nil {
		m.data = make(map[string][]byte)
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	m.data[key] = cp
	return nil
}

func (m *memCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func (m *memCache) Reset() error {
	m.data = make(map[string][]byte)
	return nil
}

func (m *memCache) Close() error {
	return nil
}

type expiringMemCache struct {
	data map[string]expiringCacheItem
}

type expiringCacheItem struct {
	payload []byte
	expires time.Time
}

func (m *expiringMemCache) GetWithTTL(key string) ([]byte, time.Duration, error) {
	it, ok := m.data[key]
	if !ok {
		return nil, 0, errors.New("miss")
	}
	if !it.expires.IsZero() && time.Now().After(it.expires) {
		delete(m.data, key)
		return nil, 0, errors.New("miss")
	}

	cp := make([]byte, len(it.payload))
	copy(cp, it.payload)
	if it.expires.IsZero() {
		return cp, 0, nil
	}
	return cp, time.Until(it.expires), nil
}

func (m *expiringMemCache) Set(key string, val []byte, exp time.Duration) error {
	if m.data == nil {
		m.data = make(map[string]expiringCacheItem)
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	item := expiringCacheItem{payload: cp}
	if exp > 0 {
		item.expires = time.Now().Add(exp)
	}
	m.data[key] = item
	return nil
}

func (m *expiringMemCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func (m *expiringMemCache) Reset() error {
	m.data = make(map[string]expiringCacheItem)
	return nil
}

func (m *expiringMemCache) Close() error {
	return nil
}

type testModel struct {
	ID int `msgpack:"id"`
}

type failCodec struct {
	marshalErr   bool
	unmarshalErr bool
}

type spyMutex struct {
	base        *KeyedMutex
	lockCalls   atomic.Int32
	unlockCalls atomic.Int32
}

func (s *spyMutex) Lock(key string) error {
	s.lockCalls.Add(1)
	return s.base.Lock(key)
}

func (s *spyMutex) Unlock(key string) error {
	s.unlockCalls.Add(1)
	return s.base.Unlock(key)
}

func (f failCodec) Marshal(v any) ([]byte, error) {
	if f.marshalErr {
		return nil, errors.New("marshal fail")
	}
	return MsgpackCodec{}.Marshal(v)
}

func (f failCodec) Unmarshal(data []byte, v any) error {
	if f.unmarshalErr {
		return errors.New("unmarshal fail")
	}
	return MsgpackCodec{}.Unmarshal(data, v)
}

func TestQuery_Errors(t *testing.T) {
	_, err := Query[testModel](context.Background(), nil, Params{}, nil)
	if !errors.Is(err, ErrNilDB) {
		t.Fatalf("expected ErrNilDB, got %v", err)
	}

	c := &Client{db: &sql.DB{}, inMemory: newL1Cache(10, time.Minute), codec: MsgpackCodec{}}
	_, err = Query[testModel](context.Background(), c, Params{}, nil)
	if err == nil || err.Error() != "sqlcwrap: loader is nil" {
		t.Fatalf("expected loader nil error, got %v", err)
	}
}

func TestQuery_L1Hit(t *testing.T) {
	c := &Client{
		db:           &sql.DB{},
		inMemory:     newL1Cache(100, time.Minute),
		codec:        MsgpackCodec{},
		CacheEnabled: true,
	}

	calls := 0
	loader := func(ctx context.Context) (testModel, error) {
		calls++
		return testModel{ID: 1}, ctx.Err()
	}

	params := Params{Key: "k1", CacheL1Delay: time.Minute}

	if _, err := Query(context.Background(), c, params, loader); err != nil {
		t.Fatalf("unexpected first call error: %v", err)
	}
	if _, err := Query(context.Background(), c, params, loader); err != nil {
		t.Fatalf("unexpected second call error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected loader call count 1, got %d", calls)
	}
}

func TestQuery_KeyPartsAndL2HitWarmsL1(t *testing.T) {
	cache := &memCache{}
	c := &Client{
		db:           &sql.DB{},
		cache:        cache,
		inMemory:     newL1Cache(100, time.Minute),
		codec:        MsgpackCodec{},
		CacheEnabled: true,
	}

	calls := 0
	loader := func(context.Context) (testModel, error) {
		calls++
		return testModel{ID: 5}, nil
	}

	params := Params{KeyParts: []any{"users", 5}, CacheL2Delay: time.Minute, CacheL1Delay: time.Minute}
	key := CreateKey(params.KeyParts...)

	if _, err := Query(context.Background(), c, params, loader); err != nil {
		t.Fatalf("unexpected first call error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected loader call count 1, got %d", calls)
	}
	if _, ok := c.inMemory.Get(key); !ok {
		t.Fatal("expected L1 warm")
	}

	c.inMemory.Reset()
	if _, err := Query(context.Background(), c, params, loader); err != nil {
		t.Fatalf("unexpected second call error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected no second loader call, got %d", calls)
	}
	if _, ok := c.inMemory.Get(key); !ok {
		t.Fatal("expected L1 warm after L2 hit")
	}
}

func TestQuery_L2DecodeFailFallsBackToLoader(t *testing.T) {
	cache := &memCache{data: map[string][]byte{"bad": {0x01, 0x02}}}
	c := &Client{
		db:           &sql.DB{},
		cache:        cache,
		inMemory:     newL1Cache(100, time.Minute),
		codec:        failCodec{unmarshalErr: true},
		CacheEnabled: true,
	}

	calls := 0
	_, err := Query(context.Background(), c, Params{Key: "bad", CacheL2Delay: time.Minute}, func(context.Context) (testModel, error) {
		calls++
		return testModel{ID: 9}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected loader call count 1, got %d", calls)
	}
}

func TestQuery_L2EncodeFailIsBestEffort(t *testing.T) {
	cache := &memCache{}
	c := &Client{
		db:           &sql.DB{},
		cache:        cache,
		inMemory:     newL1Cache(100, time.Minute),
		codec:        failCodec{marshalErr: true},
		CacheEnabled: true,
	}

	_, err := Query(context.Background(), c, Params{Key: "k", CacheL2Delay: time.Minute}, func(context.Context) (testModel, error) {
		return testModel{ID: 1}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cache.data["k"]; ok {
		t.Fatal("did not expect L2 set when marshal fails")
	}
}

func TestQuery_TimeoutRespectsParent(t *testing.T) {
	c := &Client{
		db:           &sql.DB{},
		inMemory:     newL1Cache(100, time.Minute),
		codec:        MsgpackCodec{},
		CacheEnabled: false,
	}

	_, err := Query(context.Background(), c, Params{Timeout: 15 * time.Millisecond}, func(ctx context.Context) (testModel, error) {
		<-ctx.Done()
		return testModel{}, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	parent, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Query(parent, c, Params{Timeout: time.Second}, func(ctx context.Context) (testModel, error) {
		return testModel{}, ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled, got %v", err)
	}
}

func TestTransaction(t *testing.T) {
	db, stats := openTestDB(t)
	c := &Client{
		db:           db,
		inMemory:     newL1Cache(10, time.Minute),
		codec:        MsgpackCodec{},
		CacheEnabled: false,
	}

	var gotTx bool
	_, err := Transaction(context.Background(), c, Params{}, func(_ context.Context, tx *sql.Tx) (testModel, error) {
		gotTx = tx != nil
		return testModel{ID: 42}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotTx {
		t.Fatal("expected tx to be provided when TxEnable=true")
	}
	if stats.commitCount.Load() != 1 {
		t.Fatalf("expected commit=1, got %d", stats.commitCount.Load())
	}
	if stats.rollbackCount.Load() != 0 {
		t.Fatalf("expected rollback=0, got %d", stats.rollbackCount.Load())
	}
}

func TestTransactionRollbackOnError(t *testing.T) {
	db, stats := openTestDB(t)
	c := &Client{db: db, inMemory: newL1Cache(10, time.Minute), codec: MsgpackCodec{}}

	_, err := Transaction(context.Background(), c, Params{}, func(context.Context, *sql.Tx) (testModel, error) {
		return testModel{}, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if stats.rollbackCount.Load() != 1 {
		t.Fatalf("expected rollback=1, got %d", stats.rollbackCount.Load())
	}
}

func TestTransactionRollbackOnPanic(t *testing.T) {
	db, stats := openTestDB(t)
	c := &Client{db: db, inMemory: newL1Cache(10, time.Minute), codec: MsgpackCodec{}}

	defer func() {
		if p := recover(); p == nil {
			t.Fatal("expected panic to propagate")
		}
		if stats.rollbackCount.Load() != 1 {
			t.Fatalf("expected rollback=1, got %d", stats.rollbackCount.Load())
		}
	}()

	_, _ = Transaction(context.Background(), c, Params{}, func(context.Context, *sql.Tx) (testModel, error) {
		panic("panic")
	})
}

func TestTransactionBeginAndCommitFail(t *testing.T) {
	stats := &txStats{failBegin: true}
	db := openTestDBWithStats(t, stats)
	c := &Client{db: db, inMemory: newL1Cache(10, time.Minute), codec: MsgpackCodec{}}

	_, err := Transaction(context.Background(), c, Params{}, func(context.Context, *sql.Tx) (testModel, error) {
		return testModel{ID: 1}, nil
	})
	if err == nil {
		t.Fatal("expected begin error")
	}

	stats = &txStats{failCommit: true}
	db = openTestDBWithStats(t, stats)
	c = &Client{db: db, inMemory: newL1Cache(10, time.Minute), codec: MsgpackCodec{}}

	_, err = Transaction(context.Background(), c, Params{}, func(context.Context, *sql.Tx) (testModel, error) {
		return testModel{ID: 1}, nil
	})
	if err == nil || err.Error() != "failed to commit tx: commit fail" {
		t.Fatalf("expected wrapped commit error, got %v", err)
	}
}

func TestQuery_StampedeProtection_NodeCacheOnly(t *testing.T) {
	c := &Client{
		db:           &sql.DB{},
		inMemory:     newL1Cache(100, time.Minute),
		codec:        MsgpackCodec{},
		mutex:        NewMutex(),
		CacheEnabled: true,
	}

	var calls atomic.Int32
	loader := func(ctx context.Context) (testModel, error) {
		calls.Add(1)
		time.Sleep(40 * time.Millisecond)
		return testModel{ID: 77}, ctx.Err()
	}

	params := Params{
		Key:          "stampede:key",
		CacheL1Delay: time.Minute,
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	var err1, err2 error
	go func() {
		defer wg.Done()
		<-start
		_, err1 = Query(context.Background(), c, params, loader)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err2 = Query(context.Background(), c, params, loader)
	}()

	close(start)
	wg.Wait()

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: err1=%v err2=%v", err1, err2)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", calls.Load())
	}
}

func TestQuery_UsesProvidedMutex(t *testing.T) {
	spy := &spyMutex{base: NewMutex()}
	c := &Client{
		db:           &sql.DB{},
		inMemory:     newL1Cache(100, time.Minute),
		codec:        MsgpackCodec{},
		mutex:        spy,
		CacheEnabled: true,
	}

	_, err := Query(context.Background(), c, Params{
		Key:          "mkey",
		CacheL1Delay: time.Minute,
	}, func(context.Context) (testModel, error) {
		return testModel{ID: 1}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.lockCalls.Load() != 1 {
		t.Fatalf("expected lock calls=1, got %d", spy.lockCalls.Load())
	}
	if spy.unlockCalls.Load() != 1 {
		t.Fatalf("expected unlock calls=1, got %d", spy.unlockCalls.Load())
	}
}

func TestQuery_L1TTLIsCappedByL2TTL(t *testing.T) {
	c := &Client{
		db:           &sql.DB{},
		cache:        &memCache{},
		inMemory:     newL1Cache(100, time.Minute),
		codec:        MsgpackCodec{},
		CacheEnabled: true,
	}
	t.Cleanup(c.inMemory.Close)

	params := Params{
		Key:          "ttl-cap",
		CacheL1Delay: 7 * time.Minute,
		CacheL2Delay: 5 * time.Minute,
	}

	_, err := Query(context.Background(), c, params, func(context.Context) (testModel, error) {
		return testModel{ID: 1}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.inMemory.mu.RLock()
	e, ok := c.inMemory.items["ttl-cap"]
	c.inMemory.mu.RUnlock()
	if !ok {
		t.Fatal("expected key in L1 cache")
	}

	remaining := time.Until(e.expires)
	if remaining > 5*time.Minute+time.Second {
		t.Fatalf("expected L1 TTL <= L2 TTL, got remaining %s", remaining)
	}
	if remaining < 4*time.Minute {
		t.Fatalf("unexpectedly small remaining ttl: %s", remaining)
	}
}

func TestParams_DeprecatedDelaysFallback(t *testing.T) {
	p := Params{
		NodeCacheDelay: 3 * time.Second,
		CacheDelay:     4 * time.Second,
	}
	if got := p.l1Delay(); got != 3*time.Second {
		t.Fatalf("expected deprecated NodeCacheDelay fallback, got %s", got)
	}
	if got := p.l2Delay(); got != 4*time.Second {
		t.Fatalf("expected deprecated CacheDelay fallback, got %s", got)
	}
}

func TestQuery_L1UsesRemainingL2TTLOnL2Hit(t *testing.T) {
	c := &Client{
		db:           &sql.DB{},
		cache:        &expiringMemCache{},
		inMemory:     newL1Cache(100, time.Millisecond),
		codec:        MsgpackCodec{},
		CacheEnabled: true,
	}
	t.Cleanup(c.inMemory.Close)

	params := Params{
		Key:          "remaining-ttl",
		CacheL1Delay: 7 * time.Second,
		CacheL2Delay: 120 * time.Millisecond,
	}

	loaderCalls := 0
	loader := func(context.Context) (testModel, error) {
		loaderCalls++
		return testModel{ID: 10}, nil
	}

	if _, err := Query(context.Background(), c, params, loader); err != nil {
		t.Fatalf("unexpected first query error: %v", err)
	}
	c.inMemory.Reset()

	time.Sleep(70 * time.Millisecond)

	if _, err := Query(context.Background(), c, params, loader); err != nil {
		t.Fatalf("unexpected second query error: %v", err)
	}
	if loaderCalls != 1 {
		t.Fatalf("expected loader to be called once, got %d", loaderCalls)
	}

	c.inMemory.mu.RLock()
	e, ok := c.inMemory.items["remaining-ttl"]
	c.inMemory.mu.RUnlock()
	if !ok {
		t.Fatal("expected key in L1 cache after L2 hit")
	}

	remaining := time.Until(e.expires)
	if remaining > 70*time.Millisecond {
		t.Fatalf("expected L1 TTL to use remaining L2 TTL, got %s", remaining)
	}
	if remaining <= 0 {
		t.Fatalf("expected positive remaining ttl, got %s", remaining)
	}
}

func TestQuery_L1UsesTTLFromL2Provider(t *testing.T) {
	cache := &expiringMemCache{}
	c := &Client{
		db:           &sql.DB{},
		cache:        cache,
		inMemory:     newL1Cache(100, time.Millisecond),
		codec:        MsgpackCodec{},
		CacheEnabled: true,
	}
	t.Cleanup(c.inMemory.Close)

	data, err := c.codec.Marshal(&testModel{ID: 99})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	_ = cache.Set("ttl-from-provider", data, 90*time.Millisecond)

	calls := 0
	params := Params{
		Key:          "ttl-from-provider",
		CacheL1Delay: 5 * time.Second,
		CacheL2Delay: 5 * time.Second,
	}
	got, err := Query(context.Background(), c, params, func(context.Context) (testModel, error) {
		calls++
		return testModel{ID: 1}, nil
	})
	if err != nil {
		t.Fatalf("unexpected query error: %v", err)
	}
	if got.ID != 99 {
		t.Fatalf("expected value from L2, got %+v", got)
	}
	if calls != 0 {
		t.Fatalf("expected loader not called, got %d", calls)
	}

	c.inMemory.mu.RLock()
	e, ok := c.inMemory.items["ttl-from-provider"]
	c.inMemory.mu.RUnlock()
	if !ok {
		t.Fatal("expected key in L1")
	}
	remaining := time.Until(e.expires)
	if remaining > 150*time.Millisecond {
		t.Fatalf("expected L1 ttl based on provider ttl, got %s", remaining)
	}
}
