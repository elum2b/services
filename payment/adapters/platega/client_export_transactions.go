package platega

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	json "github.com/goccy/go-json"
)

const maxExportResponseSize = 16 << 20

func (c *Client) ExportTransactions(
	ctx context.Context,
	params exportTransactionsRequest,
) ([]exportedTransaction, error) {
	if err := c.requireCredentials(); err != nil {
		return nil, err
	}

	resp, err := c.rest.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetDoNotParseResponse(true).
		SetBody(params).
		Post("/transaction/export/json")
	if err != nil {
		return nil, err
	}
	rawBody := resp.RawBody()
	if rawBody == nil {
		return nil, ErrExportResponseInvalid
	}
	defer rawBody.Close()

	raw, err := readLimitedExportBody(rawBody)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return nil, wrapAPIError("export transactions", resp.StatusCode(), string(raw))
	}

	return c.decodeExportResponse(ctx, raw)
}

func (c *Client) decodeExportResponse(ctx context.Context, raw []byte) ([]exportedTransaction, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, ErrExportResponseInvalid
	}
	if raw[0] == '[' {
		return decodeExportTransactions(raw)
	}

	downloadURL, err := exportDownloadURL(raw)
	if err != nil {
		return nil, err
	}
	if err := c.validateExportDownloadURL(ctx, downloadURL); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.exportDownloadClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, wrapAPIError("download transaction export", resp.StatusCode, resp.Status)
	}

	downloaded, err := readLimitedExportBody(resp.Body)
	if err != nil {
		return nil, err
	}

	return decodeExportTransactions(downloaded)
}

func (c *Client) exportDownloadClient() *http.Client {
	client := *c.httpClient
	previousCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return ErrExportURLUnsafe
		}
		if err := c.validateExportDownloadURL(request.Context(), request.URL.String()); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			return previousCheckRedirect(request, via)
		}

		return nil
	}

	return &client
}

func readLimitedExportBody(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maxExportResponseSize+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxExportResponseSize {
		return nil, ErrExportResponseTooLarge
	}

	return raw, nil
}

func decodeExportTransactions(raw []byte) ([]exportedTransaction, error) {
	var result []exportedTransaction
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExportResponseInvalid, err)
	}
	return result, nil
}

func exportDownloadURL(raw []byte) (string, error) {
	var direct string
	if err := json.Unmarshal(raw, &direct); err == nil && direct != "" {
		return direct, nil
	}

	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", ErrExportResponseInvalid
	}
	for _, key := range []string{"url", "downloadUrl", "download_url", "link"} {
		value, ok := envelope[key].(string)
		if ok && strings.TrimSpace(value) != "" {
			return value, nil
		}
	}

	return "", ErrExportResponseInvalid
}

func (c *Client) validateExportDownloadURL(ctx context.Context, value string) error {
	target, err := url.Parse(value)
	if err != nil || target.Hostname() == "" || target.User != nil {
		return ErrExportURLUnsafe
	}
	base, baseErr := url.Parse(c.apiBaseURL)
	allowTestHTTP := baseErr == nil && base.Scheme == "http" && target.Host == base.Host
	if target.Scheme != "https" && !allowTestHTTP {
		return ErrExportURLUnsafe
	}
	if strings.EqualFold(target.Hostname(), "localhost") && !allowTestHTTP {
		return ErrExportURLUnsafe
	}

	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, target.Hostname())
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if address.IP.IsLoopback() || address.IP.IsPrivate() || address.IP.IsLinkLocalUnicast() ||
			address.IP.IsLinkLocalMulticast() || address.IP.IsUnspecified() {
			if !allowTestHTTP {
				return ErrExportURLUnsafe
			}
		}
	}

	return nil
}
