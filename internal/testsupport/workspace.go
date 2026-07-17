package testsupport

import "github.com/google/uuid"

// WorkspaceID returns a stable canonical UUID for a human-readable test seed.
func WorkspaceID(seed string) string {
	return uuid.NewSHA1(uuid.Nil, []byte(seed)).String()
}
