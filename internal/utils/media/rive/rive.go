// Package rive validates Rive binary containers before rendering.
package rive

import (
	"bytes"
	"errors"
	"fmt"
)

var ErrNotRive = errors.New("not a rive animation")

var magic = []byte("RIVE")

// Validate verifies the Rive binary signature. Composition dimensions are
// supplied by the rendering runtime because a .riv file can contain multiple
// artboards with independent bounds.
func Validate(data []byte) error {
	if len(data) < len(magic) || !bytes.Equal(data[:len(magic)], magic) {
		return fmt.Errorf("%w: missing RIVE signature", ErrNotRive)
	}

	return nil
}
