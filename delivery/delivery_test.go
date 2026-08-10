package delivery

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"
)

const (
	testWorkspaceID = "00000000-0000-0000-0000-000000000001"
	testSecret      = "0123456789abcdef0123456789abcdef"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return f(request)
}

type callbackMarker struct {
	status Status
	reason string
}

func (m *callbackMarker) Successful() error {
	m.status = StatusOK

	return nil
}

func (m *callbackMarker) FailedWithError(reason string) error {
	m.status = StatusFailed
	m.reason = reason

	return nil
}

func (m *callbackMarker) CanceledWithReason(reason string) error {
	m.status = StatusCanceled
	m.reason = reason

	return nil
}

type staticResolver struct {
	addresses []netip.Addr
	err       error
}

func (r staticResolver) LookupNetIP(
	context.Context,
	string,
	string,
) ([]netip.Addr, error) {
	return r.addresses, r.err
}

func TestDeliverSignsEnvelopeBody(t *testing.T) {
	fixedTime := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)

	var (
		captured *http.Request
		body     []byte
	)

	service := testDelivery(http.StatusOK, "OK")

	service.now = func() time.Time { return fixedTime }
	service.client.Transport = roundTripperFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		captured = request

		var err error

		body, err = io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}

		return response(http.StatusOK, "OK"), nil
	})

	result, err := service.Deliver(t.Context(), validMessage())
	if err != nil || result.Status != StatusOK ||
		result.HTTPStatus != http.StatusOK {
		t.Fatalf("deliver result = %#v, error = %v", result, err)
	}

	if captured.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", captured.Header.Get("Content-Type"))
	}

	signature := captured.Header.Get(SignatureHeader)
	if !VerifySignature(testSecret, body, signature) {
		t.Fatalf("signature %q does not authenticate the envelope", signature)
	}

	if VerifySignature(
		testSecret,
		append(append([]byte(nil), body...), 'x'),
		signature,
	) {
		t.Fatal("signature authenticated a modified envelope")
	}

	for _, header := range []string{
		"X-Delivery-Event",
		"X-Delivery-Idempotency-Key",
		"X-Delivery-Timestamp",
		"X-Workspace-ID",
	} {
		if value := captured.Header.Get(header); value != "" {
			t.Fatalf("unexpected duplicated header %s = %q", header, value)
		}
	}

	var envelope Envelope

	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	message := validMessage()
	if envelope.Version != envelopeVersion || envelope.SentAt != fixedTime ||
		envelope.WorkspaceID != message.Destination.WorkspaceID ||
		envelope.AppID != message.Destination.AppID ||
		envelope.PlatformID != message.Destination.PlatformID ||
		envelope.EventType != message.EventType ||
		envelope.IdempotencyKey != message.IdempotencyKey ||
		string(envelope.Payload) != string(message.Payload) {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestDeliveryResponseContract(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		status     Status
		target     error
	}{
		{"ok", http.StatusOK, "OK", StatusOK, nil},
		{"failed", http.StatusOK, "FAILED", StatusFailed, ErrRetryableResponse},
		{
			"canceled",
			http.StatusOK,
			"CANCELED",
			StatusCanceled,
			ErrPermanentResponse,
		},
		{
			"unknown",
			http.StatusOK,
			"accepted",
			StatusFailed,
			ErrInvalidResponse,
		},
		{
			"created is not success",
			http.StatusCreated,
			"OK",
			StatusFailed,
			ErrRetryableResponse,
		},
		{
			"request timeout",
			http.StatusRequestTimeout,
			"",
			StatusFailed,
			ErrRetryableResponse,
		},
		{
			"too early",
			http.StatusTooEarly,
			"",
			StatusFailed,
			ErrRetryableResponse,
		},
		{
			"rate limited",
			http.StatusTooManyRequests,
			"",
			StatusFailed,
			ErrRetryableResponse,
		},
		{
			"server error",
			http.StatusBadGateway,
			"",
			StatusFailed,
			ErrRetryableResponse,
		},
		{
			"bad request",
			http.StatusBadRequest,
			"",
			StatusFailed,
			ErrRetryableResponse,
		},
		{
			"unauthorized",
			http.StatusUnauthorized,
			"",
			StatusFailed,
			ErrRetryableResponse,
		},
		{
			"not found",
			http.StatusNotFound,
			"",
			StatusFailed,
			ErrRetryableResponse,
		},
		{
			"unprocessable",
			http.StatusUnprocessableEntity,
			"",
			StatusFailed,
			ErrRetryableResponse,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpointResponse := response(test.statusCode, test.body)

			t.Cleanup(func() { _ = endpointResponse.Body.Close() })

			result, err := validateResponse(endpointResponse)
			if result.Status != test.status ||
				result.HTTPStatus != test.statusCode {
				t.Fatalf("result = %#v", result)
			}

			if test.target == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if test.target != nil && !errors.Is(err, test.target) {
				t.Fatalf("error = %v, want %v", err, test.target)
			}
		})
	}
}

func TestDeliverCallbackMapsFinalAndRetryableResults(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		status     Status
	}{
		{"ok", http.StatusOK, "OK", StatusOK},
		{"failed body", http.StatusOK, "FAILED", StatusFailed},
		{"canceled body", http.StatusOK, "CANCELED", StatusCanceled},
		{"timeout status", http.StatusRequestTimeout, "", StatusFailed},
		{"client error", http.StatusForbidden, "", StatusFailed},
		{"server error", http.StatusServiceUnavailable, "", StatusFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			marker := &callbackMarker{}
			service := testDelivery(test.statusCode, test.body)

			if err := service.DeliverCallback(
				t.Context(),
				marker,
				validMessage(),
			); err != nil {
				t.Fatalf("deliver callback: %v", err)
			}

			if marker.status != test.status {
				t.Fatalf(
					"marker status = %q, want %q",
					marker.status,
					test.status,
				)
			}

			if marker.status != StatusOK && marker.reason == "" {
				t.Fatal("failure marker has no reason")
			}
		})
	}
}

func TestDeliverCallbackRetriesNetworkAndTimeoutErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"network", errors.New("connection refused")},
		{"timeout", context.DeadlineExceeded},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := testDelivery(http.StatusOK, "OK")

			service.client.Transport = roundTripperFunc(func(
				*http.Request,
			) (*http.Response, error) {
				return nil, test.err
			})

			marker := &callbackMarker{}

			if err := service.DeliverCallback(
				t.Context(),
				marker,
				validMessage(),
			); err != nil {
				t.Fatalf("deliver callback: %v", err)
			}

			if marker.status != StatusFailed || marker.reason == "" {
				t.Fatalf("marker = %#v", marker)
			}
		})
	}
}

func TestDeliveryRejectsOversizedResponse(t *testing.T) {
	endpointResponse := response(
		http.StatusOK,
		strings.Repeat("x", maxResponseBodySize+1),
	)

	t.Cleanup(func() { _ = endpointResponse.Body.Close() })

	result, err := validateResponse(endpointResponse)
	if result.Status != StatusFailed || !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestDeliveryValidationLimits(t *testing.T) {
	valid := validMessage()

	for _, size := range []int{31, 257} {
		service := testDelivery(http.StatusOK, "OK")

		service.resolver = ResolverFunc(func(
			context.Context,
			Destination,
		) (Endpoint, error) {
			return Endpoint{
				URL:       "https://callback.example/events",
				Secret:    strings.Repeat("s", size),
				IsEnabled: true,
			}, nil
		})

		result, err := service.Deliver(t.Context(), valid)
		if err == nil || result.Status != StatusFailed {
			t.Fatalf(
				"secret size %d result = %#v, error = %v",
				size,
				result,
				err,
			)
		}
	}

	invalidMessages := []Message{
		{
			Destination:    valid.Destination,
			EventType:      "bad event",
			IdempotencyKey: "key",
			Payload:        []byte(`{}`),
		},
		{
			Destination:    valid.Destination,
			EventType:      "event",
			IdempotencyKey: "",
			Payload:        []byte(`{}`),
		},
		{
			Destination:    valid.Destination,
			EventType:      "event",
			IdempotencyKey: "key",
			Payload:        []byte(`invalid`),
		},
		{
			Destination:    valid.Destination,
			EventType:      "event",
			IdempotencyKey: "key",
			Payload:        make([]byte, MaxPayloadSize+1),
		},
	}

	for index, message := range invalidMessages {
		result, err := testDelivery(
			http.StatusOK,
			"OK",
		).Deliver(t.Context(), message)
		if err == nil || result.Status != StatusFailed {
			t.Fatalf(
				"invalid message %d result = %#v, error = %v",
				index,
				result,
				err,
			)
		}
	}
}

func TestDeliveryTimeoutCoversResolver(t *testing.T) {
	service := testDelivery(http.StatusOK, "OK")

	service.timeout = 20 * time.Millisecond
	service.resolver = ResolverFunc(func(
		ctx context.Context,
		_ Destination,
	) (Endpoint, error) {
		<-ctx.Done()

		return Endpoint{}, ctx.Err()
	})

	started := time.Now()
	result, err := service.Deliver(t.Context(), validMessage())

	if !errors.Is(err, context.DeadlineExceeded) ||
		result.Status != StatusFailed {
		t.Fatalf("result = %#v, error = %v", result, err)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("resolver timeout took %s", elapsed)
	}
}

func TestNewUsesSystemHTTPPolicy(t *testing.T) {
	service, err := New(ResolverFunc(func(
		context.Context,
		Destination,
	) (Endpoint, error) {
		return Endpoint{}, nil
	}))
	if err != nil {
		t.Fatalf("new delivery: %v", err)
	}

	transport, ok := service.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", service.client.Transport)
	}

	if service.timeout != DefaultTimeout ||
		service.client.Timeout != DefaultTimeout ||
		transport.Proxy != nil ||
		!transport.DisableCompression ||
		transport.TLSClientConfig == nil ||
		transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Fatalf("unsafe HTTP policy: %#v", service.client)
	}

	if err := service.client.CheckRedirect(
		&http.Request{},
		nil,
	); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy error = %v", err)
	}
}

func TestSecureDialRejectsReservedAddressesAndPinsDNS(t *testing.T) {
	private := netip.MustParseAddr("127.0.0.1")
	public := netip.MustParseAddr("1.1.1.1")
	called := ""
	sentinel := errors.New("dial stopped")
	base := func(
		_ context.Context,
		_, address string,
	) (net.Conn, error) {
		called = address

		return nil, sentinel
	}

	dial := secureDial(base, staticResolver{addresses: []netip.Addr{public}})
	_, err := dial(t.Context(), "tcp", "callback.example:443")

	if !errors.Is(err, sentinel) || called != "1.1.1.1:443" {
		t.Fatalf("pinned dial address = %q, error = %v", called, err)
	}

	called = ""
	dial = secureDial(
		base,
		staticResolver{addresses: []netip.Addr{public, private}},
	)

	if _, err := dial(t.Context(), "tcp", "callback.example:443"); err == nil {
		t.Fatal("mixed public/private DNS response was accepted")
	}

	if called != "" {
		t.Fatalf(
			"dial was attempted before all addresses were checked: %q",
			called,
		)
	}
}

func TestPublicAddressBlacklist(t *testing.T) {
	blocked := []string{
		"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1",
		"169.254.169.254", "192.0.2.1", "198.18.0.1", "203.0.113.1",
		"224.0.0.1", "240.0.0.1", "::", "::1", "fc00::1", "fe80::1",
		"64:ff9b:1::1", "100::1", "2001::1", "2001:db8::1",
		"2002:0a00:1::1", "3fff::1", "fec0::1", "ff02::1",
	}

	for _, raw := range blocked {
		if publicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("reserved address %s was accepted", raw)
		}
	}

	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("public address %s was rejected", raw)
		}
	}
}

func testDelivery(statusCode int, body string) *Delivery {
	return &Delivery{
		resolver: ResolverFunc(func(
			context.Context,
			Destination,
		) (Endpoint, error) {
			return Endpoint{
				URL:       "https://callback.example/events",
				Secret:    testSecret,
				IsEnabled: true,
			}, nil
		}),
		client: &http.Client{
			Transport: roundTripperFunc(func(
				*http.Request,
			) (*http.Response, error) {
				return response(statusCode, body), nil
			}),
		},
		timeout: time.Second,
		now:     time.Now,
	}
}

func validMessage() Message {
	return Message{
		Destination: Destination{
			WorkspaceID: testWorkspaceID,
			AppID:       10,
			PlatformID:  20,
		},
		EventType:      "payment.fulfilled",
		IdempotencyKey: "payment:123",
		Payload:        []byte(`{"amount":100}`),
	}
}

func response(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
