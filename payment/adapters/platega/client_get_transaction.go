package platega

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) GetTransaction(ctx context.Context, transactionID string) (transactionStatusResponse, error) {
	if err := c.requireCredentials(); err != nil {
		return transactionStatusResponse{}, err
	}

	var result transactionStatusResponse
	resp, err := c.rest.R().
		SetContext(ctx).
		SetResult(&result).
		Get("/transaction/" + url.PathEscape(transactionID))
	if err != nil {
		return transactionStatusResponse{}, err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return transactionStatusResponse{}, wrapAPIError("get transaction", resp.StatusCode(), resp.String())
	}
	return result, nil
}
