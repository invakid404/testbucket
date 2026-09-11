package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnoptedIngestStillMigratesASchema1Store is F15's control.
//
// `ingest --in <events> --store <schema-1 store>` — no wall observations, no
// plan, the Go runner, nothing about wall time requested — failed outright,
// because §15.2's migration demanded a comparability key and Save refuses to
// stamp schema 2 over an unmigrated store. That is a regression in the plain
// v0.2.2 upgrade PD-1 and §8 exist to protect.
//
// The layout migrates; `wall` stays absent, which §15.1c permits, because a wall
// object has to name the population its rows belong to and this command cannot.
func TestUnoptedIngestStillMigratesASchema1Store(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	events := filepath.Join(dir, "events.json")

	// A SCHEMA-1 STORE: the v0.2.2 layout, reporter weights and no `wall`.
	legacy := map[string]any{
		"schema": 1,
		"flags":  "go -count=1",
		"units": map[string]any{
			"./pkg/a": map[string]any{"seconds": 1.5, "samples": 3},
		},
		"coverage": []string{"./pkg/a"},
	}
	writeFixture(t, store, legacy)

	// One ordinary `go test -json` event stream. Nothing here mentions wall time.
	if err := os.WriteFile(events,
		[]byte(`{"Action":"pass","Package":"./pkg/a","Elapsed":1.25}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "ingest", "--in", events, "--store", store, "--no-golist")
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("an unopted reporter ingest over a schema-1 store failed: %v\n%s", err, out.String())
	}

	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Schema       int             `json:"schema"`
		MigratedFrom *int            `json:"migrated_from"`
		Wall         json.RawMessage `json:"wall"`
		Units        map[string]struct {
			Seconds float64 `json:"seconds"`
		} `json:"units"`
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Schema != 2 {
		t.Errorf("the saved store is schema %d, want 2: §15.2's forward step runs in ingest", saved.Schema)
	}
	if saved.MigratedFrom == nil || *saved.MigratedFrom != 1 {
		t.Errorf("migrated_from is %v, want 1: a migration that leaves no marker cannot be audited", saved.MigratedFrom)
	}
	if len(saved.Wall) != 0 && string(saved.Wall) != "null" {
		t.Errorf("the migration invented a wall object with no comparability key: %s", saved.Wall)
	}
	// AND THE REPORTER STATE SURVIVED. A migration that loses the weights is not
	// a migration.
	if u, ok := saved.Units["./pkg/a"]; !ok {
		t.Error("the migrated store lost its reporter rows")
	} else if u.Seconds <= 0 {
		t.Errorf("./pkg/a carries seconds %v after migration", u.Seconds)
	}

	t.Run("a second unopted ingest is idempotent", func(t *testing.T) {
		cmd := exec.Command(bin, "ingest", "--in", events, "--store", store, "--no-golist")
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("a second unopted ingest failed: %v\n%s", err, out.String())
		}
	})
}
