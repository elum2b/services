package target

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
)

type Context struct {
	IsPremium  bool
	Sex        string
	Country    string
	Locale     string
	Platform   string
	PlatformID int64
}

type Rules struct {
	IsPremium   *bool    `json:"is_premium"`
	Sex         []string `json:"sex"`
	Country     []string `json:"country"`
	Countries   []string `json:"countries"`
	Loc         []string `json:"loc"`
	Locale      []string `json:"locale"`
	Locales     []string `json:"locales"`
	Platform    []string `json:"platform"`
	PlatformID  []string `json:"platform_id"`
	PlatformIDs []string `json:"platform_ids"`
}

func Match(raw json.RawMessage, ctx Context) bool {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return true
	}
	var rules Rules
	if err := json.Unmarshal(raw, &rules); err != nil {
		return false
	}
	if rules.IsPremium != nil && *rules.IsPremium != ctx.IsPremium {
		return false
	}
	if len(rules.Sex) > 0 && !containsFold(rules.Sex, ctx.Sex) {
		return false
	}
	countries := append(append([]string{}, rules.Country...), rules.Countries...)
	if len(countries) > 0 && !containsFold(countries, ctx.Country) {
		return false
	}
	locales := append(append(append([]string{}, rules.Loc...), rules.Locale...), rules.Locales...)
	if len(locales) > 0 && !containsFold(locales, ctx.Locale) {
		return false
	}
	if len(rules.Platform) > 0 && !matchesPlatform(rules.Platform, ctx) {
		return false
	}
	platformIDs := append(append([]string{}, rules.PlatformID...), rules.PlatformIDs...)
	if len(platformIDs) > 0 && !matchesPlatformID(platformIDs, ctx.PlatformID) {
		return false
	}
	return true
}

func Validate(raw json.RawMessage) error {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}

	var rules Rules
	return json.Unmarshal(raw, &rules)
}

func (r *Rules) UnmarshalJSON(data []byte) error {
	type rawRules struct {
		IsPremium   *bool           `json:"is_premium"`
		Sex         json.RawMessage `json:"sex"`
		Country     json.RawMessage `json:"country"`
		Countries   json.RawMessage `json:"countries"`
		Loc         json.RawMessage `json:"loc"`
		Locale      json.RawMessage `json:"locale"`
		Locales     json.RawMessage `json:"locales"`
		Platform    json.RawMessage `json:"platform"`
		PlatformID  json.RawMessage `json:"platform_id"`
		PlatformIDs json.RawMessage `json:"platform_ids"`
	}
	var raw rawRules
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for key := range fields {
		if !isRuleKey(key) {
			return fmt.Errorf("target: unsupported field %q", key)
		}
	}

	sex, err := stringList(raw.Sex)
	if err != nil {
		return fmt.Errorf("target.sex: %w", err)
	}
	country, err := stringList(raw.Country)
	if err != nil {
		return fmt.Errorf("target.country: %w", err)
	}
	countries, err := stringList(raw.Countries)
	if err != nil {
		return fmt.Errorf("target.countries: %w", err)
	}
	loc, err := stringList(raw.Loc)
	if err != nil {
		return fmt.Errorf("target.loc: %w", err)
	}
	locale, err := stringList(raw.Locale)
	if err != nil {
		return fmt.Errorf("target.locale: %w", err)
	}
	locales, err := stringList(raw.Locales)
	if err != nil {
		return fmt.Errorf("target.locales: %w", err)
	}
	platform, err := stringList(raw.Platform)
	if err != nil {
		return fmt.Errorf("target.platform: %w", err)
	}
	platformID, err := stringList(raw.PlatformID)
	if err != nil {
		return fmt.Errorf("target.platform_id: %w", err)
	}
	platformIDs, err := stringList(raw.PlatformIDs)
	if err != nil {
		return fmt.Errorf("target.platform_ids: %w", err)
	}

	r.IsPremium = raw.IsPremium
	r.Sex = sex
	r.Country = country
	r.Countries = countries
	r.Loc = loc
	r.Locale = locale
	r.Locales = locales
	r.Platform = platform
	r.PlatformID = platformID
	r.PlatformIDs = platformIDs
	return nil
}

func isRuleKey(key string) bool {
	switch key {
	case "is_premium", "sex", "country", "countries", "loc", "locale", "locales", "platform", "platform_id", "platform_ids":
		return true
	default:
		return false
	}
}

func stringList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == "" {
			return nil, errors.New("value must not be empty")
		}
		return []string{single}, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return []string{number.String()}, nil
	}
	var rawList []json.RawMessage
	if err := json.Unmarshal(raw, &rawList); err != nil {
		return nil, errors.New("value must be a string, number, or array")
	}
	out := make([]string, 0, len(rawList))
	for index, item := range rawList {
		var value string
		if err := json.Unmarshal(item, &value); err != nil {
			var number json.Number
			if err := json.Unmarshal(item, &number); err == nil {
				value = number.String()
			} else {
				return nil, fmt.Errorf("item %d must be a string or number", index)
			}
		}
		if value == "" {
			return nil, fmt.Errorf("item %d must not be empty", index)
		}
		out = append(out, value)
	}
	return out, nil
}

func containsFold(values []string, target string) bool {
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func matchesPlatform(values []string, ctx Context) bool {
	if ctx.Platform != "" && containsFold(values, ctx.Platform) {
		return true
	}
	if matchesPlatformID(values, ctx.PlatformID) {
		return true
	}
	return false
}

func matchesPlatformID(values []string, platformID int64) bool {
	if platformID != 0 && containsFold(values, strconv.FormatInt(platformID, 10)) {
		return true
	}
	return false
}
