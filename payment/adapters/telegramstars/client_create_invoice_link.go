package telegramstars

import (
	"context"
	"net/http"
)

func (c *Client) CreateInvoiceLink(ctx context.Context, payload createInvoiceLinkRequest) (string, error) {
	if err := c.requireCredentials(); err != nil {
		return "", err
	}

	var result botAPIResponse[string]
	resp, err := c.rest.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		SetResult(&result).
		Post(c.methodPath("createInvoiceLink"))
	if err != nil {
		return "", err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices || !result.OK {
		return "", wrapAPIError("createInvoiceLink", resp.StatusCode(), result.ErrorCode, result.Description, resp.String())
	}
	return result.Result, nil
}
