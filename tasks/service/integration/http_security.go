package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

func validateHTTPCheckURL(ctx context.Context, value *url.URL, allowPrivate bool) error {

	if value == nil || value.Hostname() == "" || value.User != nil ||
		(value.Scheme != "https" && !(allowPrivate && value.Scheme == "http")) {
		return fmt.Errorf("HTTP check URL must be an absolute HTTPS URL without credentials")
	}

	_, err := resolveHTTPCheckHost(ctx, value.Hostname(), allowPrivate)
	return err

}

func secureHTTPCheckClient(base *http.Client, timeout time.Duration, allowPrivate bool) *http.Client {

	if base == nil {
		base = &http.Client{}
	}

	client := *base
	if timeout > 0 {
		client.Timeout = timeout
	} else if client.Timeout <= 0 {
		client.Timeout = defaultHTTPCheckTimeout
	}

	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {

		if err := validateHTTPCheckURL(request.Context(), request.URL, allowPrivate); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		return nil

	}

	baseTransport := client.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	if transport, ok := baseTransport.(*http.Transport); ok {
		client.Transport = secureHTTPCheckTransport(transport, allowPrivate)
	}

	return &client

}

func secureHTTPCheckTransport(base *http.Transport, allowPrivate bool) *http.Transport {

	transport := base.Clone()
	dial := transport.DialContext
	if dial == nil {
		var dialer net.Dialer
		dial = dialer.DialContext
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {

		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolveHTTPCheckHost(ctx, host, allowPrivate)
		if err != nil {
			return nil, err
		}

		var dialErr error
		for _, resolved := range addresses {
			connection, err := dial(ctx, network, net.JoinHostPort(resolved.String(), port))
			if err == nil {
				return connection, nil
			}
			dialErr = err
		}
		return nil, dialErr

	}

	return transport

}

func resolveHTTPCheckHost(ctx context.Context, host string, allowPrivate bool) ([]netip.Addr, error) {

	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("HTTP check host cannot be resolved")
	}
	if allowPrivate {
		return addresses, nil
	}
	for _, address := range addresses {
		if !isPublicHTTPCheckAddress(address) {
			return nil, fmt.Errorf("HTTP check host resolves to a non-public address")
		}
	}

	return addresses, nil

}

func isPublicHTTPCheckAddress(address netip.Addr) bool {

	address = address.Unmap()
	return address.IsValid() && !address.IsLoopback() && !address.IsPrivate() &&
		!address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() &&
		!address.IsMulticast() && !address.IsUnspecified() &&
		!strings.EqualFold(address.String(), "169.254.169.254")

}
