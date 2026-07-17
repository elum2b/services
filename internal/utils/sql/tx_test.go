package sql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

type txStats struct {
	beginCount    atomic.Int32
	commitCount   atomic.Int32
	rollbackCount atomic.Int32
	failBegin     bool
	failCommit    bool
}

var driverSeq atomic.Int32

type testDriver struct {
	stats *txStats
}

func (d *testDriver) Open(string) (driver.Conn, error) {
	return &testConn{stats: d.stats}, nil
}

type testConn struct {
	stats *txStats
}

func (c *testConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (c *testConn) Close() error                        { return nil }
func (c *testConn) Begin() (driver.Tx, error) {
	if c.stats.failBegin {
		return nil, errors.New("begin fail")
	}
	c.stats.beginCount.Add(1)
	return &testTx{stats: c.stats}, nil
}
func (c *testConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if c.stats.failBegin {
		return nil, errors.New("begin fail")
	}
	c.stats.beginCount.Add(1)
	return &testTx{stats: c.stats}, nil
}

type testTx struct {
	stats *txStats
}

func (t *testTx) Commit() error {
	if t.stats.failCommit {
		return errors.New("commit fail")
	}
	t.stats.commitCount.Add(1)
	return nil
}

func (t *testTx) Rollback() error {
	t.stats.rollbackCount.Add(1)
	return nil
}

type fakeQueries struct{}

func openTestDB(t *testing.T) (*sql.DB, *txStats) {
	t.Helper()

	stats := &txStats{}
	return openTestDBWithStats(t, stats), stats
}

func openTestDBWithStats(t *testing.T, stats *txStats) *sql.DB {
	t.Helper()

	driverName := fmt.Sprintf("sqlcwrap-test-%s-%d", t.Name(), driverSeq.Add(1))
	sql.Register(driverName, &testDriver{stats: stats})

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestWithTx_Commit(t *testing.T) {
	db, stats := openTestDB(t)

	err := WithTx(context.Background(), db, func(*sql.Tx) *fakeQueries {
		return &fakeQueries{}
	}, func(_ *sql.Tx, _ *fakeQueries) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if stats.beginCount.Load() != 1 {
		t.Fatalf("expected begin=1, got %d", stats.beginCount.Load())
	}
	if stats.commitCount.Load() != 1 {
		t.Fatalf("expected commit=1, got %d", stats.commitCount.Load())
	}
	if stats.rollbackCount.Load() != 0 {
		t.Fatalf("expected rollback=0, got %d", stats.rollbackCount.Load())
	}
}

func TestWithTx_RollbackOnError(t *testing.T) {
	db, stats := openTestDB(t)

	err := WithTx(context.Background(), db, func(*sql.Tx) *fakeQueries {
		return &fakeQueries{}
	}, func(_ *sql.Tx, _ *fakeQueries) error {
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if stats.commitCount.Load() != 0 {
		t.Fatalf("expected commit=0, got %d", stats.commitCount.Load())
	}
	if stats.rollbackCount.Load() != 1 {
		t.Fatalf("expected rollback=1, got %d", stats.rollbackCount.Load())
	}
}

func TestWithTx_RollbackOnPanic(t *testing.T) {
	db, stats := openTestDB(t)

	defer func() {
		if p := recover(); p == nil {
			t.Fatal("expected panic to propagate")
		}
		if stats.commitCount.Load() != 0 {
			t.Fatalf("expected commit=0, got %d", stats.commitCount.Load())
		}
		if stats.rollbackCount.Load() != 1 {
			t.Fatalf("expected rollback=1, got %d", stats.rollbackCount.Load())
		}
	}()

	_ = WithTx(context.Background(), db, func(*sql.Tx) *fakeQueries {
		return &fakeQueries{}
	}, func(_ *sql.Tx, _ *fakeQueries) error {
		panic("panic in tx")
	})
}

func TestWithTx_BeginFail(t *testing.T) {
	stats := &txStats{failBegin: true}
	db := openTestDBWithStats(t, stats)

	err := WithTx(context.Background(), db, func(*sql.Tx) *fakeQueries { return &fakeQueries{} }, func(*sql.Tx, *fakeQueries) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected begin error")
	}
}

func TestWithTx_CommitFail(t *testing.T) {
	stats := &txStats{failCommit: true}
	db := openTestDBWithStats(t, stats)

	err := WithTx(context.Background(), db, func(*sql.Tx) *fakeQueries { return &fakeQueries{} }, func(*sql.Tx, *fakeQueries) error {
		return nil
	})
	if err == nil || err.Error() != "failed to commit tx: commit fail" {
		t.Fatalf("expected wrapped commit error, got %v", err)
	}
}

func TestWithTx_Validation(t *testing.T) {
	db, _ := openTestDB(t)

	if err := WithTx[fakeQueries](context.Background(), nil, func(*sql.Tx) *fakeQueries { return &fakeQueries{} }, func(*sql.Tx, *fakeQueries) error { return nil }); !errors.Is(err, ErrNilDB) {
		t.Fatalf("expected ErrNilDB, got %v", err)
	}
	if err := WithTx[fakeQueries](context.Background(), db, nil, func(*sql.Tx, *fakeQueries) error { return nil }); err == nil {
		t.Fatal("expected newQueries nil error")
	}
	if err := WithTx[fakeQueries](context.Background(), db, func(*sql.Tx) *fakeQueries { return &fakeQueries{} }, nil); err == nil {
		t.Fatal("expected callback nil error")
	}
}

func TestInTx(t *testing.T) {
	db, stats := openTestDB(t)
	c := &Client{db: db}

	if err := c.InTx(context.Background(), func(*sql.Tx) error { return nil }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.commitCount.Load() != 1 {
		t.Fatalf("expected commit=1, got %d", stats.commitCount.Load())
	}
}

func TestInTx_ValidationAndRollback(t *testing.T) {
	c := &Client{}
	if err := c.InTx(context.Background(), func(*sql.Tx) error { return nil }); err == nil {
		t.Fatal("expected nil client/db error")
	}

	db, stats := openTestDB(t)
	c = &Client{db: db}
	if err := c.InTx(context.Background(), nil); err == nil {
		t.Fatal("expected callback nil error")
	}

	err := c.InTx(context.Background(), func(*sql.Tx) error { return errors.New("x") })
	if err == nil {
		t.Fatal("expected callback error")
	}
	if stats.rollbackCount.Load() != 1 {
		t.Fatalf("expected rollback=1, got %d", stats.rollbackCount.Load())
	}
}

func TestInTx_BeginCommitAndPanicPaths(t *testing.T) {
	stats := &txStats{failBegin: true}
	db := openTestDBWithStats(t, stats)
	c := &Client{db: db}
	if err := c.InTx(context.Background(), func(*sql.Tx) error { return nil }); err == nil {
		t.Fatal("expected begin error")
	}

	stats = &txStats{failCommit: true}
	db = openTestDBWithStats(t, stats)
	c = &Client{db: db}
	if err := c.InTx(context.Background(), func(*sql.Tx) error { return nil }); err == nil {
		t.Fatal("expected commit error")
	}

	stats = &txStats{}
	db = openTestDBWithStats(t, stats)
	c = &Client{db: db}

	defer func() {
		if p := recover(); p == nil {
			t.Fatal("expected panic")
		}
		if stats.rollbackCount.Load() != 1 {
			t.Fatalf("expected rollback=1, got %d", stats.rollbackCount.Load())
		}
	}()
	_ = c.InTx(context.Background(), func(*sql.Tx) error {
		panic("panic in InTx")
	})
}
