package yookassa

import (
	"context"
	"net/http"
)

func (c *Client) CreatePayment(ctx context.Context, payload createPaymentRequest, idempotencyKey string) (paymentAPIResponse, error) {
	if err := c.requireCredentials(); err != nil {
		return paymentAPIResponse{}, err
	}

	var result paymentAPIResponse
	resp, err := c.rest.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("Idempotence-Key", idempotencyKey).
		SetBody(payload).
		SetResult(&result).
		Post("/v3/payments")
	if err != nil {
		return paymentAPIResponse{}, err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return paymentAPIResponse{}, wrapAPIError("create payment", resp.StatusCode(), resp.String())
	}
	return result, nil
}
