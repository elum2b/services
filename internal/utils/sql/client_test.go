package sql

import (
	"context"
	"testing"
	"time"
)

func TestClient_DBAndClose(t *testing.T) {
	db, _ := openTestDB(t)
	c := &Client{
		db:       db,
		cache:    &memCache{},
		inMemory: newL1Cache(10, time.Minute),
	}

	if c.DB() == nil {
		t.Fatal("expected DB() not nil")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	var nilClient *Client
	if nilClient.DB() != nil {
		t.Fatal("expected nil DB for nil client")
	}
	if err := nilClient.Close(); err != nil {
		t.Fatalf("unexpected nil client close error: %v", err)
	}

	onlyCacheClient := &Client{cache: &memCache{}}
	if err := onlyCacheClient.Close(); err != nil {
		t.Fatalf("unexpected close error for cache-only client: %v", err)
	}
}

func TestCreateContextWithTimeout_DefaultAndParent(t *testing.T) {
	ctx, cancel := createContextWithTimeout(context.Background(), 0)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected deadline for default timeout")
	}

	parent, parentCancel := context.WithCancel(context.Background())
	parentCancel()

	child, childCancel := createContextWithTimeout(parent, time.Second)
	defer childCancel()
	select {
	case <-child.Done():
	default:
		t.Fatal("expected child canceled with parent")
	}
}
