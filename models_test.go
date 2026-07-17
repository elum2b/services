package services

import (
	"errors"
	"testing"

	json "github.com/goccy/go-json"
)

func TestValidateWorkspaceIDRequiresCanonicalUUID(t *testing.T) {
	if err := ValidateWorkspaceID("00000000-0000-0000-0000-000000000001"); err != nil {
		t.Fatalf("validate canonical UUID: %v", err)
	}

	for _, value := range []string{
		"",
		"workspace",
		" 00000000-0000-0000-0000-000000000001 ",
		"00000000000000000000000000000001",
		"00000000-0000-0000-0000-00000000000A",
	} {
		err := ValidateWorkspaceID(value)
		if err == nil {
			t.Fatalf("workspace %q must be rejected", value)
		}
		if value == "" && !errors.Is(err, ErrIdentityWorkspaceRequired) {
			t.Fatalf("empty workspace error = %v", err)
		}
	}
}

func TestRewardPayloadJSON(t *testing.T) {
	day := "day"
	value := RewardPayload{
		Identity: Identity{
			WorkspaceID:    "00000000-0000-0000-0000-000000000001",
			AppID:          1,
			PlatformID:     2,
			PlatformUserID: "3",
		},
		Rewards: []Reward{
			{Key: "coin", Type: "quantity", Quantity: 10, Scale: 2},
			{Key: "premium", Type: "duration", Quantity: 1, Unit: &day},
		},
	}

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	var decoded RewardPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.WorkspaceID != value.WorkspaceID ||
		decoded.AppID != value.AppID ||
		decoded.PlatformID != value.PlatformID ||
		decoded.PlatformUserID != value.PlatformUserID ||
		len(decoded.Rewards) != 2 ||
		decoded.Rewards[0].Scale != 2 ||
		decoded.Rewards[1].Unit == nil ||
		*decoded.Rewards[1].Unit != day {
		t.Fatalf("unexpected decoded payload: %+v", decoded)
	}
}
