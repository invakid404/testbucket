package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/invakid404/testbucket/internal/walltime"
)

// runWallStage1Binary prints the binary digest a Stage-1 manifest AUTHORISES.
//
// The installer refuses a pre-publication candidate whose binary digest it was
// not told in advance, which is the right rule and was, until now, unusable:
// nothing in the shipped delivery path produced the value. A caller could only
// type it in, and a digest the caller chose says which bytes the caller wanted
// to run — not which bytes an authority approved.
//
// So the value is DERIVED here, from the one document that is already the
// authority on it. Validating the manifest verifies the builder's attestation
// for the delivered binary against the manifest's own predeclared builder
// keys, requires a second party's countersignature over the same subject, and
// requires the delivered binary, the attested subject and the approved
// physical wrapper to be one digest. Verifying the manifest's own signature
// says the protected campaign authority approved that set. What this command
// prints is therefore the binary the campaign authorised, and the installer's
// re-digest of the extracted bytes is what closes the delivery.
//
// It runs from a TRUSTED testbucket — a published, checksum-verified release —
// before the candidate is installed. Deriving the candidate's own approval
// with the candidate would be the delivery vouching for itself.
func runWallStage1Binary(args []string) error {
	fs := flag.NewFlagSet("wall stage1-binary", flag.ExitOnError)
	file := fs.String("file", "", "the signed Stage-1 input manifest to read (required)")
	authority := fs.String("authority", walltime.CampaignAuthority, "the protected environment that must have approved these inputs")
	var authorityKeys stringList
	fs.Var(&authorityKeys, "authority-key", "a PREDECLARED public key (hex) allowed to sign the manifest; repeatable and required. A manifest verified against whatever signed it is one anybody can mint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*file) == "" {
		return fmt.Errorf("wall stage1-binary needs --file, the signed Stage-1 manifest that approves the delivered binary")
	}
	if len(authorityKeys) == 0 {
		return fmt.Errorf("wall stage1-binary needs at least one --authority-key: a manifest checked against the key it carries authorises whatever signed it")
	}
	if strings.TrimSpace(*authority) == "" {
		return fmt.Errorf("wall stage1-binary needs --authority, the protected environment that must have approved these inputs; a manifest approved under any label would otherwise pass")
	}

	var m walltime.Stage1Manifest
	if err := walltime.ReadJSONFile(*file, &m); err != nil {
		return fmt.Errorf("read the Stage-1 manifest: %w", err)
	}
	// Validate FIRST: it is what verifies the build attestation, so a manifest
	// whose delivered binary nobody attested never reaches the printing below.
	if err := m.Validate(); err != nil {
		return fmt.Errorf("the Stage-1 manifest does not authorise a delivery: %w", err)
	}
	if m.Signature == nil {
		return fmt.Errorf("the Stage-1 manifest is unsigned; only the protected campaign authority may authorise a delivered binary")
	}
	d, err := m.DigestOf()
	if err != nil {
		return fmt.Errorf("digest the Stage-1 manifest: %w", err)
	}
	if err := walltime.VerifySigned(m.Signature, d, authorityKeys); err != nil {
		return fmt.Errorf("stage-1 authority signature: %w", err)
	}
	if m.Signature.Authority != *authority {
		return fmt.Errorf("the Stage-1 manifest names authority %q, not the expected %q", m.Signature.Authority, *authority)
	}
	if !m.Source.BinaryDigest.Valid() {
		return fmt.Errorf("the Stage-1 manifest binds delivered binary %q, which is not sha256:<64-hex>", m.Source.BinaryDigest)
	}
	fmt.Fprintln(os.Stdout, string(m.Source.BinaryDigest))
	return nil
}
