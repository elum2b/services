package platega

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) GetH2H(ctx context.Context, transactionID string) (H2HResponse, error) {
	if err := c.requireCredentials(); err != nil {
		return H2HResponse{}, err
	}

	var result H2HResponse
	resp, err := c.rest.R().
		SetContext(ctx).
		SetResult(&result).
		Get("/h2h/" + url.PathEscape(transactionID))
	if err != nil {
		return H2HResponse{}, err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return H2HResponse{}, wrapAPIError("get h2h", resp.StatusCode(), resp.String())
	}
	return result, nil
}
