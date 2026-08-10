package callback

import "testing"

const callbackTestWorkspaceID = "00000000-0000-0000-0000-000000000001"

func TestRouteSchedulerLimitsEachDestinationIndependently(t *testing.T) {
	scheduler := newRouteScheduler()

	for range maxRouteConcurrency {
		if !scheduler.acquire("first") {
			t.Fatal("first route reached its limit too early")
		}
	}

	if scheduler.acquire("first") {
		t.Fatal("first route exceeded its concurrency limit")
	}

	for range maxRouteConcurrency {
		if !scheduler.acquire("second") {
			t.Fatal("second route was blocked by first route")
		}
	}

	if scheduler.total != maxRouteConcurrency*2 {
		t.Fatalf("active callback count = %d", scheduler.total)
	}

	scheduler.release("first")

	if !scheduler.acquire("first") {
		t.Fatal("released route capacity was not restored")
	}
}

func TestCallbackRouteKeyUsesFullApplicationIdentity(t *testing.T) {
	event := storedEvent{}

	event.WorkspaceID = callbackTestWorkspaceID
	event.RoutingKey = callbackTestWorkspaceID + ":10:20"

	if key := callbackRouteKey(event); key !=
		callbackTestWorkspaceID+":10:20" {
		t.Fatalf("callback route key = %q", key)
	}
}

func TestCallbackRouteKeyFallsBackToWorkspace(t *testing.T) {
	event := storedEvent{}

	event.WorkspaceID = callbackTestWorkspaceID

	if key := callbackRouteKey(event); key != event.WorkspaceID {
		t.Fatalf("fallback callback route key = %q", key)
	}
}

func TestCallbackRoutingKeyUsesTrustedWorkspace(t *testing.T) {
	payload := []byte(`{
        "workspace_id":"00000000-0000-0000-0000-000000000099",
        "app_id":10,
        "platform_id":20
    }`)

	key := callbackRoutingKey(callbackTestWorkspaceID, payload)
	if key != callbackTestWorkspaceID+":10:20" {
		t.Fatalf("callback routing key = %q", key)
	}
}

func TestCallbackRoutingKeyFallsBackForMissingApplication(t *testing.T) {
	key := callbackRoutingKey(
		callbackTestWorkspaceID,
		[]byte(`{"event":"workspace scoped"}`),
	)
	if key != callbackTestWorkspaceID {
		t.Fatalf("callback routing key fallback = %q", key)
	}
}

func TestRouteSchedulerReportsOnlySaturatedRoutes(t *testing.T) {
	scheduler := newRouteScheduler()

	for range maxRouteConcurrency {
		if !scheduler.acquire("saturated") {
			t.Fatal("route reached its limit too early")
		}
	}

	for range maxRouteConcurrency - 1 {
		if !scheduler.acquire("available") {
			t.Fatal("available route reached its limit too early")
		}
	}

	routes := scheduler.saturatedRoutes()
	if len(routes) != 1 || routes[0] != "saturated" {
		t.Fatalf("saturated routes = %v", routes)
	}

	scheduler.release("saturated")

	if routes := scheduler.saturatedRoutes(); len(routes) != 0 {
		t.Fatalf("saturated routes after release = %v", routes)
	}
}
