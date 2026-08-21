// Package lottie exposes lightweight Lottie metadata validation.
package lottie

import (
	"errors"
	"fmt"

	json "github.com/goccy/go-json"
)

var ErrNotLottie = errors.New("not a lottie animation")
var ErrUnsafeContent = errors.New("unsafe lottie content")

// Meta contains the composition bounds and frame range.
type Meta struct {
	Width  int
	Height int
	In     float64
	Out    float64
}

// Validate verifies the JSON container and its composition dimensions.
func Validate(data []byte) (Meta, error) {
	var document struct {
		Width   int             `json:"w"`
		Height  int             `json:"h"`
		In      float64         `json:"ip"`
		Out     float64         `json:"op"`
		Version string          `json:"v"`
		Layers  json.RawMessage `json:"layers"`
	}

	if err := json.Unmarshal(data, &document); err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrNotLottie, err)
	}

	if document.Version == "" || len(document.Layers) == 0 {
		return Meta{}, fmt.Errorf("%w: missing version or layers", ErrNotLottie)
	}

	if document.Width <= 0 || document.Height <= 0 {
		return Meta{}, fmt.Errorf(
			"%w: invalid dimensions %dx%d",
			ErrNotLottie,
			document.Width,
			document.Height,
		)
	}

	var root map[string]any

	if err := json.Unmarshal(data, &root); err != nil {
		return Meta{}, fmt.Errorf("%w: %w", ErrNotLottie, err)
	}

	if err := validateSafe(root, false); err != nil {
		return Meta{}, err
	}

	return Meta{
		Width:  document.Width,
		Height: document.Height,
		In:     document.In,
		Out:    document.Out,
	}, nil
}

func validateSafe(value any, inAssets bool) error {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if err := validateSafe(item, inAssets); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, item := range value {
			if key == "x" {
				if expression, ok := item.(string); ok && expression != "" {
					return fmt.Errorf(
						"%w: expressions are not allowed",
						ErrUnsafeContent,
					)
				}
			}

			if inAssets && (key == "p" || key == "u") {
				if reference, ok := item.(string); ok && reference != "" {
					return fmt.Errorf(
						"%w: external assets are not allowed",
						ErrUnsafeContent,
					)
				}
			}

			if err := validateSafe(
				item,
				inAssets || key == "assets",
			); err != nil {
				return err
			}
		}
	}

	return nil
}
