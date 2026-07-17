package telegramstars

import (
	"context"
	"net/http"
)

func (c *Client) RefundStarPayment(ctx context.Context, payload refundStarPaymentRequest) error {
	if err := c.requireCredentials(); err != nil {
		return err
	}

	var result botAPIResponse[bool]
	resp, err := c.rest.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		SetResult(&result).
		Post(c.methodPath("refundStarPayment"))
	if err != nil {
		return err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices || !result.OK || !result.Result {
		return wrapAPIError("refundStarPayment", resp.StatusCode(), result.ErrorCode, result.Description, resp.String())
	}
	return nil
}
