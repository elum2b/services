package user

import "github.com/elum2b/services/cpa/repository"

func mapAssignment(value repository.Assignment) AssignmentModel {
	return AssignmentModel{
		ID:          value.ID,
		CPAID:       value.CPAID,
		Code:        value.Code,
		CodeMode:    value.CodeMode,
		Status:      value.Status,
		IssuedAt:    value.IssuedAt,
		CompletedAt: value.CompletedAt,
	}
}

func mapRewards(values []repository.Reward) []RewardModel {
	result := make([]RewardModel, 0, len(values))
	for _, value := range values {
		result = append(result, RewardModel{
			Key:      value.Key,
			Type:     value.Type,
			Quantity: value.Quantity,
			Scale:    value.Scale,
			Unit:     value.Unit,
		})
	}
	return result
}

func scope(identity Identity, cpaID string) repository.UserScope {
	return repository.UserScope{
		WorkspaceID:    identity.WorkspaceID,
		CPAID:          cpaID,
		AppID:          identity.AppID,
		PlatformID:     identity.PlatformID,
		PlatformUserID: identity.PlatformUserID,
	}
}
