package admin

import "testing"

func TestValidateReward(t *testing.T) {
	week := "week"
	base := SaveRewardParams{
		WorkspaceID: "00000000-0000-0000-0000-000000000001", CalendarID: "calendar", StepID: 1,
		Key: "premium", Quantity: 2, Position: 1,
	}
	if err := validateReward(base); err != nil {
		t.Fatalf("default quantity reward: %v", err)
	}
	base.Type = "duration"
	base.Unit = &week
	if err := validateReward(base); err != nil {
		t.Fatalf("duration reward: %v", err)
	}
	base.Unit = nil
	if err := validateReward(base); err == nil {
		t.Fatal("duration reward without unit must fail")
	}
}
