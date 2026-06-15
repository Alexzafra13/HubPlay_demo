package torrentstream

import "testing"

func src(title string, seeders int, sizeBytes int64) SearchResult {
	m := parseTitle(title)
	return SearchResult{
		Identifier:   title,
		Title:        title,
		Seeders:      seeders,
		SizeBytes:    sizeBytes,
		Resolution:   m.Resolution,
		Languages:    m.Languages,
		IsCam:        m.IsCam,
		QualityScore: m.QualityScore,
	}
}

func TestCurateDropsCams(t *testing.T) {
	in := []SearchResult{
		src("Movie 1080p WEB-DL", 10, 2e9),
		src("Movie 1080p HDCAM", 999, 1e9),
	}
	out := Curate(in, DefaultFilterOptions())
	if len(out) != 1 {
		t.Fatalf("len: got %d want 1 (cam dropped)", len(out))
	}
	if out[0].IsCam {
		t.Error("cam survived the filter")
	}
}

func TestCurateExcludeResolution(t *testing.T) {
	in := []SearchResult{
		src("Movie 2160p WEB-DL", 50, 8e9),
		src("Movie 1080p WEB-DL", 10, 2e9),
	}
	opts := DefaultFilterOptions()
	opts.ExcludeResolutions = []string{"2160p"}
	out := Curate(in, opts)
	if len(out) != 1 || out[0].Resolution != "1080p" {
		t.Fatalf("expected only 1080p, got %+v", out)
	}
}

func TestCurateMaxSize(t *testing.T) {
	in := []SearchResult{
		src("Movie 2160p", 5, 30e9), // 30 GB
		src("Movie 1080p", 5, 4e9),  // 4 GB
	}
	opts := DefaultFilterOptions()
	opts.MaxSizeGB = 10
	out := Curate(in, opts)
	if len(out) != 1 || out[0].SizeBytes != int64(4e9) {
		t.Fatalf("expected only the 4GB result, got %+v", out)
	}
}

func TestCurateSortCascade(t *testing.T) {
	in := []SearchResult{
		src("Movie 1080p WEB-DL", 100, 5e9),
		src("Movie 2160p WEB-DL", 5, 20e9),
		src("Movie 2160p WEB-DL big", 5, 25e9),
	}
	out := Curate(in, DefaultFilterOptions())
	// Quality first: both 2160p ahead of 1080p despite far fewer seeders.
	if out[0].Resolution != "2160p" || out[1].Resolution != "2160p" {
		t.Fatalf("4K should rank first: %+v", out)
	}
	// Equal quality + seeders → smaller size first.
	if out[0].SizeBytes > out[1].SizeBytes {
		t.Errorf("smaller size should win the tiebreak: %d before %d", out[0].SizeBytes, out[1].SizeBytes)
	}
	if out[2].Resolution != "1080p" {
		t.Errorf("1080p should be last: %+v", out[2])
	}
}

func TestCuratePreferredLanguage(t *testing.T) {
	in := []SearchResult{
		src("Movie 1080p WEB-DL ENGLISH", 10, 5e9),
		src("Movie 1080p WEB-DL LATINO", 10, 5e9),
	}
	opts := DefaultFilterOptions()
	opts.PreferredLanguage = "Latino"
	out := Curate(in, opts)
	if !hasLanguage(out[0], "latino") {
		t.Errorf("preferred language should sort first: %+v", out)
	}
}

func TestCurateCapPerResolution(t *testing.T) {
	in := []SearchResult{
		src("A 1080p", 50, 5e9),
		src("B 1080p", 40, 5e9),
		src("C 1080p", 30, 5e9),
		src("D 720p", 20, 3e9),
	}
	opts := DefaultFilterOptions()
	opts.MaxPerResolution = 2
	out := Curate(in, opts)
	// Top 2 of 1080p + the single 720p = 3.
	if len(out) != 3 {
		t.Fatalf("len: got %d want 3", len(out))
	}
	count1080 := 0
	for _, r := range out {
		if r.Resolution == "1080p" {
			count1080++
		}
	}
	if count1080 != 2 {
		t.Errorf("expected 2 of 1080p after cap, got %d", count1080)
	}
}
