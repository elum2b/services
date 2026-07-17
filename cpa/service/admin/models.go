package admin

import (
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/cpa/model"
	"github.com/elum2b/services/cpa/repository"
	"github.com/elum2b/services/cpa/service/user"
)

type Page struct {
	Limit  int32
	Offset int32
}

type OfferModel struct {
	ID                string              `json:"id"`
	Payload           json.RawMessage     `json:"payload"`
	Target            json.RawMessage     `json:"target,omitempty"`
	CodeMode          string              `json:"code_mode"`
	CodeSource        *string             `json:"code_source,omitempty"`
	SharedCode        *string             `json:"shared_code,omitempty"`
	GeneratedLength   *int16              `json:"generated_length,omitempty"`
	GeneratedAlphabet *string             `json:"generated_alphabet,omitempty"`
	IsActive          bool                `json:"is_active"`
	StartAt           *time.Time          `json:"start_at,omitempty"`
	EndAt             *time.Time          `json:"end_at,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
	Localizations     []LocalizationModel `json:"localizations,omitempty"`
	Rewards           []user.RewardModel  `json:"rewards,omitempty"`
}

type LocalizationModel struct {
	Locale      string `json:"locale"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type StatsModel struct {
	AssignmentsTotal uint64 `json:"assignments_total"`
	IssuedTotal      uint64 `json:"issued_total"`
	CompletedTotal   uint64 `json:"completed_total"`
	DeletedTotal     uint64 `json:"deleted_total"`
	CodesTotal       uint64 `json:"codes_total"`
	AvailableCodes   uint64 `json:"available_codes"`
	IssuedCodes      uint64 `json:"issued_codes"`
	CompletedCodes   uint64 `json:"completed_codes"`
	DeletedCodes     uint64 `json:"deleted_codes"`
}

type DailyStatsModel struct {
	Date           time.Time `json:"date"`
	IssuedCount    uint64    `json:"issued_count"`
	CompletedCount uint64    `json:"completed_count"`
	UniqueUsers    uint64    `json:"unique_users"`
}

type AssignmentModel struct {
	ID             uint64                 `json:"id"`
	CPAID          string                 `json:"cpa_id"`
	AppID          int64                  `json:"app_id"`
	PlatformID     int64                  `json:"platform_id"`
	PlatformUserID string                 `json:"platform_user_id"`
	Code           string                 `json:"code"`
	CodeMode       string                 `json:"code_mode"`
	Status         model.AssignmentStatus `json:"status"`
	IssuedAt       time.Time              `json:"issued_at"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
}

type CodeModel struct {
	ID        uint64           `json:"id"`
	Code      string           `json:"code"`
	Source    string           `json:"source"`
	Status    model.CodeStatus `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	DeletedAt *time.Time       `json:"deleted_at,omitempty"`
}

type AssignmentEventModel struct {
	ID           uint64                    `json:"id"`
	AssignmentID uint64                    `json:"assignment_id"`
	EventType    model.AssignmentEventType `json:"event_type"`
	OccurredAt   time.Time                 `json:"occurred_at"`
}

type ExportRequest = repository.ExportRequest
type ExportPackage = repository.ExportPackage
type ExportOffer = repository.ExportOffer
type ExportText = repository.ExportText
type ExportReward = repository.ExportReward
type ImportRequest = repository.ImportRequest
type ImportPreview = repository.ImportPreview
type ImportCounts = repository.ImportCounts
type ImportConflict = repository.ImportConflict
type ImportResult = repository.ImportResult
