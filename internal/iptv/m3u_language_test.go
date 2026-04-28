package iptv

import (
	"testing"
)

// Empty allowlist must short-circuit to keep-everything (the no-
// filter contract that legacy libraries rely on).
func TestMatchesLanguageFilter_NoFilterKeepsAll(t *testing.T) {
	cases := []M3UChannel{
		{Name: "TVE 1", Language: "es"},
		{Name: "Random", GroupName: "Random Group"},
		{Name: "ZZZ", Country: "ru"},
		{Name: "Untagged"},
	}
	for _, ch := range cases {
		if !MatchesLanguageFilter(ch, nil) {
			t.Errorf("nil allowlist should keep %q", ch.Name)
		}
		if !MatchesLanguageFilter(ch, []string{}) {
			t.Errorf("empty allowlist should keep %q", ch.Name)
		}
		if !MatchesLanguageFilter(ch, []string{""}) {
			t.Errorf("blank-only allowlist should keep %q", ch.Name)
		}
	}
}

// Heuristic 1: tvg-language with the ISO code wins immediately.
func TestMatchesLanguageFilter_TVGLanguageISOCode(t *testing.T) {
	ch := M3UChannel{Name: "TVE 1", Language: "es"}
	if !MatchesLanguageFilter(ch, []string{"es"}) {
		t.Error("tvg-language=es with allow=[es] should match")
	}
	if MatchesLanguageFilter(ch, []string{"en"}) {
		t.Error("tvg-language=es with allow=[en] should not match")
	}
}

// Heuristic 1 also accepts the English name ("Spanish") because
// some feeds tag languages in human form.
func TestMatchesLanguageFilter_TVGLanguageHumanName(t *testing.T) {
	ch := M3UChannel{Name: "Telecinco", Language: "Spanish"}
	if !MatchesLanguageFilter(ch, []string{"es"}) {
		t.Error("tvg-language=Spanish should map to es")
	}
}

// Heuristic 1 splits multi-language tokens.
func TestMatchesLanguageFilter_MultiLanguageTokenAnyMatches(t *testing.T) {
	ch := M3UChannel{Name: "Multilingual", Language: "Spanish, English"}
	if !MatchesLanguageFilter(ch, []string{"en"}) {
		t.Error("any token in tvg-language should be enough")
	}
}

// Heuristic 2: tvg-country resolves to the dominant language for
// unambiguous countries.
func TestMatchesLanguageFilter_CountryMappedToLanguage(t *testing.T) {
	tests := []struct {
		country string
		allow   string
		want    bool
	}{
		{"es", "es", true},
		{"mx", "es", true}, // Mexico → Spanish
		{"ar", "es", true}, // Argentina → Spanish
		{"br", "pt", true}, // Brazil → Portuguese
		{"us", "en", true},
		{"fr", "en", false}, // France is not English
		// Multi-language countries are NOT in the table — fall through.
		{"ch", "de", false}, // Switzerland: no mapping → no signal here
	}
	for _, tc := range tests {
		ch := M3UChannel{Name: "X", Country: tc.country}
		got := MatchesLanguageFilter(ch, []string{tc.allow})
		if got != tc.want {
			t.Errorf("country=%s allow=%s got=%v want=%v",
				tc.country, tc.allow, got, tc.want)
		}
	}
}

// Heuristic 3: group-title keywords. Threadfin/xTeVe-style — the
// most common pattern in real-world Xtream feeds where neither
// tvg-language nor tvg-country is set.
func TestMatchesLanguageFilter_GroupTitleKeyword(t *testing.T) {
	tests := []struct {
		group string
		allow string
		want  bool
	}{
		{"ES | Cine", "es", true},
		{"Spain HD", "es", true},
		{"España SD", "es", true},
		{"LATINO", "es", true},
		{"UK Sports", "en", true},
		{"USA - News", "en", true},
		{"France 24h", "fr", true},
		{"Germany HD", "de", true},
		{"Italia News", "it", true},
		{"Brasil HD", "pt", true},
		{"Russia Today", "ru", true},
		// Negative: random groups must not match by accident.
		{"Best of HD", "es", false}, // 'es' substring inside 'best' must not trip
		{"Premium Channels", "en", false},
		{"News 24", "es", false},
	}
	for _, tc := range tests {
		ch := M3UChannel{Name: "x", GroupName: tc.group}
		got := MatchesLanguageFilter(ch, []string{tc.allow})
		if got != tc.want {
			t.Errorf("group=%q allow=%q got=%v want=%v",
				tc.group, tc.allow, got, tc.want)
		}
	}
}

// Heuristic 4: name prefix. Drives results when the only signal is
// in the visible channel name (very common in dirty feeds).
func TestMatchesLanguageFilter_NamePrefix(t *testing.T) {
	tests := []struct {
		name  string
		allow string
		want  bool
	}{
		{"[ES] CanalSur HD", "es", true},
		{"(ES) Antena 3", "es", true},
		{"ES | TVE 1", "es", true},
		{"ES: La Sexta", "es", true},
		{"ES - Telecinco", "es", true},
		{"ES. Cuatro", "es", true},
		{"FR | TF1", "fr", true},
		{"[EN] BBC One", "en", true},
		// Wrong language requested.
		{"[ES] CanalSur HD", "en", false},
		// No prefix at all → no signal, falls through to "allow"
		// because there's no other signal either.
		{"Just a Name", "es", true},
	}
	for _, tc := range tests {
		ch := M3UChannel{Name: tc.name}
		got := MatchesLanguageFilter(ch, []string{tc.allow})
		if got != tc.want {
			t.Errorf("name=%q allow=%q got=%v want=%v",
				tc.name, tc.allow, got, tc.want)
		}
	}
}

// Channels with NO signal whatsoever pass through (deny-only-when-
// sure rule). Dropping every untagged channel would be too
// aggressive on feeds with no metadata.
func TestMatchesLanguageFilter_NoSignalAllowsThrough(t *testing.T) {
	ch := M3UChannel{Name: "MysteryChannel"}
	if !MatchesLanguageFilter(ch, []string{"es"}) {
		t.Error("channel with zero language signal should be allowed")
	}
}

// Channels WITH a signal that doesn't match are dropped. This is
// the contract that gives the filter its value.
func TestMatchesLanguageFilter_WrongLanguageWithSignalIsDropped(t *testing.T) {
	tests := []struct {
		name string
		ch   M3UChannel
	}{
		{"tvg-language", M3UChannel{Name: "x", Language: "Russian"}},
		{"tvg-country", M3UChannel{Name: "x", Country: "ru"}},
		{"group-title", M3UChannel{Name: "x", GroupName: "Russia HD"}},
		{"name prefix", M3UChannel{Name: "[RU] Channel One"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if MatchesLanguageFilter(tc.ch, []string{"es"}) {
				t.Errorf("Russian-tagged channel should be dropped when filter=es")
			}
		})
	}
}

// Multi-language allowlist: any of the allowed codes is enough.
func TestMatchesLanguageFilter_MultiAllowAnyMatches(t *testing.T) {
	ch := M3UChannel{Name: "TVE 1", Language: "es"}
	if !MatchesLanguageFilter(ch, []string{"en", "es", "fr"}) {
		t.Error("any allowed code should be enough")
	}
}

// extractNamePrefixCode and namePrefixRE tested directly at the
// boundary because the regex has historically been the most
// accident-prone part of language heuristics.
func TestExtractNamePrefixCode(t *testing.T) {
	tests := []struct {
		in        string
		want      bool
		wantCode  string
	}{
		{"[ES] Channel", true, "es"},
		{"(en) Channel", true, "en"},
		{"FR | Foo", true, "fr"},
		{"DE: Foo", true, "de"},
		{"PT - Foo", true, "pt"},
		{"PT. Foo", true, "pt"},
		{"  [es] Channel", true, "es"}, // leading whitespace tolerated
		// Negatives: no prefix.
		{"Channel without prefix", false, ""},
		{"Foo BAR baz", false, ""},
		{"BarES", false, ""}, // 'ES' not at start
		// Negative: 4-letter "prefix" is too long.
		{"SPAIN | Foo", false, ""},
	}
	for _, tc := range tests {
		gotHit, gotCode := extractNamePrefixCode(tc.in)
		if gotHit != tc.want || gotCode != tc.wantCode {
			t.Errorf("extractNamePrefixCode(%q) = (%v, %q), want (%v, %q)",
				tc.in, gotHit, gotCode, tc.want, tc.wantCode)
		}
	}
}
