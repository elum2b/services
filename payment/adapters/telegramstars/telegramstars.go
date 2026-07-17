package telegramstars

import (
	"context"
	"net/http"
	"strings"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	"github.com/elum2b/services/payment/repository"

	"github.com/go-resty/resty/v2"
)

const (
	ProviderCode  = "telegram_stars"
	AssetCode     = "XTR"
	defaultAPIURL = "https://api.telegram.org"
)

type Credentials struct {
	BotToken   string
	APIBaseURL string
	HTTPClient *http.Client
}

type TelegramStars struct {
	repository *repository.PaymentRepository
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *TelegramStars {
	return NewWithOptions(ctx, db, repository.Options{})
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *TelegramStars {
	repo, err := repository.NewPreparedPaymentRepositoryWithOptions(context.Background(), db, options)
	if err != nil {
		repo = repository.NewPaymentRepositoryWithOptions(db, options)
	}
	return &TelegramStars{repository: repo, rootCtx: contextutil.Normalize(ctx)}
}

func (a *TelegramStars) Close() error {
	if a == nil || a.repository == nil {
		return nil
	}
	return a.repository.Close()
}

func (a *TelegramStars) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}

type Client struct {
	botToken string
	rest     *resty.Client
}

func NewClient(credentials Credentials) *Client {
	apiBaseURL := strings.TrimRight(credentials.APIBaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIURL
	}

	restClient := resty.New()
	if credentials.HTTPClient != nil {
		restClient = resty.NewWithClient(credentials.HTTPClient)
	}
	restClient.SetBaseURL(apiBaseURL)
	restClient.SetHeader("Accept", "application/json")

	return &Client{
		botToken: credentials.BotToken,
		rest:     restClient,
	}
}

func (c *Client) requireCredentials() error {
	if c == nil || c.botToken == "" {
		return ErrBotTokenRequired
	}
	return nil
}

func (c *Client) methodPath(method string) string {
	return "/bot" + c.botToken + "/" + method
}
