package repository

import "testing"

func TestValidReward(t *testing.T) {
	day := "day"
	invalid := "fortnight"
	if !validReward("quantity", nil) {
		t.Fatal("quantity reward without unit must be valid")
	}
	if !validReward("duration", &day) {
		t.Fatal("duration reward with supported unit must be valid")
	}
	if validReward("duration", nil) {
		t.Fatal("duration reward without unit must be invalid")
	}
	if validReward("duration", &invalid) {
		t.Fatal("duration reward with unsupported unit must be invalid")
	}
	if validReward("quantity", &day) {
		t.Fatal("quantity reward with unit must be invalid")
	}
}
