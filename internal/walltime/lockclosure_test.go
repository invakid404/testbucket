package walltime

import (
	"strings"
	"testing"
)

const npmLockFixture = `{
  "name": "mandel",
  "lockfileVersion": 3,
  "packages": {
    "": {"name": "mandel"},
    "node_modules/vitest": {"version": "4.1.10", "integrity": "sha512-vitest"},
    "node_modules/@vitest/runner": {"version": "4.1.10", "integrity": "sha512-runner"},
    "node_modules/tinyrainbow": {"version": "2.0.0", "integrity": "sha512-rainbow"}
  }
}`

// TestTheLockClosureIsDerivedFromTheLockfileBytes proves the closure is READ
// from the lock rather than taken from the receipt: both supported parsers
// resolve names, versions and integrities out of the bound bytes, including a
// scoped name and a pnpm peer suffix.
func TestTheLockClosureIsDerivedFromTheLockfileBytes(t *testing.T) {
	for _, tc := range []struct {
		parser string
		bytes  string
		want   map[string]LockedPackage
	}{
		{LockParserNPM, npmLockFixture, map[string]LockedPackage{
			"vitest":         {Version: "4.1.10", Integrity: "sha512-vitest"},
			"@vitest/runner": {Version: "4.1.10", Integrity: "sha512-runner"},
			"tinyrainbow":    {Version: "2.0.0", Integrity: "sha512-rainbow"},
		}},
		{LockParserPNPM, testPnpmLock, map[string]LockedPackage{
			"vitest":         {Version: "4.1.10", Integrity: "sha512-vitest"},
			"@vitest/runner": {Version: "4.1.10", Integrity: "sha512-runner"},
			"@vitest/expect": {Version: "4.1.10", Integrity: "sha512-expect"},
			"tinyrainbow":    {Version: "2.0.0", Integrity: "sha512-tinyrainbow"},
		}},
	} {
		t.Run(tc.parser, func(t *testing.T) {
			got, err := DeriveLockClosure(tc.parser, []byte(tc.bytes))
			if err != nil {
				t.Fatalf("DeriveLockClosure: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("derived %v, want %v", got, tc.want)
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Errorf("%s derived %+v, want %+v", name, got[name], want)
				}
			}
		})
	}
}

// TestAnUnimplementedLockParserIsRefused: a receipt naming a parser nobody
// here can run leaves the closure exactly as unchecked as a bare digest, so it
// is a refusal rather than a pass.
func TestAnUnimplementedLockParserIsRefused(t *testing.T) {
	if _, err := DeriveLockClosure("yarn-berry", []byte(npmLockFixture)); err == nil {
		t.Fatal("an unimplemented lock parser derived a closure")
	}
	if _, err := DeriveLockClosure(LockParserNPM, nil); err == nil {
		t.Fatal("an absent lockfile derived a closure")
	}
}

// TestTheSourceProfileClosureIsCheckedAgainstTheLock is the F6 regression for
// the source profile. Each case is the accepted receipt with exactly one
// disagreement between what it DECLARES and what the bound lockfile bytes
// actually say. Every one of them used to validate, because the closure was
// only ever checked against itself.
func TestTheSourceProfileClosureIsCheckedAgainstTheLock(t *testing.T) {
	cases := []struct {
		name string
		edit func(*SourceProfileReceipt)
		want string
	}{
		{"no lockfile bytes", func(r *SourceProfileReceipt) { r.LockfileBytes = nil }, "exact lockfile bytes are not bound"},
		{"no façade bytes", func(r *SourceProfileReceipt) { r.FacadeBytes = nil }, "exact façade bytes are not bound"},
		{"lockfile bytes that are not the ones the digest names", func(r *SourceProfileReceipt) {
			r.LockfileBytes = append(append([]byte(nil), r.LockfileBytes...), '\n')
		}, "digest to"},
		{"a parser this verifier cannot run", func(r *SourceProfileReceipt) {
			r.ParserID.Name = "yarn-berry"
		}, "not one this verifier can re-derive"},
		{"a package the lock does not resolve", func(r *SourceProfileReceipt) {
			r.Packages["@vitest/invented"] = RequiredVitest
			r.Integrities["@vitest/invented"] = "sha512-invented"
		}, "which the bound lockfile does not resolve"},
		{"a version the lock disagrees with", func(r *SourceProfileReceipt) {
			r.Packages["tinyrainbow"] = "2.0.0"
			r.Integrities["tinyrainbow"] = "sha512-tinyrainbow"
			r.Packages["vitest"] = "4.1.11"
		}, "the bound lockfile resolves"},
		{"an integrity the lock disagrees with", func(r *SourceProfileReceipt) {
			r.Integrities["vitest"] = "sha512-somethingelse"
		}, "but the bound lockfile records"},
		{"a Vitest-family package the closure omits", func(r *SourceProfileReceipt) {
			delete(r.Packages, "@vitest/expect")
			delete(r.Integrities, "@vitest/expect")
		}, "the declared closure omits it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := testSourceProfile()
			if err := r.Validate(); err != nil {
				t.Fatalf("the unedited receipt does not validate: %v", err)
			}
			tc.edit(&r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("the receipt validated with %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestAWrongVitestVersionInTheLockStartsANewEpoch: the version rule is applied
// to the DERIVED closure, so a receipt that declares 4.1.10 over a lock that
// resolves something else is refused rather than believed.
func TestAWrongVitestVersionInTheLockStartsANewEpoch(t *testing.T) {
	r := testSourceProfile()
	lock := strings.Replace(testPnpmLock, "vitest@4.1.10:", "vitest@4.1.11:", 1)
	r.LockfileBytes = []byte(lock)
	r.Lockfile = DigestBytes(r.LockfileBytes)
	r.Packages["vitest"] = "4.1.11"
	err := r.Validate()
	if err == nil {
		t.Fatal("a closure resolving the wrong Vitest version validated")
	}
	if !strings.Contains(err.Error(), "new source-inventory epoch") {
		t.Errorf("error %q does not name the epoch rule", err)
	}
}
