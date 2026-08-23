// Package svg validates self-contained SVG documents before rendering.
package svg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrNotSVG = errors.New("not an SVG document")
var ErrUnsafeContent = errors.New("unsafe SVG content")

func Validate(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	root := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrNotSVG, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if !root {
			if start.Name.Local != "svg" {
				return ErrNotSVG
			}
			root = true
		}
		switch strings.ToLower(start.Name.Local) {
		case "script", "foreignobject", "image", "iframe", "object", "embed":
			return fmt.Errorf("%w: disallowed <%s>", ErrUnsafeContent, start.Name.Local)
		}
		for _, attribute := range start.Attr {
			name := strings.ToLower(attribute.Name.Local)
			if strings.HasPrefix(name, "on") {
				return fmt.Errorf("%w: event attribute", ErrUnsafeContent)
			}
			if name == "href" && attribute.Value != "" && !strings.HasPrefix(attribute.Value, "#") {
				return fmt.Errorf("%w: external reference", ErrUnsafeContent)
			}
		}
	}
	if !root {
		return ErrNotSVG
	}
	return nil
}
