package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shim writes an executable that answers `--version`, so the resolver meets a
// real file rather than a stub it cannot interrogate.
func shim(t *testing.T, path, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+version+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestTheClosureBindsTheProgramThatActuallyLaunchesTheFacade is the F5
// regression.
//
// The frozen Mandel façade is `pnpm exec tsx scripts/tb-vitest.ts`. The
// resolver bound argv[0] and reported success, so the closure named pnpm,
// node and npm — and never the package-selected `tsx` shim, which is the
// program that actually launches the TypeScript façade. "The exact resolved
// executable path" has to mean the executable that runs.
func TestTheClosureBindsTheProgramThatActuallyLaunchesTheFacade(t *testing.T) {
	root := t.TempDir()
	shim(t, filepath.Join(root, "node_modules", ".bin", "tsx"), "tsx v4.23.1")
	bin := t.TempDir()
	for _, p := range []struct{ name, version string }{
		{"pnpm", "9.0.0"}, {"node", "v24.19.0"}, {"npm", "11.0.0"},
	} {
		shim(t, filepath.Join(bin, p.name), p.version)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	execs, tools, err := closureResolver(root, nil)([]string{"pnpm", "exec", "tsx", "scripts/tb-vitest.ts"})
	if err != nil {
		t.Fatalf("closureResolver: %v", err)
	}
	path, ok := execs["tsx"]
	if !ok {
		t.Fatalf("the resolved executables omit the pnpm-executed tsx shim: %v", execs)
	}
	// Resolved where the package manager looks, not from PATH: a shim in the
	// project's own node_modules is a different file from any tsx that
	// happens to be installed globally.
	if want := filepath.Join(root, "node_modules", ".bin", "tsx"); path != want {
		t.Errorf("tsx resolved to %s, want the package-selected %s", path, want)
	}
	tool, ok := tools["tsx"]
	if !ok {
		t.Fatalf("the tool closure omits tsx: %v", tools)
	}
	if tool.Path != path || tool.Version == "" || tool.Integrity == "" {
		t.Errorf("the tsx tool identity is incomplete: %+v", tool)
	}
	// The launcher itself is still bound, and so is the toolchain.
	for _, name := range []string{"pnpm", "node", "npm"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("the tool closure omits %s: %v", name, tools)
		}
	}
}

// TestAnUnresolvableDelegatedProgramFailsClosed: a launcher whose selected
// executable cannot be found would run something the bundle could not name, so
// it is a refusal rather than a closure that quietly describes the launcher
// alone.
func TestAnUnresolvableDelegatedProgramFailsClosed(t *testing.T) {
	root := t.TempDir()
	bin := t.TempDir()
	for _, p := range []struct{ name, version string }{
		{"pnpm", "9.0.0"}, {"node", "v24.19.0"}, {"npm", "11.0.0"},
	} {
		shim(t, filepath.Join(bin, p.name), p.version)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, _, err := closureResolver(root, nil)([]string{"pnpm", "exec", "definitely-not-installed", "x.ts"})
	if err == nil {
		t.Fatal("a closure was returned for a launcher whose selected executable does not resolve")
	}
	if !strings.Contains(err.Error(), "definitely-not-installed") {
		t.Errorf("error %q does not name the unresolved program", err)
	}
}

// TestAnOrdinaryCommandDelegatesToNothing: the delegation rule must not invent
// a second program for a command that has none, or every ordinary argv would
// acquire a spurious binding.
func TestAnOrdinaryCommandDelegatesToNothing(t *testing.T) {
	root := t.TempDir()
	bin := t.TempDir()
	for _, p := range []struct{ name, version string }{
		{"npx", "11.0.0"}, {"node", "v24.19.0"}, {"npm", "11.0.0"}, {"vitest", "4.1.10"},
	} {
		shim(t, filepath.Join(bin, p.name), p.version)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// `vitest list --json` is not a launcher; nothing is delegated.
	execs, _, err := closureResolver(root, nil)([]string{"vitest", "list", "--filesOnly", "--json"})
	if err != nil {
		t.Fatalf("closureResolver: %v", err)
	}
	if len(execs) != 1 {
		t.Errorf("an ordinary command resolved %d executables: %v", len(execs), execs)
	}
	// `npx vitest` IS a launcher, and delegates to vitest.
	execs, tools, err := closureResolver(root, nil)([]string{"npx", "vitest", "list", "--json"})
	if err != nil {
		t.Fatalf("closureResolver: %v", err)
	}
	if _, ok := execs["vitest"]; !ok {
		t.Errorf("npx did not delegate to vitest: %v", execs)
	}
	if _, ok := tools["vitest"]; !ok {
		t.Errorf("the tool closure omits the npx-selected vitest: %v", tools)
	}
}
