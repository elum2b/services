// Package delivery sends callback payloads to an application's configured HTTPS endpoint.
package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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

	services "github.com/elum2b/services"
)

const (
	DefaultTimeout      = 30 * time.Second
	maxResponseBodySize = 64 << 10
)

var (
	ErrDestinationNotFound = errors.New(
		"delivery destination is not configured",
	)
	ErrDestinationDisabled = errors.New("delivery destination is disabled")
	ErrPermanentResponse   = errors.New(
		"delivery endpoint returned a permanent response",
	)
)

type Destination struct {
	WorkspaceID string
	AppID       int64
	PlatformID  int64
}

func (d Destination) Validate() error {
	return (services.Identity{WorkspaceID: d.WorkspaceID, AppID: d.AppID, PlatformID: d.PlatformID, PlatformUserID: "delivery"}).Validate()
}

type Endpoint struct {
	URL       string
	Secret    string
	IsEnabled bool
}

type Resolver interface {
	GetDeliveryEndpoint(context.Context, Destination) (Endpoint, error)
}

type Message struct {
	Destination    Destination
	EventType      string
	IdempotencyKey string
	Payload        []byte
}

type Delivery struct {
	resolver Resolver
	client   *http.Client
}

func New(resolver Resolver) (*Delivery, error) {
	if resolver == nil {
		return nil, errors.New("delivery resolver is required")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	transport.DialContext = secureDial(transport.DialContext)

	return &Delivery{
		resolver: resolver,
		client: &http.Client{
			Timeout:       DefaultTimeout,
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (d *Delivery) Deliver(ctx context.Context, message Message) error {
	if d == nil || d.resolver == nil {
		return errors.New("delivery is not initialized")
	}

	if err := message.Destination.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(message.EventType) == "" ||
		strings.TrimSpace(message.IdempotencyKey) == "" {
		return errors.New(
			"delivery event type and idempotency key are required",
		)
	}

	endpoint, err := d.endpoint(ctx, message.Destination)
	if err != nil {
		return err
	}
	req, err := newRequest(ctx, endpoint, message)
	if err != nil {
		return err
	}
	response, err := d.client.Do(req)
	if err != nil {
		return err
	}
	return validateResponse(response)
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
	if _, err := validateURL(ctx, endpoint.URL); err != nil {
		return Endpoint{}, err
	}
	return endpoint, nil
}

func newRequest(
	ctx context.Context,
	endpoint Endpoint,
	message Message,
) (*http.Request, error) {
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint.URL,
		bytes.NewReader(message.Payload),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Delivery-Event", message.EventType)
	req.Header.Set("X-Delivery-Idempotency-Key", message.IdempotencyKey)
	req.Header.Set("X-Delivery-Timestamp", timestamp)
	req.Header.Set(
		"X-Delivery-Signature",
		sign(
			endpoint.Secret,
			timestamp,
			message.IdempotencyKey,
			message.Payload,
		),
	)
	return req, nil
}
func validateResponse(response *http.Response) error {
	defer response.Body.Close()
	_, _ = io.Copy(
		io.Discard,
		io.LimitReader(response.Body, maxResponseBodySize),
	)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 &&
		response.StatusCode != http.StatusRequestTimeout &&
		response.StatusCode != http.StatusTooManyRequests {
		return fmt.Errorf("%w: %s", ErrPermanentResponse, response.Status)
	}
	return fmt.Errorf("delivery endpoint returned %s", response.Status)
}

func sign(secret, timestamp, key string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write([]byte(key))
	mac.Write([]byte("\n"))
	mac.Write(payload)

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func validateURL(ctx context.Context, raw string) (*url.URL, error) {
	value, err := url.Parse(raw)
	if err != nil || value.Scheme != "https" || value.Hostname() == "" ||
		value.User != nil {
		return nil, errors.New(
			"delivery URL must be an absolute HTTPS URL without credentials",
		)
	}

	addresses, err := net.DefaultResolver.LookupNetIP(
		ctx,
		"ip",
		value.Hostname(),
	)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("delivery host cannot be resolved")
	}

	for _, address := range addresses {
		if !publicAddress(address) {
			return nil, errors.New(
				"delivery host resolves to a non-public address",
			)
		}
	}

	return value, nil
}

func secureDial(
	base func(context.Context, string, string) (net.Conn, error),
) func(context.Context, string, string) (net.Conn, error) {
	if base == nil {
		var dialer net.Dialer

		base = dialer.DialContext
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
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

		return base(ctx, network, address)
	}
}
func publicAddress(value netip.Addr) bool {
	value = value.Unmap()
	return value.IsValid() && !value.IsLoopback() && !value.IsPrivate() &&
		!value.IsLinkLocalUnicast() &&
		!value.IsLinkLocalMulticast() &&
		!value.IsMulticast() &&
		!value.IsUnspecified()
}
