package iptv

// PublicCountry represents a country with available public IPTV channels.
type PublicCountry struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Flag string `json:"flag"`
}

// M3UURL returns the iptv-org M3U playlist URL for this country.
func (c PublicCountry) M3UURL() string {
	return "https://iptv-org.github.io/iptv/countries/" + c.Code + ".m3u"
}

// PublicCountries returns the list of countries with known public IPTV streams.
func PublicCountries() []PublicCountry {
	return []PublicCountry{
		{Code: "ar", Name: "Argentina", Flag: "AR"},
		{Code: "au", Name: "Australia", Flag: "AU"},
		{Code: "at", Name: "Austria", Flag: "AT"},
		{Code: "be", Name: "Belgium", Flag: "BE"},
		{Code: "bo", Name: "Bolivia", Flag: "BO"},
		{Code: "br", Name: "Brazil", Flag: "BR"},
		{Code: "ca", Name: "Canada", Flag: "CA"},
		{Code: "cl", Name: "Chile", Flag: "CL"},
		{Code: "cn", Name: "China", Flag: "CN"},
		{Code: "co", Name: "Colombia", Flag: "CO"},
		{Code: "cr", Name: "Costa Rica", Flag: "CR"},
		{Code: "cu", Name: "Cuba", Flag: "CU"},
		{Code: "cz", Name: "Czech Republic", Flag: "CZ"},
		{Code: "dk", Name: "Denmark", Flag: "DK"},
		{Code: "do", Name: "Dominican Republic", Flag: "DO"},
		{Code: "ec", Name: "Ecuador", Flag: "EC"},
		{Code: "eg", Name: "Egypt", Flag: "EG"},
		{Code: "sv", Name: "El Salvador", Flag: "SV"},
		{Code: "fi", Name: "Finland", Flag: "FI"},
		{Code: "fr", Name: "France", Flag: "FR"},
		{Code: "de", Name: "Germany", Flag: "DE"},
		{Code: "gr", Name: "Greece", Flag: "GR"},
		{Code: "gt", Name: "Guatemala", Flag: "GT"},
		{Code: "hn", Name: "Honduras", Flag: "HN"},
		{Code: "in", Name: "India", Flag: "IN"},
		{Code: "id", Name: "Indonesia", Flag: "ID"},
		{Code: "ir", Name: "Iran", Flag: "IR"},
		{Code: "iq", Name: "Iraq", Flag: "IQ"},
		{Code: "ie", Name: "Ireland", Flag: "IE"},
		{Code: "il", Name: "Israel", Flag: "IL"},
		{Code: "it", Name: "Italy", Flag: "IT"},
		{Code: "jp", Name: "Japan", Flag: "JP"},
		{Code: "kr", Name: "South Korea", Flag: "KR"},
		{Code: "mx", Name: "Mexico", Flag: "MX"},
		{Code: "nl", Name: "Netherlands", Flag: "NL"},
		{Code: "nz", Name: "New Zealand", Flag: "NZ"},
		{Code: "ni", Name: "Nicaragua", Flag: "NI"},
		{Code: "no", Name: "Norway", Flag: "NO"},
		{Code: "pa", Name: "Panama", Flag: "PA"},
		{Code: "py", Name: "Paraguay", Flag: "PY"},
		{Code: "pe", Name: "Peru", Flag: "PE"},
		{Code: "ph", Name: "Philippines", Flag: "PH"},
		{Code: "pl", Name: "Poland", Flag: "PL"},
		{Code: "pt", Name: "Portugal", Flag: "PT"},
		{Code: "ro", Name: "Romania", Flag: "RO"},
		{Code: "ru", Name: "Russia", Flag: "RU"},
		{Code: "sa", Name: "Saudi Arabia", Flag: "SA"},
		{Code: "rs", Name: "Serbia", Flag: "RS"},
		{Code: "za", Name: "South Africa", Flag: "ZA"},
		{Code: "es", Name: "Spain", Flag: "ES"},
		{Code: "se", Name: "Sweden", Flag: "SE"},
		{Code: "ch", Name: "Switzerland", Flag: "CH"},
		{Code: "tr", Name: "Turkey", Flag: "TR"},
		{Code: "ua", Name: "Ukraine", Flag: "UA"},
		{Code: "ae", Name: "United Arab Emirates", Flag: "AE"},
		{Code: "gb", Name: "United Kingdom", Flag: "GB"},
		{Code: "us", Name: "United States", Flag: "US"},
		{Code: "uy", Name: "Uruguay", Flag: "UY"},
		{Code: "ve", Name: "Venezuela", Flag: "VE"},
		{Code: "int", Name: "International", Flag: "UN"},
	}
}

// FindCountry looks up a country by its code.
func FindCountry(code string) (PublicCountry, bool) {
	for _, c := range PublicCountries() {
		if c.Code == code {
			return c, true
		}
	}
	return PublicCountry{}, false
}
