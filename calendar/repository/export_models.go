package repository

import "time"

const (
	ExportFormat         = "calendar.export.v1"
	ImportConflictFail   = "fail_on_conflict"
	ImportConflictSkip   = "skip_existing"
	ImportConflictUpdate = "update_existing"
)

type ExportRequest struct {
	Now time.Time `json:"now,omitempty"`
}

type ExportPackage struct {
	Format    string           `json:"format"`
	Service   string           `json:"service"`
	CreatedAt time.Time        `json:"created_at"`
	Calendars []ExportCalendar `json:"calendars"`
}

type ExportCalendar struct {
	ID                  string                `json:"id,omitempty"`
	Type                string                `json:"type"`
	Mode                string                `json:"mode"`
	IntervalType        string                `json:"interval_type"`
	IntervalUnit        string                `json:"interval_unit"`
	IntervalCount       uint32                `json:"interval_count"`
	ResetAfterIntervals uint32                `json:"reset_after_intervals"`
	EndBehavior         string                `json:"end_behavior"`
	Timezone            string                `json:"timezone"`
	HideFutureRewards   bool                  `json:"hide_future_rewards"`
	IsActive            bool                  `json:"is_active"`
	StartAt             *time.Time            `json:"start_at,omitempty"`
	EndAt               *time.Time            `json:"end_at,omitempty"`
	Localization        map[string]ExportText `json:"localization,omitempty"`
	Steps               []ExportStep          `json:"steps,omitempty"`
}

type ExportText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ExportStep struct {
	Position uint32         `json:"position"`
	Rewards  []ExportReward `json:"rewards,omitempty"`
}

type ExportReward struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
	Position uint32  `json:"position"`
}

type ImportRequest struct {
	Package          ExportPackage `json:"package"`
	ConflictStrategy string        `json:"conflict_strategy,omitempty"`
}

type ImportPreview struct {
	Format    string           `json:"format"`
	Service   string           `json:"service"`
	Counts    ImportCounts     `json:"counts"`
	Conflicts []ImportConflict `json:"conflicts,omitempty"`
}

type ImportCounts struct {
	Calendars     uint64 `json:"calendars"`
	Localizations uint64 `json:"localizations"`
	Steps         uint64 `json:"steps"`
	Rewards       uint64 `json:"rewards"`
}

type ImportConflict struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type ImportResult struct {
	Imported ImportCounts `json:"imported"`
	Skipped  ImportCounts `json:"skipped"`
}
