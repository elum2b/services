// Package delivery sends signed callback envelopes to configured HTTPS endpoints.
package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
)

const (
	DefaultTimeout       = 30 * time.Second
	MaxPayloadSize       = 1 << 20
	SignatureHeader      = "X-Delivery-Signature"
	maxEnvelopeSize      = MaxPayloadSize + (4 << 10)
	maxResponseBodySize  = 64
	maxEventTypeLength   = 128
	maxIdempotencyLength = 191
	envelopeVersion      = 1
)

var (
	ErrDestinationNotFound = errors.New(
		"delivery destination is not configured",
	)
	ErrDestinationDisabled = errors.New("delivery destination is disabled")
	ErrInvalidMessage      = errors.New("delivery message is invalid")
	ErrInvalidResponse     = errors.New(
		"delivery endpoint returned an invalid response",
	)
	ErrPayloadTooLarge   = errors.New("delivery payload is too large")
	ErrRetryableResponse = errors.New(
		"delivery endpoint requested a retry",
	)
	ErrPermanentResponse = errors.New(
		"delivery endpoint canceled delivery",
	)
)

type Status string

const (
	StatusOK       Status = "OK"
	StatusFailed   Status = "FAILED"
	StatusCanceled Status = "CANCELED"
)

type Destination struct {
	WorkspaceID string `json:"workspace_id"`
	AppID       int64  `json:"app_id"`
	PlatformID  int64  `json:"platform_id"`
}

func (d Destination) Validate() error {
	if err := services.ValidateWorkspaceID(d.WorkspaceID); err != nil {
		return err
	}

	if d.AppID <= 0 || d.PlatformID <= 0 {
		return fmt.Errorf(
			"%w: app and platform IDs must be positive",
			ErrInvalidMessage,
		)
	}

	return nil
}

type Endpoint struct {
	URL       string
	Secret    string
	IsEnabled bool
}

type Resolver interface {
	GetDeliveryEndpoint(context.Context, Destination) (Endpoint, error)
}

type ResolverFunc func(context.Context, Destination) (Endpoint, error)

func (f ResolverFunc) GetDeliveryEndpoint(
	ctx context.Context,
	destination Destination,
) (Endpoint, error) {
	return f(ctx, destination)
}

type Message struct {
	Destination    Destination
	EventType      string
	IdempotencyKey string
	Payload        []byte
}

type Envelope struct {
	Version        uint8           `json:"version"`
	WorkspaceID    string          `json:"workspace_id"`
	AppID          int64           `json:"app_id"`
	PlatformID     int64           `json:"platform_id"`
	EventType      string          `json:"event_type"`
	IdempotencyKey string          `json:"idempotency_key"`
	SentAt         time.Time       `json:"sent_at"`
	Payload        json.RawMessage `json:"payload"`
}

type Result struct {
	Status     Status
	HTTPStatus int
}

type CallbackMarker interface {
	Successful() error
	FailedWithError(string) error
	CanceledWithReason(string) error
}

type Delivery struct {
	resolver Resolver
	client   *http.Client
	timeout  time.Duration
	now      func() time.Time
}

func New(resolver Resolver) (*Delivery, error) {
	if resolver == nil {
		return nil, errors.New("delivery resolver is required")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	transport.Proxy = nil
	transport.DisableCompression = true
	transport.DialContext = secureDial(
		transport.DialContext,
		net.DefaultResolver,
	)

	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.MinVersion = tls.VersionTLS12
	}

	return &Delivery{
		resolver: resolver,
		client: &http.Client{
			Timeout:       DefaultTimeout,
			Transport:     transport,
			CheckRedirect: rejectRedirect,
		},
		timeout: DefaultTimeout,
		now:     time.Now,
	}, nil
}

func (d *Delivery) Deliver(
	ctx context.Context,
	message Message,
) (Result, error) {
	if d == nil || d.resolver == nil || d.client == nil {
		return Result{Status: StatusFailed}, errors.New(
			"delivery is not initialized",
		)
	}

	if ctx == nil {
		return Result{Status: StatusFailed}, errors.New(
			"delivery context is required",
		)
	}

	timeout := d.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	deliveryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := validateMessage(message); err != nil {
		return Result{Status: StatusFailed}, err
	}

	endpoint, err := d.endpoint(deliveryCtx, message.Destination)
	if err != nil {
		return Result{Status: StatusFailed}, err
	}

	now := time.Now
	if d.now != nil {
		now = d.now
	}

	body, err := marshalEnvelope(message, now().UTC())
	if err != nil {
		return Result{Status: StatusFailed}, err
	}

	req, err := newRequest(
		deliveryCtx,
		endpoint.URL,
		body,
		sign(endpoint.Secret, body),
	)
	if err != nil {
		return Result{Status: StatusFailed}, err
	}

	response, err := d.client.Do(req)
	if err != nil {
		return Result{Status: StatusFailed}, err
	}

	return validateResponse(response)
}

func (d *Delivery) DeliverCallback(
	ctx context.Context,
	marker CallbackMarker,
	message Message,
) error {
	if marker == nil {
		return errors.New("delivery callback marker is required")
	}

	result, err := d.Deliver(ctx, message)
	switch result.Status {
	case StatusOK:
		return marker.Successful()
	case StatusCanceled:
		return marker.CanceledWithReason(deliveryReason(err, StatusCanceled))
	default:
		return marker.FailedWithError(deliveryReason(err, StatusFailed))
	}
}

func (d *Delivery) endpoint(
	ctx context.Context,
	destination Destination,
) (Endpoint, error) {
	endpoint, err := d.resolver.GetDeliveryEndpoint(ctx, destination)
	if err != nil {
		return Endpoint{}, err
	}

	if !endpoint.IsEnabled {
		return Endpoint{}, ErrDestinationDisabled
	}

	value, err := validateURL(endpoint.URL)
	if err != nil {
		return Endpoint{}, err
	}

	if err := validateSecret(endpoint.Secret); err != nil {
		return Endpoint{}, err
	}

	endpoint.URL = value.String()

	return endpoint, nil
}

func validateMessage(message Message) error {
	if err := message.Destination.Validate(); err != nil {
		return err
	}

	if err := validateText(
		message.EventType,
		maxEventTypeLength,
		"event type",
	); err != nil {
		return err
	}

	if err := validateText(
		message.IdempotencyKey,
		maxIdempotencyLength,
		"idempotency key",
	); err != nil {
		return err
	}

	if len(message.Payload) > MaxPayloadSize {
		return ErrPayloadTooLarge
	}

	if !json.Valid(message.Payload) {
		return fmt.Errorf("%w: payload must be valid JSON", ErrInvalidMessage)
	}

	return nil
}

func validateText(value string, limit int, field string) error {
	if value == "" || len(value) > limit || strings.TrimSpace(value) != value {
		return fmt.Errorf("%w: invalid %s", ErrInvalidMessage, field)
	}

	for _, char := range value {
		if char < 0x21 || char == 0x7f {
			return fmt.Errorf("%w: invalid %s", ErrInvalidMessage, field)
		}
	}

	return nil
}

func validateSecret(secret string) error {
	secretLength := len([]byte(secret))
	if secretLength < 32 || secretLength > 256 {
		return fmt.Errorf(
			"%w: secret must contain from 32 to 256 bytes",
			ErrInvalidMessage,
		)
	}

	return nil
}

func marshalEnvelope(message Message, sentAt time.Time) ([]byte, error) {
	body, err := json.Marshal(Envelope{
		Version:        envelopeVersion,
		WorkspaceID:    message.Destination.WorkspaceID,
		AppID:          message.Destination.AppID,
		PlatformID:     message.Destination.PlatformID,
		EventType:      message.EventType,
		IdempotencyKey: message.IdempotencyKey,
		SentAt:         sentAt,
		Payload:        json.RawMessage(message.Payload),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal delivery envelope: %w", err)
	}

	if len(body) > maxEnvelopeSize {
		return nil, ErrPayloadTooLarge
	}

	return body, nil
}

func newRequest(
	ctx context.Context,
	endpointURL string,
	body []byte,
	signature string,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpointURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SignatureHeader, signature)

	return req, nil
}

func validateResponse(response *http.Response) (Result, error) {
	if response == nil || response.Body == nil {
		return Result{Status: StatusFailed}, ErrInvalidResponse
	}

	defer response.Body.Close()

	body, err := io.ReadAll(
		io.LimitReader(response.Body, maxResponseBodySize+1),
	)
	if err != nil {
		return Result{
			Status:     StatusFailed,
			HTTPStatus: response.StatusCode,
		}, err
	}

	if response.StatusCode == http.StatusOK {
		if len(body) > maxResponseBodySize {
			return Result{
					Status:     StatusFailed,
					HTTPStatus: response.StatusCode,
				},
				ErrInvalidResponse
		}

		status := Status(strings.TrimSpace(string(body)))
		switch status {
		case StatusOK:
			return Result{
				Status:     StatusOK,
				HTTPStatus: response.StatusCode,
			}, nil
		case StatusFailed:
			return Result{
					Status:     StatusFailed,
					HTTPStatus: response.StatusCode,
				},
				ErrRetryableResponse
		case StatusCanceled:
			return Result{
					Status:     StatusCanceled,
					HTTPStatus: response.StatusCode,
				},
				ErrPermanentResponse
		default:
			return Result{
					Status:     StatusFailed,
					HTTPStatus: response.StatusCode,
				},
				ErrInvalidResponse
		}
	}

	return Result{
		Status:     StatusFailed,
		HTTPStatus: response.StatusCode,
	}, fmt.Errorf("%w: %s", ErrRetryableResponse, response.Status)
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))

	_, _ = mac.Write(body)

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(secret string, body []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	want, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))

	_, _ = mac.Write(body)

	return hmac.Equal(mac.Sum(nil), want)
}

func validateURL(raw string) (*url.URL, error) {
	value, err := url.Parse(raw)
	if err != nil || value.Scheme != "https" || value.Hostname() == "" ||
		value.User != nil || value.Fragment != "" || value.Opaque != "" {
		return nil, errors.New(
			"delivery URL must be an absolute HTTPS URL without credentials or fragment",
		)
	}

	return value, nil
}

type netIPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func secureDial(
	base func(context.Context, string, string) (net.Conn, error),
	resolver netIPResolver,
) func(context.Context, string, string) (net.Conn, error) {
	if base == nil {
		var dialer net.Dialer

		base = dialer.DialContext
	}

	if resolver == nil {
		resolver = net.DefaultResolver
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		addresses, err := resolveAddresses(ctx, resolver, host)
		if err != nil {
			return nil, err
		}

		for _, ip := range addresses {
			if !publicAddress(ip) {
				return nil, errors.New(
					"delivery host resolves to a non-public address",
				)
			}
		}

		var dialErrors []error

		for _, ip := range addresses {
			connection, dialErr := base(
				ctx,
				network,
				net.JoinHostPort(ip.String(), port),
			)
			if dialErr == nil {
				return connection, nil
			}

			dialErrors = append(dialErrors, dialErr)
		}

		return nil, errors.Join(dialErrors...)
	}
}

func resolveAddresses(
	ctx context.Context,
	resolver netIPResolver,
	host string,
) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address}, nil
	}

	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("delivery host cannot be resolved")
	}

	return addresses, nil
}

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("fec0::/10"),
}

func publicAddress(value netip.Addr) bool {
	value = value.Unmap()
	if !value.IsValid() || !value.IsGlobalUnicast() || value.IsLoopback() ||
		value.IsPrivate() || value.IsLinkLocalUnicast() ||
		value.IsLinkLocalMulticast() || value.IsMulticast() ||
		value.IsUnspecified() {
		return false
	}

	for _, prefix := range reservedPrefixes {
		if prefix.Contains(value) {
			return false
		}
	}

	return true
}

func rejectRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func deliveryReason(err error, fallback Status) string {
	if err == nil {
		return string(fallback)
	}

	return err.Error()
}
