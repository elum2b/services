package repository

import (
	"fmt"
	"math"
	"strings"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/internal/utils/target"
)

func normalizeSaveTaskParams(params SaveTaskParams) SaveTaskParams {
	params.Key = strings.TrimSpace(params.Key)
	params.GroupKey = strings.TrimSpace(params.GroupKey)
	params.ActionKey = strings.TrimSpace(params.ActionKey)
	params.TaskKind = defaultString(params.TaskKind, TaskKindInternal)
	params.ClaimMode = defaultString(params.ClaimMode, ClaimModeManual)
	params.StartMode = defaultString(params.StartMode, StartModeNone)
	params.ResetUnit = defaultString(params.ResetUnit, ResetNever)
	params.ResetEvery = defaultUint32(params.ResetEvery, 1)
	if len(params.Payload) == 0 {
		params.Payload = []byte("{}")
	}
	if len(params.Target) == 0 {
		params.Target = []byte("null")
	}
	if len(params.IntegrationPayload) == 0 {
		params.IntegrationPayload = []byte("null")
	}
	return params
}

func validateSaveTask(params SaveTaskParams) error {
	if err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}
	if params.ID > math.MaxInt64 || params.TargetCount > math.MaxInt64 ||
		params.ResetEvery > math.MaxInt32 {
		return fmt.Errorf("tasks numeric value is out of database range")
	}
	if params.Key == "" {
		return fmt.Errorf("tasks key is required")
	}
	if params.GroupKey == "" {
		return fmt.Errorf("tasks group_key is required")
	}
	if params.ActionKey == "" {
		return fmt.Errorf("tasks action_key is required")
	}
	if !validTaskKind(params.TaskKind) {
		return fmt.Errorf("tasks task_kind %q is unsupported", params.TaskKind)
	}
	if !validActionKind(params.ActionKind) {
		return fmt.Errorf("tasks action_kind %q is unsupported", params.ActionKind)
	}
	if !validTaskActionKind(params.TaskKind, params.ActionKind) {
		return fmt.Errorf(
			"tasks task_kind %q is incompatible with action_kind %q",
			params.TaskKind,
			params.ActionKind,
		)
	}
	if params.ClaimMode != ClaimModeManual && params.ClaimMode != ClaimModeAuto {
		return fmt.Errorf("tasks claim_mode %q is unsupported", params.ClaimMode)
	}
	if params.ClaimMode == ClaimModeAuto && params.TaskKind != TaskKindInternal {
		return fmt.Errorf(
			"tasks claim_mode %q is unsupported for task_kind %q",
			params.ClaimMode,
			params.TaskKind,
		)
	}
	if params.StartMode != StartModeNone && params.StartMode != StartModeRequired {
		return fmt.Errorf("tasks start_mode %q is unsupported", params.StartMode)
	}
	if params.TargetCount == 0 {
		return fmt.Errorf("tasks target_count must be positive")
	}
	if !validResetUnit(params.ResetUnit) || params.ResetEvery == 0 {
		return fmt.Errorf("tasks reset configuration is invalid")
	}
	if (params.SequenceKey == nil) != (params.SequencePosition == nil) {
		return fmt.Errorf("tasks sequence_key and sequence_position must be set together")
	}
	if params.SequencePosition != nil &&
		(*params.SequencePosition == 0 || *params.SequencePosition > math.MaxInt32) {
		return fmt.Errorf("tasks sequence_position must be positive and fit int32")
	}
	if params.StartAt != nil && params.EndAt != nil && !params.StartAt.Before(*params.EndAt) {
		return fmt.Errorf("tasks start_at must be before end_at")
	}
	if !json.Valid(params.Payload) {
		return fmt.Errorf("tasks payload must be valid JSON")
	}
	if err := target.Validate(params.Target); err != nil {
		return fmt.Errorf("tasks target: %w", err)
	}
	if !json.Valid(params.IntegrationPayload) {
		return fmt.Errorf("tasks integration payload must be valid JSON")
	}
	return nil
}

func validTaskKind(value string) bool {
	switch value {
	case TaskKindInternal,
		TaskKindChannelBoost,
		TaskKindChannelSubscribe,
		TaskKindExternalCheck,
		TaskKindExternalConfirming,
		TaskKindComplex,
		TaskKindPartner:
		return true
	default:
		return false
	}
}

func validActionKind(value string) bool {
	switch value {
	case ActionKindAppAction,
		ActionKindAmountAction,
		ActionKindChannelBoost,
		ActionKindChannelSubscribe,
		ActionKindAdvertisementView,
		ActionKindExternal,
		ActionKindComposite:
		return true
	default:
		return false
	}
}

func validTaskActionKind(taskKind, actionKind string) bool {
	switch taskKind {
	case TaskKindInternal:
		return actionKind == ActionKindAppAction ||
			actionKind == ActionKindAmountAction ||
			actionKind == ActionKindAdvertisementView
	case TaskKindChannelBoost:
		return actionKind == ActionKindChannelBoost
	case TaskKindChannelSubscribe:
		return actionKind == ActionKindChannelSubscribe
	case TaskKindExternalCheck, TaskKindExternalConfirming, TaskKindPartner:
		return actionKind == ActionKindExternal
	case TaskKindComplex:
		return actionKind == ActionKindComposite
	default:
		return false
	}
}

func validResetUnit(value string) bool {
	switch value {
	case ResetNever, ResetSecond, ResetMinute, ResetHour, ResetDay, ResetYear:
		return true
	default:
		return false
	}
}

func validateRewardDefinition(reward ExportReward) error {
	if strings.TrimSpace(reward.Key) == "" || reward.Quantity <= 0 {
		return fmt.Errorf("reward key and positive quantity are required")
	}
	if reward.Scale > math.MaxInt16 {
		return fmt.Errorf("reward scale is out of database range")
	}
	switch defaultString(reward.Type, "quantity") {
	case "quantity":
		if reward.Unit != nil {
			return fmt.Errorf("quantity reward must not have duration unit")
		}
	case "duration":
		if reward.Unit == nil || !validDurationUnit(*reward.Unit) {
			return fmt.Errorf("duration reward requires a valid duration unit")
		}
	default:
		return fmt.Errorf("reward type must be quantity or duration")
	}
	return nil
}

func validDurationUnit(value string) bool {
	switch value {
	case "second", "minute", "hour", "day", "week", "month", "year":
		return true
	default:
		return false
	}
}

func validateComplexCondition(params SaveComplexConditionParams) error {
	if err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}
	if params.ParentTaskID == 0 || params.ConditionTaskID == 0 {
		return fmt.Errorf("tasks complex condition task IDs must be positive")
	}
	if params.ParentTaskID > math.MaxInt64 || params.ConditionTaskID > math.MaxInt64 {
		return fmt.Errorf("tasks complex condition task ID is out of database range")
	}
	if params.ParentTaskID == params.ConditionTaskID {
		return fmt.Errorf("tasks complex condition cannot reference itself")
	}
	if params.RequiredStatus != ComplexRequiredStatusReady &&
		params.RequiredStatus != ComplexRequiredStatusClaimed {
		return fmt.Errorf("tasks complex condition required_status %q is unsupported", params.RequiredStatus)
	}
	return nil
}

func hasDirectedCycle[T comparable](graph map[T][]T) bool {
	const (
		visiting uint8 = iota + 1
		visited
	)

	state := make(map[T]uint8, len(graph))
	var visit func(T) bool
	visit = func(node T) bool {
		switch state[node] {
		case visiting:
			return true
		case visited:
			return false
		}

		state[node] = visiting
		for _, child := range graph[node] {
			if visit(child) {
				return true
			}
		}
		state[node] = visited
		return false
	}

	for node := range graph {
		if visit(node) {
			return true
		}
	}
	return false
}
