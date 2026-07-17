package user

import (
	"context"
	"net/http"
	"time"

	json "github.com/goccy/go-json"
)

const defaultFlyerBaseURL = "https://api.flyerhubs.com"

type FlyerProvider struct {
	Client  *http.Client
	BaseURL string
	Timeout time.Duration
}

type flyerTasksResponse struct {
	Tasks  []flyerTask `json:"tasks"`
	Result []flyerTask `json:"result"`
	Data   []flyerTask `json:"data"`
	Error  string      `json:"error"`
}

type flyerTask struct {
	Signature  string          `json:"signature"`
	Type       string          `json:"type"`
	TaskType   string          `json:"task_type"`
	Title      string          `json:"title"`
	Name       string          `json:"name"`
	Link       string          `json:"link"`
	URL        string          `json:"url"`
	ButtonText string          `json:"button_text"`
	Raw        json.RawMessage `json:"-"`
}

type flyerCheckResponse struct {
	Status    string `json:"status"`
	Completed *bool  `json:"completed"`
	Skip      *bool  `json:"skip"`
	Error     string `json:"error"`
}

func (p FlyerProvider) ListPartnerTasks(
	ctx context.Context,
	params PartnerListProviderParams,
) ([]PartnerExternalTask, error) {
	body := map[string]any{
		"key":           partnerSecret(params.Config.Secret),
		"user_id":       partnerInt64String(params.Identity.PlatformUserID),
		"language_code": params.Locale,
		"limit":         partnerLimit(params.Limit, 5),
	}
	var response flyerTasksResponse
	path := "/get_tasks"
	if params.Config.Platform == "max" {
		path = "/max/get_tasks"
		body["user_locale"] = params.Locale
		if chatID := partnerString(params.Variables, "chat_id"); chatID != "" {
			body["chat_id"] = partnerInt64String(chatID)
		}
	}
	if err := p.client().postJSON(ctx, path, nil, body, &response); err != nil {
		return nil, err
	}
	tasks := response.Tasks
	if len(tasks) == 0 {
		tasks = response.Result
	}
	if len(tasks) == 0 {
		tasks = response.Data
	}
	result := make([]PartnerExternalTask, 0, len(tasks))
	for _, task := range tasks {
		if task.Signature == "" {
			continue
		}
		link := firstNonEmpty(task.Link, task.URL)
		externalType := firstNonEmpty(task.TaskType, task.Type, "task")
		title := firstNonEmpty(task.Title, task.Name)
		publicPayload := partnerMarshal(map[string]any{
			"signature": task.Signature, "link": link, "title": title,
			"button_text": firstNonEmpty(task.ButtonText, flyerButtonText(externalType)),
			"flyer_type":  externalType,
		})
		privatePayload := partnerMarshal(map[string]any{"signature": task.Signature})
		result = append(result, PartnerExternalTask{
			ExternalID: task.Signature, ExternalType: externalType,
			PublicPayload: publicPayload, PrivatePayload: privatePayload,
		})
	}
	return result, nil
}

func (p FlyerProvider) CheckPartnerTask(
	ctx context.Context,
	params PartnerCheckProviderParams,
) (PartnerCheckResult, error) {
	var private struct {
		Signature string `json:"signature"`
	}
	_ = json.Unmarshal(params.Issue.PrivatePayload, &private)
	body := map[string]any{"key": partnerSecret(params.Config.Secret), "signature": private.Signature}
	path := "/check_task"
	if params.Config.Platform == "max" {
		path = "/max/check_task"
	}
	var response flyerCheckResponse
	if err := p.client().postJSON(ctx, path, nil, body, &response); err != nil {
		return PartnerCheckResult{}, err
	}
	completed := false
	switch {
	case response.Completed != nil:
		completed = *response.Completed
	case response.Skip != nil:
		completed = *response.Skip
	case response.Status == "completed" || response.Status == "ok" || response.Status == "done":
		completed = true
	}
	status := firstNonEmpty(response.Status, boolStatus(completed))
	if response.Error != "" {
		status = response.Error
	}
	payload := partnerMarshal(map[string]any{
		"provider": "flyer", "status": status, "completed": completed,
	})
	return PartnerCheckResult{Completed: completed, Status: status, Payload: payload}, nil
}

func (p FlyerProvider) client() partnerHTTPClient {
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = defaultFlyerBaseURL
	}
	return partnerHTTPClient{client: p.Client, timeout: p.Timeout, baseURL: baseURL}
}

func flyerButtonText(externalType string) string {
	switch externalType {
	case "subscribe channel", "channel", "subscribe":
		return "Подписаться"
	default:
		return "Перейти"
	}
}

func boolStatus(value bool) string {
	if value {
		return "completed"
	}
	return "not_completed"
}

var _ PartnerProvider = FlyerProvider{}
