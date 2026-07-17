package platega

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
	ProviderCode  = "platega"
	AssetCode     = "RUB"
	defaultAPIURL = "https://app.platega.io"
)

type Credentials struct {
	MerchantID string
	Secret     string
	APIBaseURL string
	HTTPClient *http.Client
}

type Platega struct {
	repository *repository.PaymentRepository
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *Platega {
	return NewWithOptions(ctx, db, repository.Options{})
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *Platega {
	repo, err := repository.NewPreparedPaymentRepositoryWithOptions(context.Background(), db, options)
	if err != nil {
		repo = repository.NewPaymentRepositoryWithOptions(db, options)
	}
	return &Platega{repository: repo, rootCtx: contextutil.Normalize(ctx)}
}

func (a *Platega) Close() error {
	if a == nil || a.repository == nil {
		return nil
	}
	return a.repository.Close()
}

func (a *Platega) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}

type Client struct {
	merchantID string
	secret     string
	rest       *resty.Client
	httpClient *http.Client
	apiBaseURL string
}

func NewClient(credentials Credentials) *Client {
	apiBaseURL := strings.TrimRight(credentials.APIBaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIURL
	}

	restClient := resty.New()
	httpClient := http.DefaultClient
	if credentials.HTTPClient != nil {
		restClient = resty.NewWithClient(credentials.HTTPClient)
		httpClient = credentials.HTTPClient
	}
	restClient.SetBaseURL(apiBaseURL)
	restClient.SetHeader("Accept", "application/json")
	restClient.SetHeader("X-MerchantId", credentials.MerchantID)
	restClient.SetHeader("X-Secret", credentials.Secret)

	return &Client{
		merchantID: credentials.MerchantID,
		secret:     credentials.Secret,
		rest:       restClient,
		httpClient: httpClient,
		apiBaseURL: apiBaseURL,
	}
}

func (c *Client) requireCredentials() error {
	if c == nil || c.merchantID == "" || c.secret == "" {
		return ErrCredentialsRequired
	}
	return nil
}
