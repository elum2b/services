package utils

//go:fix inline
func Ref[T any](value T) *T {
	return new(value)
}

func Deref[T any](value *T) T {
	var zero T
	if value != nil {
		return *value
	}
	return zero
}
