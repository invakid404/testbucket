package main

import (
	"strings"
	"testing"
	"time"
)

// The CLI is where a consumer's contract is enforced before anything expensive
// happens, so these are the checks that must fail on a line of output rather
// than after a discovery sweep or, worse, after a store write.

func TestResolveCountKeepsEachAdapterHonest(t *testing.T) {
	cases := []struct {
		name     string
		runner   string
		count    int
		explicit bool
		want     int
		wantErr  bool
	}{
		{name: "the Go sweep default survives", runner: "go", count: 100, want: 100},
		{name: "an explicit Go sweep is honoured", runner: "go", count: 50, explicit: true, want: 50},
		{name: "Vitest defaults to one run", runner: "vitest", count: 100, want: 1},
		{name: "an explicit Vitest count of 1 is fine", runner: "vitest", count: 1, explicit: true, want: 1},
		{name: "any other Vitest sweep is refused", runner: "vitest", count: 100, explicit: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveCount(tc.runner, tc.count, tc.explicit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("count %d was accepted for %s", tc.count, tc.runner)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveCount: %v", err)
			}
			if got != tc.want {
				t.Errorf("count = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestWallDirIsVitestOnly: a flag that silently does nothing is how a consumer
// ends up believing a campaign was instrumented when it was not.
func TestWallDirIsVitestOnly(t *testing.T) {
	if err := checkWallDirRunner("vitest", "/var/tb/wall"); err != nil {
		t.Errorf("--wall-dir was refused for the Vitest adapter: %v", err)
	}
	if err := checkWallDirRunner("go", ""); err != nil {
		t.Errorf("an unset --wall-dir was refused: %v", err)
	}
	err := checkWallDirRunner("go", "/var/tb/wall")
	if err == nil {
		t.Fatalf("--wall-dir was silently ignored on the Go adapter")
	}
	if !strings.Contains(err.Error(), "--runner vitest") {
		t.Errorf("the error does not say what to do: %v", err)
	}
}

func TestDiscoveryTimeoutDefault(t *testing.T) {
	t.Setenv("TB_DISCOVERY_TIMEOUT", "")
	got, err := discoveryTimeoutDefault()
	if err != nil || got != defaultDiscoveryTimeout {
		t.Errorf("unset gave %v, %v; want %v", got, err, defaultDiscoveryTimeout)
	}
	t.Setenv("TB_DISCOVERY_TIMEOUT", "3m")
	if got, err := discoveryTimeoutDefault(); err != nil || got != 3*time.Minute {
		t.Errorf("3m gave %v, %v", got, err)
	}
	t.Setenv("TB_DISCOVERY_TIMEOUT", "-1s")
	if _, err := discoveryTimeoutDefault(); err == nil {
		t.Errorf("a negative deadline was accepted")
	}
	t.Setenv("TB_DISCOVERY_TIMEOUT", "banana")
	if _, err := discoveryTimeoutDefault(); err == nil {
		t.Errorf("a malformed deadline was accepted")
	}
}
