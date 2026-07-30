package tonconnect

import (
	"errors"
	"testing"
)

func TestManifestValidate(t *testing.T) {
	t.Parallel()

	valid := Manifest{
		URL:     "https://chimpbot.org",
		Name:    "Chimp",
		IconURL: "https://chimpbot.org/logo.png",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	tests := []struct {
		name     string
		manifest Manifest
		want     error
	}{
		{
			name: "app URL is HTTP",
			manifest: Manifest{
				URL:     "http://chimpbot.org",
				Name:    "Chimp",
				IconURL: valid.IconURL,
			},
			want: ErrManifestURLInvalid,
		},
		{
			name: "app URL has trailing slash",
			manifest: Manifest{
				URL:     "https://chimpbot.org/",
				Name:    "Chimp",
				IconURL: valid.IconURL,
			},
			want: ErrManifestURLInvalid,
		},
		{
			name: "name is missing",
			manifest: Manifest{
				URL:     valid.URL,
				IconURL: valid.IconURL,
			},
			want: ErrManifestNameInvalid,
		},
		{
			name: "icon URL is relative",
			manifest: Manifest{
				URL:     valid.URL,
				Name:    "Chimp",
				IconURL: "/logo.png",
			},
			want: ErrManifestIconURLInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.manifest.Validate(); !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want %v", err, test.want)
			}
		})
	}
}
