package telegramstars

import (
	"context"
	"net/http"
)

func (c *Client) AnswerPreCheckoutQuery(ctx context.Context, queryID string, ok bool, errorMessage string) error {
	if err := c.requireCredentials(); err != nil {
		return err
	}

	var result botAPIResponse[bool]
	resp, err := c.rest.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(answerPreCheckoutQueryRequest{
			PreCheckoutQueryID: queryID,
			OK:                 ok,
			ErrorMessage:       errorMessage,
		}).
		SetResult(&result).
		Post(c.methodPath("answerPreCheckoutQuery"))
	if err != nil {
		return err
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices || !result.OK || !result.Result {
		return wrapAPIError("answerPreCheckoutQuery", resp.StatusCode(), result.ErrorCode, result.Description, resp.String())
	}
	return nil
}
