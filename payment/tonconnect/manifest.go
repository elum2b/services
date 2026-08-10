package tonconnect

import (
	"net/url"
	"strings"
	"unicode/utf8"

	serviceerrors "github.com/elum2b/services/errors"
)

const (
	maxNameLength = 255
	maxURLLength  = 2048
)

var (
	ErrManifestURLInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment TON Connect manifest url must be an absolute HTTPS URL without a trailing slash",
	)
	ErrManifestNameInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment TON Connect manifest name is required and must not exceed 255 characters",
	)
	ErrManifestIconURLInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment TON Connect manifest icon URL must be an absolute HTTPS URL",
	)
	ErrManifestTermsOfUseURLInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment TON Connect manifest terms of use URL must be an absolute HTTPS URL",
	)
	ErrManifestPrivacyPolicyURLInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment TON Connect manifest privacy policy URL must be an absolute HTTPS URL",
	)
)

// Manifest contains the public dApp metadata displayed by TON Connect wallets.
type Manifest struct {
	URL              string  `json:"url"`
	Name             string  `json:"name"`
	IconURL          string  `json:"iconUrl"`
	TermsOfUseURL    *string `json:"termsOfUseUrl,omitempty"`
	PrivacyPolicyURL *string `json:"privacyPolicyUrl,omitempty"`
}

// Validate checks the TON Connect manifest contract without changing input values.
func (m Manifest) Validate() error {
	if !validHTTPSURL(m.URL) || strings.HasSuffix(m.URL, "/") {
		return ErrManifestURLInvalid
	}

	if m.Name == "" || utf8.RuneCountInString(m.Name) > maxNameLength {
		return ErrManifestNameInvalid
	}

	if !validHTTPSURL(m.IconURL) {
		return ErrManifestIconURLInvalid
	}

	if m.TermsOfUseURL != nil && !validHTTPSURL(*m.TermsOfUseURL) {
		return ErrManifestTermsOfUseURLInvalid
	}

	if m.PrivacyPolicyURL != nil && !validHTTPSURL(*m.PrivacyPolicyURL) {
		return ErrManifestPrivacyPolicyURLInvalid
	}

	return nil
}

func validHTTPSURL(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > maxURLLength {
		return false
	}

	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return false
	}

	return parsed.Scheme == "https" &&
		parsed.Host != "" &&
		parsed.User == nil &&
		parsed.Fragment == ""
}
