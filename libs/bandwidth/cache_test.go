// SPDX-License-Identifier: Apache-2.0

package bandwidth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEntry(t *testing.T, home string, e cacheEntry) {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, cacheFileName), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func readEntry(t *testing.T, home string) *cacheEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, cacheFileName))
	if err != nil {
		t.Fatal(err)
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	return &e
}

// A measurable link, for the cases that need the prober to succeed.
func goodProber() *fakeProber {
	return &fakeProber{servers: []fakeServer{
		srv("a", "A", "One", 10*time.Millisecond, 900, 890),
		srv("b", "B", "Two", 20*time.Millisecond, 880, 870),
	}}
}

// Rules: [CT-12].
func TestMeasurementIsStoredAndReused(t *testing.T) {
	home := t.TempDir()
	p := goodProber()
	defer stubProber(func(Options) prober { return p })()
	opts := Options{IP: "203.0.113.10", Home: home, Logger: &recorder{}}

	first := Measure(context.Background(), opts)
	if first.Source != SourceSpeedtest || len(p.measured) != 2 {
		t.Fatalf("first run: %+v, measured %v", first, p.measured)
	}
	e := readEntry(t, home)
	if e.IP != opts.IP || e.Upload != first.Upload || e.Download != first.Download || e.MeasuredAt.IsZero() {
		t.Fatalf("stored %+v", e)
	}

	// Same host, within the week: no probing at all.
	p.measured = nil
	log := &recorder{}
	opts.Logger = log
	second := Measure(context.Background(), opts)
	if len(p.measured) != 0 {
		t.Fatalf("second run probed %v", p.measured)
	}
	if *second != *first {
		t.Fatalf("second run %+v, want %+v", second, first)
	}
	if !log.has(log.infos, "Reusing") {
		t.Fatalf("reuse not logged: %v", log.infos)
	}
}

// Rules: [CT-12].
func TestStaleOrForeignCacheIsMeasuredAgain(t *testing.T) {
	cases := []struct {
		name  string
		entry cacheEntry
	}{
		{"older than the TTL", cacheEntry{IP: "203.0.113.10", Upload: 1, Download: 1, MeasuredAt: time.Now().Add(-cacheTTL - time.Hour)}},
		{"another address", cacheEntry{IP: "198.51.100.7", Upload: 1, Download: 1, MeasuredAt: time.Now()}},
	}
	for _, c := range cases {
		home := t.TempDir()
		writeEntry(t, home, c.entry)
		p := goodProber()
		restore := stubProber(func(Options) prober { return p })

		res := Measure(context.Background(), Options{IP: "203.0.113.10", Home: home})
		restore()

		if len(p.measured) == 0 {
			t.Errorf("%s: not measured again", c.name)
		}
		if res.Source != SourceSpeedtest || res.Download == 1 {
			t.Errorf("%s: got %+v", c.name, res)
		}
		if e := readEntry(t, home); e.IP != "203.0.113.10" || e.Download == 1 {
			t.Errorf("%s: cache not rewritten: %+v", c.name, e)
		}
	}
}

// A restart during a speed-test outage must not turn a fast node into one
// that advertises nothing: an old measurement for the same address is reused,
// however old, and the operator is told.
//
// Rules: [CT-12].
func TestFailedMeasurementFallsBackToOldCache(t *testing.T) {
	home := t.TempDir()
	old := cacheEntry{IP: "203.0.113.10", Upload: 111, Download: 222, MeasuredAt: time.Now().Add(-30 * 24 * time.Hour)}
	writeEntry(t, home, old)
	defer stubProber(func(Options) prober { return &fakeProber{listErr: context.DeadlineExceeded} })()
	log := &recorder{}

	res := Measure(context.Background(), Options{IP: "203.0.113.10", Home: home, Logger: log})

	if res.Source != SourceSpeedtest || res.Upload != 111 || res.Download != 222 {
		t.Fatalf("got %+v", res)
	}
	if !log.has(log.errors, "measured earlier") {
		t.Fatalf("fallback not explained: %v", log.errors)
	}
	// The old entry is kept as it was.
	if e := readEntry(t, home); e.Upload != 111 {
		t.Fatalf("cache overwritten: %+v", e)
	}
}

// Rules: [CT-12].
func TestFailedMeasurementWithoutCacheGivesNone(t *testing.T) {
	defer stubProber(func(Options) prober { return &fakeProber{listErr: context.DeadlineExceeded} })()
	log := &recorder{}

	res := Measure(context.Background(), Options{IP: "203.0.113.10", Home: t.TempDir(), Logger: log})

	if res.Source != SourceNone || res.Upload != 0 || res.Download != 0 {
		t.Fatalf("got %+v", res)
	}
	if !log.has(log.errors, "[bandwidth]") {
		t.Fatalf("remedy not named: %v", log.errors)
	}
}

// Rules: [CT-12].
func TestCorruptCacheIsIgnored(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, cacheFileName), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	p := goodProber()
	defer stubProber(func(Options) prober { return p })()
	log := &recorder{}

	res := Measure(context.Background(), Options{IP: "203.0.113.10", Home: home, Logger: log})

	if res.Source != SourceSpeedtest || len(p.measured) == 0 {
		t.Fatalf("got %+v, measured %v", res, p.measured)
	}
	if !log.has(log.errors, "unreadable") {
		t.Fatalf("corruption not logged: %v", log.errors)
	}
	if e := readEntry(t, home); e.Download != res.Download {
		t.Fatalf("cache not replaced: %+v", e)
	}
}

// Rules: [CT-12].
func TestDeclaredLinkLeavesCacheAlone(t *testing.T) {
	home := t.TempDir()
	defer stubProber(func(Options) prober { return goodProber() })()

	Measure(context.Background(), Options{UploadMbps: 100, DownloadMbps: 100, IP: "203.0.113.10", Home: home})

	if _, err := os.Stat(filepath.Join(home, cacheFileName)); !os.IsNotExist(err) {
		t.Fatalf("cache file written by the declared path: %v", err)
	}
}

// Rules: [CT-12].
func TestNoHomeDisablesCache(t *testing.T) {
	p := goodProber()
	defer stubProber(func(Options) prober { return p })()

	res := Measure(context.Background(), Options{IP: "203.0.113.10"})

	if res.Source != SourceSpeedtest || len(p.measured) == 0 {
		t.Fatalf("got %+v", res)
	}
}
