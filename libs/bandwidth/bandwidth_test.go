// SPDX-License-Identifier: Apache-2.0

package bandwidth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder captures log lines so tests can assert on what the operator would see.
type recorder struct {
	mu     sync.Mutex
	debugs []string
	infos  []string
	errors []string
}

func (r *recorder) Debug(msg string, kv ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.debugs = append(r.debugs, msg+" "+fmt.Sprint(kv...))
}

func (r *recorder) Info(msg string, kv ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.infos = append(r.infos, msg+" "+fmt.Sprint(kv...))
}

func (r *recorder) Error(msg string, kv ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, msg+" "+fmt.Sprint(kv...))
}

func (r *recorder) has(lines []string, text string) bool {
	for _, l := range lines {
		if strings.Contains(l, text) {
			return true
		}
	}
	return false
}

// fakeServer is one speed-test server as the fake prober presents it.
type fakeServer struct {
	c    Candidate     // as listed; Latency is the first quick sample
	min  time.Duration // MinLatency after the accurate ping; 0 means the ping fails
	down float64       // bytes per second the download test yields
	up   float64       // bytes per second the upload test yields
	err  error         // what Measure returns
}

type fakeProber struct {
	servers  []fakeServer
	listErr  error
	refines  int
	measured []string
}

func (p *fakeProber) Candidates(context.Context) ([]Candidate, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	out := make([]Candidate, len(p.servers))
	for i, s := range p.servers {
		out[i] = s.c
	}
	return out, nil
}

func (p *fakeProber) Refine(_ context.Context, cs []Candidate) []Candidate {
	p.refines++
	out := make([]Candidate, len(cs))
	for i, c := range cs {
		out[i] = c
		for _, s := range p.servers {
			if s.c.ID == c.ID {
				out[i].MinLatency = s.min
				if s.min == 0 {
					out[i].Latency = -1
				}
			}
		}
	}
	return out
}

func (p *fakeProber) Measure(_ context.Context, c Candidate) (Sample, error) {
	p.measured = append(p.measured, c.ID)
	for _, s := range p.servers {
		if s.c.ID == c.ID {
			if s.err != nil {
				return Sample{}, s.err
			}
			return Sample{Candidate: c, Upload: s.up, Download: s.down}, nil
		}
	}
	return Sample{}, errors.New("unknown server")
}

// srv builds a reachable server whose mean latency equals its minimum unless
// told otherwise; throughput is given in Mbit/s for readability.
func srv(id, sponsor, city string, latency time.Duration, downMbps, upMbps float64) fakeServer {
	return fakeServer{
		c:    Candidate{ID: id, Name: city, Sponsor: sponsor, Country: "XX", Latency: latency},
		min:  latency,
		down: float64(BytesPerSecond(downMbps)),
		up:   float64(BytesPerSecond(upMbps)),
	}
}

func mbps(bytesPerSecond int64) float64 { return Mbps(float64(bytesPerSecond)) }

// Rules: [CT-11].
func TestBytesPerSecond(t *testing.T) {
	cases := []struct {
		mbps float64
		want int64
	}{{1000, 125_000_000}, {1, 125_000}, {0.5, 62_500}, {0, 0}}
	for _, c := range cases {
		if got := BytesPerSecond(c.mbps); got != c.want {
			t.Errorf("BytesPerSecond(%v) = %d, want %d", c.mbps, got, c.want)
		}
		if back := Mbps(float64(c.want)); back != c.mbps {
			t.Errorf("Mbps(%d) = %v, want %v", c.want, back, c.mbps)
		}
	}
}

// The case that started this: a server inside the host's own facility answers
// in well under a millisecond and reports the local network's throughput. It
// must never be measured, the operator must be told why, and the figure must
// come from the servers that are genuinely off the host's network.
//
// Rules: [CT-11].
func TestSameFacilityServerIsRejected(t *testing.T) {
	p := &fakeProber{servers: []fakeServer{
		srv("near", "Cloud Tenant", "Sametown", 500*time.Microsecond, 9000, 8000),
		srv("mid", "Carrier A", "Nearby", 15*time.Millisecond, 900, 890),
		srv("far", "Carrier B", "Further", 30*time.Millisecond, 880, 870),
	}}
	log := &recorder{}

	res := measure(context.Background(), p, log)

	for _, id := range p.measured {
		if id == "near" {
			t.Fatal("the same-facility server was measured")
		}
	}
	if !log.has(log.infos, "rejected") || !log.has(log.infos, "Sametown") {
		t.Fatalf("no rejection line naming the server; infos: %v", log.infos)
	}
	if res.Source != SourceSpeedtest {
		t.Fatalf("source %q", res.Source)
	}
	// The two off-network servers agree, so measuring stops at two; the lower
	// of the pair, per direction.
	if got := mbps(res.Download); got != 880 {
		t.Fatalf("download %v Mbit/s, want 880", got)
	}
	if got := mbps(res.Upload); got != 870 {
		t.Fatalf("upload %v Mbit/s, want 870", got)
	}
}

// Rules: [CT-11].
func TestAgreementStopsAfterTwoServers(t *testing.T) {
	p := &fakeProber{servers: []fakeServer{
		srv("a", "A", "One", 10*time.Millisecond, 950, 940),
		srv("b", "B", "Two", 20*time.Millisecond, 930, 920),
		srv("c", "C", "Three", 30*time.Millisecond, 910, 900),
	}}

	res := measure(context.Background(), p, &recorder{})

	if len(p.measured) != 2 {
		t.Fatalf("measured %v, want exactly two servers", p.measured)
	}
	if mbps(res.Download) != 930 || mbps(res.Upload) != 920 {
		t.Fatalf("got %v/%v Mbit/s", mbps(res.Download), mbps(res.Upload))
	}
}

// One server on the host's network that somehow cleared the floor reads far
// higher than the rest; taking the lowest independent figure discards it.
//
// Rules: [CT-11].
func TestDisagreementMeasuresThirdServerAndTakesLowest(t *testing.T) {
	p := &fakeProber{servers: []fakeServer{
		srv("a", "A", "One", 5*time.Millisecond, 7000, 6000),
		srv("b", "B", "Two", 20*time.Millisecond, 950, 940),
		srv("c", "C", "Three", 30*time.Millisecond, 930, 920),
	}}
	log := &recorder{}

	res := measure(context.Background(), p, log)

	if len(p.measured) != 3 {
		t.Fatalf("measured %v, want three servers", p.measured)
	}
	// The inflated server is discarded: the lowest independent figure stands.
	if mbps(res.Download) != 930 || mbps(res.Upload) != 920 {
		t.Fatalf("got %v/%v Mbit/s, want 930/920", mbps(res.Download), mbps(res.Upload))
	}
}

// A weak server pulls the figure down, not up: understating is the safe
// direction, and the operator override exists for when it matters. Here the
// first two servers disagree (400 against 940), so a third is measured, and the
// lowest of the three stands.
//
// Rules: [CT-11].
func TestWeakServerUnderstatesRatherThanOverstates(t *testing.T) {
	p := &fakeProber{servers: []fakeServer{
		srv("a", "A", "One", 10*time.Millisecond, 940, 930),
		srv("b", "B", "Two", 20*time.Millisecond, 400, 380),
		srv("c", "C", "Three", 30*time.Millisecond, 920, 910),
	}}

	res := measure(context.Background(), p, &recorder{})

	if mbps(res.Download) != 400 || mbps(res.Upload) != 380 {
		t.Fatalf("got %v/%v Mbit/s, want 400/380 (never above the lowest independent server)", mbps(res.Download), mbps(res.Upload))
	}
}

// Upload and download are combined separately, so each direction's figure may
// come from a different server.
//
// Rules: [CT-11].
func TestDirectionsAreCombinedIndependently(t *testing.T) {
	p := &fakeProber{servers: []fakeServer{
		srv("a", "A", "One", 10*time.Millisecond, 950, 300), // receives badly
		srv("b", "B", "Two", 20*time.Millisecond, 940, 900),
		srv("c", "C", "Three", 30*time.Millisecond, 500, 890), // sends badly
	}}

	res := measure(context.Background(), p, &recorder{})

	if len(p.measured) != 3 {
		t.Fatalf("measured %v, want three", p.measured)
	}
	// Each direction takes its own lowest independent figure.
	if mbps(res.Download) != 500 || mbps(res.Upload) != 300 {
		t.Fatalf("got %v/%v Mbit/s, want 500/300", mbps(res.Download), mbps(res.Upload))
	}
}

// The floor is applied to the minimum round-trip time, not the mean: jitter
// lifts a mean, but a minimum below the floor means the server is too close.
//
// Rules: [CT-11].
func TestFloorUsesMinimumLatency(t *testing.T) {
	jittery := srv("j", "Metro ISP", "Sametown", 2500*time.Microsecond, 5000, 4000)
	jittery.min = 1200 * time.Microsecond
	p := &fakeProber{servers: []fakeServer{
		jittery,
		srv("b", "B", "Two", 20*time.Millisecond, 940, 930),
		srv("c", "C", "Three", 30*time.Millisecond, 920, 910),
	}}

	res := measure(context.Background(), p, &recorder{})

	for _, id := range p.measured {
		if id == "j" {
			t.Fatal("a server with a sub-floor minimum latency was measured")
		}
	}
	if mbps(res.Download) != 920 {
		t.Fatalf("download %v Mbit/s, want 920", mbps(res.Download))
	}
}

// In a dense market the ten nearest servers can all be inside the metropolitan
// area; the next batch is refined before the floor is relaxed.
//
// Rules: [CT-11].
func TestRefinesNextBatchWhenNearestAreAllTooClose(t *testing.T) {
	var servers []fakeServer
	for i := 0; i < 12; i++ {
		servers = append(servers, srv(fmt.Sprintf("m%d", i), fmt.Sprintf("Metro %d", i), "Bigcity",
			time.Duration(500+i*100)*time.Microsecond, 8000, 7000))
	}
	servers = append(servers,
		srv("x", "X", "Elsewhere", 12*time.Millisecond, 950, 940),
		srv("y", "Y", "Faraway", 25*time.Millisecond, 930, 920),
		srv("z", "Z", "Beyond", 40*time.Millisecond, 910, 900),
	)
	p := &fakeProber{servers: servers}
	log := &recorder{}

	res := measure(context.Background(), p, log)

	if p.refines != 2 {
		t.Fatalf("refine batches %d, want 2", p.refines)
	}
	if log.has(log.errors, "relaxing") {
		t.Fatal("the floor was relaxed although further servers existed")
	}
	if mbps(res.Download) != 930 {
		t.Fatalf("download %v Mbit/s, want 930", mbps(res.Download))
	}
}

// Rules: [CT-11].
func TestOneServerPerSponsorAndCity(t *testing.T) {
	p := &fakeProber{servers: []fakeServer{
		srv("a1", "Same Corp", "Onecity", 10*time.Millisecond, 950, 940),
		srv("a2", "same corp", "onecity", 11*time.Millisecond, 950, 940),
		srv("b", "Other", "Twocity", 20*time.Millisecond, 500, 490),
		srv("c", "Third", "Threecity", 30*time.Millisecond, 940, 930),
	}}

	measure(context.Background(), p, &recorder{})

	for _, id := range p.measured {
		if id == "a2" {
			t.Fatalf("both servers of one sponsor and city were measured: %v", p.measured)
		}
	}
}

// The library reports -1 when it could not measure; that is a failure, never a
// figure.
//
// Rules: [CT-11].
func TestNoResultSampleIsSkipped(t *testing.T) {
	bad := srv("a", "A", "One", 10*time.Millisecond, 0, 0)
	bad.down, bad.up = -1, -1
	failing := srv("b", "B", "Two", 15*time.Millisecond, 0, 0)
	failing.err = errors.New("connection reset")
	p := &fakeProber{servers: []fakeServer{
		bad, failing,
		srv("c", "C", "Three", 20*time.Millisecond, 900, 890),
		srv("d", "D", "Four", 30*time.Millisecond, 880, 870),
	}}
	log := &recorder{}

	res := measure(context.Background(), p, log)

	if res.Source != SourceSpeedtest || mbps(res.Download) != 880 {
		t.Fatalf("got source %q download %v", res.Source, mbps(res.Download))
	}
	if !log.has(log.errors, "no result") || !log.has(log.errors, "connection reset") {
		t.Fatalf("failures not logged: %v", log.errors)
	}
}

// Rules: [CT-11].
func TestRelaxedFloorWhenNothingLeavesTheMetro(t *testing.T) {
	p := &fakeProber{servers: []fakeServer{
		srv("a", "A", "One", 600*time.Microsecond, 9000, 8000),
		srv("b", "B", "Two", 1200*time.Microsecond, 3000, 2900),
		srv("c", "C", "Three", 1500*time.Microsecond, 2800, 2700),
	}}
	log := &recorder{}

	res := measure(context.Background(), p, log)

	for _, id := range p.measured {
		if id == "a" {
			t.Fatal("the sub-millisecond server was measured under the relaxed floor")
		}
	}
	if res.Source != SourceSpeedtest || mbps(res.Download) != 2800 {
		t.Fatalf("got source %q download %v", res.Source, mbps(res.Download))
	}
	if !log.has(log.errors, "relaxing") {
		t.Fatalf("relaxation not logged: %v", log.errors)
	}
}

// Rules: [CT-11].
func TestRefusesWhenEveryServerIsInTheFacility(t *testing.T) {
	p := &fakeProber{servers: []fakeServer{
		srv("a", "A", "One", 300*time.Microsecond, 9000, 8000),
		srv("b", "B", "Two", 800*time.Microsecond, 9000, 8000),
	}}
	log := &recorder{}

	res := measure(context.Background(), p, log)

	if len(p.measured) != 0 {
		t.Fatalf("measured %v, want nothing", p.measured)
	}
	if res.Source != SourceNone || res.Upload != 0 || res.Download != 0 {
		t.Fatalf("got %+v", res)
	}
	if !log.has(log.errors, "inside this facility") {
		t.Fatalf("refusal not explained: %v", log.errors)
	}
}

// Rules: [CT-11].
func TestDegenerateCasesGiveNone(t *testing.T) {
	unreachable := srv("a", "A", "One", 10*time.Millisecond, 900, 900)
	unreachable.c.Latency = -1
	pingFails := srv("b", "B", "Two", 10*time.Millisecond, 900, 900)
	pingFails.min = 0
	allFail := srv("c", "C", "Three", 10*time.Millisecond, 900, 900)
	allFail.err = errors.New("timeout")

	cases := []struct {
		name string
		p    *fakeProber
		text string
	}{
		{"list fetch fails", &fakeProber{listErr: errors.New("dns")}, "server list"},
		{"empty list", &fakeProber{}, "No speed test server answered"},
		{"nothing reachable", &fakeProber{servers: []fakeServer{unreachable}}, "No speed test server answered"},
		{"accurate ping fails everywhere", &fakeProber{servers: []fakeServer{pingFails}}, "inside this facility"},
		{"every measurement fails", &fakeProber{servers: []fakeServer{allFail}}, "Every speed test failed"},
	}
	for _, c := range cases {
		log := &recorder{}
		res := measure(context.Background(), c.p, log)
		if res.Source != SourceNone || res.Upload != 0 || res.Download != 0 {
			t.Errorf("%s: got %+v", c.name, res)
		}
		if !log.has(log.errors, c.text) {
			t.Errorf("%s: expected an error line containing %q, got %v", c.name, c.text, log.errors)
		}
	}
}

// Rules: [CT-11].
func TestCancelledContextStopsMeasuring(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &fakeProber{servers: []fakeServer{
		srv("a", "A", "One", 10*time.Millisecond, 900, 900),
	}}

	res := measure(ctx, p, &recorder{})

	if len(p.measured) != 0 || res.Source != SourceNone {
		t.Fatalf("measured %v, source %q", p.measured, res.Source)
	}
}

// Rules: [CT-11].
func TestMinSample(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{{[]float64{5}, 5}, {[]float64{9, 4}, 4}, {[]float64{4, 9}, 4}, {[]float64{7000, 950, 930}, 930}, {[]float64{940, 400, 920}, 400}}
	for _, c := range cases {
		samples := make([]Sample, len(c.in))
		for i, v := range c.in {
			samples[i] = Sample{Download: v}
		}
		if got := minSample(samples, func(s Sample) float64 { return s.Download }); got != c.want {
			t.Errorf("minSample(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// Measure, the public entry point: the declared link wins without any probing.
//
// Rules: [CT-11].
func TestDeclaredLinkSkipsMeasurement(t *testing.T) {
	called := false
	defer stubProber(func(Options) prober { called = true; return &fakeProber{} })()
	log := &recorder{}

	res := Measure(context.Background(), Options{UploadMbps: 1000, DownloadMbps: 500, Logger: log})

	if called {
		t.Fatal("the prober was built although the link is declared")
	}
	if res.Source != SourceConfig || res.Upload != 125_000_000 || res.Download != 62_500_000 {
		t.Fatalf("got %+v", res)
	}
	if !log.has(log.infos, "declared") {
		t.Fatalf("not logged: %v", log.infos)
	}
}

// stubProber replaces the real prober for the duration of a test.
func stubProber(f func(Options) prober) func() {
	orig := newProber
	newProber = f
	return func() { newProber = orig }
}
