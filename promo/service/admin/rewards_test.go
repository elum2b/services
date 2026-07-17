package admin

import "testing"

func TestValidateReward(t *testing.T) {
	month := "month"
	if rewardType, err := validateReward("coin", "", 1, nil); err != nil || rewardType != "quantity" {
		t.Fatalf("default quantity reward: type=%q err=%v", rewardType, err)
	}
	if rewardType, err := validateReward("premium", "duration", 1, &month); err != nil || rewardType != "duration" {
		t.Fatalf("duration reward: type=%q err=%v", rewardType, err)
	}
	if _, err := validateReward("premium", "duration", 1, nil); err == nil {
		t.Fatal("duration reward without unit must fail")
	}
}
