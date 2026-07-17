package target

import (
	"testing"

	json "github.com/goccy/go-json"
)

func TestMatchEmptyTargetAllowsAll(t *testing.T) {
	if !Match(nil, Context{Locale: "en"}) {
		t.Fatal("empty target must match")
	}
	if !Match(json.RawMessage("null"), Context{Locale: "en"}) {
		t.Fatal("null target must match")
	}
}

func TestMatchAllCriteria(t *testing.T) {
	raw := json.RawMessage(`{
		"is_premium": true,
		"sex": ["man"],
		"country": ["us", "de"],
		"loc": ["en", "ru"]
	}`)
	if !Match(raw, Context{IsPremium: true, Sex: "MAN", Country: "US", Locale: "en"}) {
		t.Fatal("matching criteria should allow")
	}
	if Match(raw, Context{IsPremium: false, Sex: "man", Country: "us", Locale: "en"}) {
		t.Fatal("premium mismatch should deny")
	}
	if Match(raw, Context{IsPremium: true, Sex: "woman", Country: "us", Locale: "en"}) {
		t.Fatal("sex mismatch should deny")
	}
	if Match(raw, Context{IsPremium: true, Sex: "man", Country: "fr", Locale: "en"}) {
		t.Fatal("country mismatch should deny")
	}
	if Match(raw, Context{IsPremium: true, Sex: "man", Country: "us", Locale: "tr"}) {
		t.Fatal("locale mismatch should deny")
	}
}

func TestMatchStringAliases(t *testing.T) {
	if !Match(json.RawMessage(`{"countries":"ru","locale":"ru"}`), Context{Country: "RU", Locale: "ru"}) {
		t.Fatal("string country and locale aliases should match")
	}
	if !Match(json.RawMessage(`{"locales":["en","ru"]}`), Context{Locale: "ru"}) {
		t.Fatal("locales alias should match")
	}
}

func TestMatchPlatform(t *testing.T) {
	if !Match(json.RawMessage(`{"platform_id":10}`), Context{PlatformID: 10}) {
		t.Fatal("numeric platform_id should match platform id")
	}
	if !Match(json.RawMessage(`{"platform_ids":[10,"20"]}`), Context{PlatformID: 20}) {
		t.Fatal("mixed platform_ids should match platform id")
	}
	if !Match(json.RawMessage(`{"platform":["vkma","tma"]}`), Context{Platform: "TMA"}) {
		t.Fatal("platform keys should match platform")
	}
	if !Match(json.RawMessage(`{"platform":10}`), Context{PlatformID: 10}) {
		t.Fatal("numeric platform should match platform id")
	}
	if Match(json.RawMessage(`{"platform":["vkma"]}`), Context{Platform: "ok", PlatformID: 10}) {
		t.Fatal("platform mismatch should deny")
	}
}

func TestMatchInvalidJSONDenies(t *testing.T) {
	if Match(json.RawMessage(`{`), Context{}) {
		t.Fatal("invalid JSON target must deny")
	}
}

func TestValidateRejectsInvalidRules(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{
			name: "object value for sex",
			raw:  json.RawMessage(`{"sex":{}}`),
		},
		{
			name: "invalid premium type",
			raw:  json.RawMessage(`{"is_premium":"yes"}`),
		},
		{
			name: "unknown field",
			raw:  json.RawMessage(`{"platforms":["tma"]}`),
		},
		{
			name: "empty list item",
			raw:  json.RawMessage(`{"country":[""]}`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Validate(test.raw); err == nil {
				t.Fatalf("target %s must be rejected", test.raw)
			}
			if Match(test.raw, Context{}) {
				t.Fatalf("invalid target %s must deny visibility", test.raw)
			}
		})
	}
}

func BenchmarkMatchEmptyTarget(b *testing.B) {
	ctx := Context{IsPremium: true, Sex: "man", Country: "us", Locale: "en", Platform: "tma", PlatformID: 10}
	for b.Loop() {
		_ = Match(nil, ctx)
	}
}

func BenchmarkMatchFullTarget(b *testing.B) {
	raw := json.RawMessage(`{
		"is_premium": true,
		"sex": ["man"],
		"country": ["us", "de"],
		"loc": ["en", "ru"],
		"platform": ["vkma", "tma"],
		"platform_ids": [10, 20]
	}`)
	ctx := Context{IsPremium: true, Sex: "man", Country: "us", Locale: "en", Platform: "tma", PlatformID: 10}
	for b.Loop() {
		_ = Match(raw, ctx)
	}
}
