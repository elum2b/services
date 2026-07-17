package repository

import (
	"testing"
	"time"
)

func TestTOTPAndBackupCodes(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	code := totp(secret, now)
	if code == "" || !validTOTP(secret, code, now) {
		t.Fatalf("valid TOTP rejected: %q", code)
	}
	if validTOTP(secret, "000000", now) && code != "000000" {
		t.Fatal("invalid TOTP accepted")
	}
	codes, hashes, err := newBackupCodes()
	if err != nil || len(codes) != 10 || len(hashes) != 10 {
		t.Fatalf("backup codes: codes=%d hashes=%d err=%v", len(codes), len(hashes), err)
	}
	if backupHash(codes[0]) != hashes[0] || backupHash(codes[0]) == backupHash(codes[1]) {
		t.Fatal("backup code hashes are invalid")
	}
}
