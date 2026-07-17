package yookassa

import (
	"context"
	"net/http"
)

func (c *Client) CreateRefund(ctx context.Context, payload createRefundRequest, idempotencyKey string) (refundAPIResponse, error) {
	if err := c.requireCredentials(); err != nil {
		return refundAPIResponse{}, err
	}

	var result refundAPIResponse
	resp, err := c.rest.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("Idempotence-Key", idempotencyKey).
		SetBody(payload).
		SetResult(&result).
		Post("/v3/refunds")
	if err != nil {
		return refundAPIResponse{}, err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return refundAPIResponse{}, wrapAPIError("create refund", resp.StatusCode(), resp.String())
	}
	return result, nil
}
