package runtime

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

func validatePartnerURL(
	ctx context.Context,
	value *url.URL,
	allowPrivate bool,
) error {

	if value == nil || value.Hostname() == "" || value.User != nil ||
		(value.Scheme != "https" && !(allowPrivate && value.Scheme == "http")) {
		return fmt.Errorf(
			"partner http URL must be an absolute HTTPS URL without credentials",
		)
	}
	if allowPrivate {
		return nil
	}
	return validatePartnerHost(ctx, value.Hostname(), allowPrivate)

}

func validatePartnerHost(
	ctx context.Context,
	host string,
	allowPrivate bool,
) error {

	_, err := resolvePartnerHost(ctx, host, allowPrivate)
	return err

}

func resolvePartnerHost(
	ctx context.Context,
	host string,
	allowPrivate bool,
) ([]netip.Addr, error) {

	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("partner http host cannot be resolved")
	}
	if allowPrivate {
		return addresses, nil
	}
	for _, address := range addresses {
		if !isPublicAddress(address) {
			return nil, fmt.Errorf(
				"partner http host resolves to a non-public address",
			)
		}
	}
	return addresses, nil

}

func isPublicAddress(address netip.Addr) bool {

	address = address.Unmap()
	return address.IsValid() && !address.IsLoopback() && !address.IsPrivate() &&
		!address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() &&
		!address.IsMulticast() && !address.IsUnspecified() &&
		!strings.EqualFold(address.String(), "169.254.169.254")

}
