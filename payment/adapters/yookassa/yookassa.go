package yookassa

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
	ProviderCode  = "yookassa"
	AssetCode     = "RUB"
	defaultAPIURL = "https://api.yookassa.ru"
)

type Credentials struct {
	ShopID     string
	SecretKey  string
	APIBaseURL string
	HTTPClient *http.Client
}

type YooKassa struct {
	repository *repository.PaymentRepository
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *YooKassa {
	return NewWithOptions(ctx, db, repository.Options{})
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *YooKassa {
	repo, err := repository.NewPreparedPaymentRepositoryWithOptions(context.Background(), db, options)
	if err != nil {
		repo = repository.NewPaymentRepositoryWithOptions(db, options)
	}
	return &YooKassa{repository: repo, rootCtx: contextutil.Normalize(ctx)}
}

func (a *YooKassa) Close() error {
	if a == nil || a.repository == nil {
		return nil
	}
	return a.repository.Close()
}

func (a *YooKassa) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}

type Client struct {
	shopID    string
	secretKey string
	rest      *resty.Client
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
	restClient.SetBasicAuth(credentials.ShopID, credentials.SecretKey)
	restClient.SetHeader("Accept", "application/json")

	return &Client{
		shopID:    credentials.ShopID,
		secretKey: credentials.SecretKey,
		rest:      restClient,
	}
}

func (c *Client) requireCredentials() error {
	if c == nil || c.shopID == "" || c.secretKey == "" {
		return ErrCredentialsRequired
	}
	return nil
}
