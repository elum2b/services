package platega

import (
	"context"
	"net/http"
)

func (c *Client) CreateTransaction(ctx context.Context, payload createTransactionRequest) (createTransactionResponse, error) {
	if err := c.requireCredentials(); err != nil {
		return createTransactionResponse{}, err
	}

	path := "/v2/transaction/process"
	if payload.PaymentMethod != nil {
		path = "/transaction/process"
	}

	var result createTransactionResponse
	resp, err := c.rest.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		SetResult(&result).
		Post(path)
	if err != nil {
		return createTransactionResponse{}, err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return createTransactionResponse{}, wrapAPIError("create transaction", resp.StatusCode(), resp.String())
	}
	return result, nil
}
