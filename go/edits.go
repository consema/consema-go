package consema

// This file implements the cross-family mandatory structural edit surface
// (RFC 0016 §5.3; RFC 0004 §10-§16; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.5
// G4.3): the root-package counterpart of the per-family
// EditTransactionBuilder. The family builders stay typed and family-owned;
// this package dispatches one union `Document` to the owning family's
// existing edit public API and closes the shared artifacts
// (document.ChangeSet, document.EditPlan, document.UntouchedByteProof,
// document.SourcePatch — RFC 0004 §13-§16) that the batch-plan records
// consume (RFC 0015 §8).
//
// The YAML family is the one dispatch gap: go/yaml publishes Commit but no
// DryRun (its Rust counterpart publishes dry_run; go/yaml G2.1 gap), so
// YAML transactions cannot produce a transferable plan through this
// package yet.

import (
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/toml"
	"consema.dev/consema/xml"
	"consema.dev/consema/yaml"
)

// EditTransaction is the closed union of family edit transactions of this
// Go milestone (RFC 0004 §13: one immutable transaction binds one base
// SnapshotIdentity). Exactly one family transaction is set; the family
// transaction types are the family-owned typed operations.
type EditTransaction struct {
	json       *jsonpkg.EditTransaction
	toml       *toml.EditTransaction
	yaml       *yaml.EditTransaction
	ini        *ini.EditTransaction
	properties *properties.EditTransaction
	xml        *xml.EditTransaction
	plist      *plist.EditTransaction
	hcl        *hcl.EditTransaction
}

// NewJSONEditTransaction wraps one JSON-family edit transaction.
func NewJSONEditTransaction(transaction *jsonpkg.EditTransaction) *EditTransaction {
	return &EditTransaction{json: transaction}
}

// NewTOMLEditTransaction wraps one TOML-family edit transaction.
func NewTOMLEditTransaction(transaction *toml.EditTransaction) *EditTransaction {
	return &EditTransaction{toml: transaction}
}

// NewYAMLEditTransaction wraps one YAML-family edit transaction.
func NewYAMLEditTransaction(transaction *yaml.EditTransaction) *EditTransaction {
	return &EditTransaction{yaml: transaction}
}

// NewINIEditTransaction wraps one INI-family edit transaction.
func NewINIEditTransaction(transaction *ini.EditTransaction) *EditTransaction {
	return &EditTransaction{ini: transaction}
}

// NewPropertiesEditTransaction wraps one Java-Properties-family edit
// transaction.
func NewPropertiesEditTransaction(transaction *properties.EditTransaction) *EditTransaction {
	return &EditTransaction{properties: transaction}
}

// NewXMLEditTransaction wraps one XML-family edit transaction.
func NewXMLEditTransaction(transaction *xml.EditTransaction) *EditTransaction {
	return &EditTransaction{xml: transaction}
}

// NewPlistEditTransaction wraps one plist-family edit transaction.
func NewPlistEditTransaction(transaction *plist.EditTransaction) *EditTransaction {
	return &EditTransaction{plist: transaction}
}

// NewHCLEditTransaction wraps one HCL-family edit transaction.
func NewHCLEditTransaction(transaction *hcl.EditTransaction) *EditTransaction {
	return &EditTransaction{hcl: transaction}
}

// EditCommit is the root-package view of one atomic edit commit over the
// shared artifacts (RFC 0004 §13-§16): the new document union, the
// complete old-to-new change facts, the portable exact raw-byte
// application facts, and the verifiable untouched-byte proof. A failure
// never returns any of these artifacts.
type EditCommit struct {
	// Document is the new immutable document in the union.
	Document *Document
	// ChangeSet carries the complete old-to-new change facts.
	ChangeSet document.ChangeSet
	// SourcePatch carries the portable exact raw-byte application facts.
	SourcePatch *document.SourcePatch
	// UntouchedProof carries verifiable evidence for every byte outside
	// the replacement set.
	UntouchedProof *document.UntouchedByteProof
}

// EditUnsupportedError is the typed failure of an edit dispatch this Go
// milestone cannot execute: the family transaction has no dry-run surface
// (the yaml family, G2.1 gap) or the union does not hold the family
// document.
type EditUnsupportedError struct {
	// Family is the format family id of the transaction.
	Family string
	// Reason explains which surface is missing.
	Reason string
}

// Error implements error; the text is human presentation only (RFC 0016
// §6).
func (e *EditUnsupportedError) Error() string {
	return fmt.Sprintf("consema: edit dispatch unsupported for %s: %s", e.Family, e.Reason)
}

// Code returns the frozen registered operation-unsupported code (RFC 0004
// §17).
func (e *EditUnsupportedError) Code() string { return "core.edit.operation-unsupported@1" }

// PlanEdit dry-runs one family edit transaction through the owning family
// and returns the shared transferable plan (document.EditPlan; RFC 0004
// §14: fully validated, byte-planning complete, never authority to write).
// The source ID is the caller-stable source identity of the plan.
//
// The YAML family has no dry-run surface in this Go milestone (its Rust
// counterpart publishes dry_run; go/yaml G2.1 gap), so YAML transactions
// fail with EditUnsupportedError.
func PlanEdit(doc *Document, transaction *EditTransaction,
	sourceID string) (*document.EditPlan, error) {
	switch {
	case transaction.json != nil:
		if doc.inner.json == nil {
			return nil, &EditUnsupportedError{Family: "json", Reason: "document union mismatch"}
		}
		plan, failure := doc.inner.json.DryRun(transaction.json, sourceID)
		if failure != nil {
			return nil, failure
		}
		return plan, nil
	case transaction.toml != nil:
		if doc.inner.toml == nil {
			return nil, &EditUnsupportedError{Family: "toml", Reason: "document union mismatch"}
		}
		source, err := document.NewEditPlanSourceId(sourceID)
		if err != nil {
			return nil, err
		}
		plan, failure := doc.inner.toml.DryRun(transaction.toml, *source)
		if failure != nil {
			return nil, failure
		}
		return plan, nil
	case transaction.yaml != nil:
		return nil, &EditUnsupportedError{Family: "yaml",
			Reason: "the yaml family has no dry-run surface in this milestone (G2.1 gap); " +
				"use the yaml package's Commit directly"}
	case transaction.ini != nil:
		if doc.inner.ini == nil {
			return nil, &EditUnsupportedError{Family: "ini", Reason: "document union mismatch"}
		}
		plan, failure := doc.inner.ini.DryRun(transaction.ini, sourceID)
		if failure != nil {
			return nil, failure
		}
		return plan, nil
	case transaction.properties != nil:
		if doc.inner.properties == nil {
			return nil, &EditUnsupportedError{Family: "java-properties",
				Reason: "document union mismatch"}
		}
		plan, failure := doc.inner.properties.DryRun(transaction.properties, sourceID)
		if failure != nil {
			return nil, failure
		}
		return plan, nil
	case transaction.xml != nil:
		if doc.inner.xml == nil {
			return nil, &EditUnsupportedError{Family: "xml", Reason: "document union mismatch"}
		}
		plan, failure := doc.inner.xml.DryRun(transaction.xml, sourceID)
		if failure != nil {
			return nil, failure
		}
		return plan, nil
	case transaction.plist != nil:
		if doc.inner.plist == nil {
			return nil, &EditUnsupportedError{Family: "plist", Reason: "document union mismatch"}
		}
		plan, failure := doc.inner.plist.DryRun(transaction.plist, sourceID)
		if failure != nil {
			return nil, failure
		}
		return plan, nil
	case transaction.hcl != nil:
		if doc.inner.hcl == nil {
			return nil, &EditUnsupportedError{Family: "hcl", Reason: "document union mismatch"}
		}
		plan, failure := doc.inner.hcl.DryRun(transaction.hcl, sourceID)
		if failure != nil {
			return nil, failure
		}
		return plan, nil
	}
	return nil, &EditUnsupportedError{Family: "", Reason: "empty edit transaction"}
}

// CommitEdit atomically commits one family edit transaction through the
// owning family (RFC 0004 §13: validation, source-edit preparation, output
// allocation, reparse, mapping, untouched proof, and SourcePatch derivation
// form one atomic commit; a failure returns none of the artifacts).
func CommitEdit(doc *Document, transaction *EditTransaction) (*EditCommit, error) {
	switch {
	case transaction.json != nil:
		if doc.inner.json == nil {
			return nil, &EditUnsupportedError{Family: "json", Reason: "document union mismatch"}
		}
		commit, failure := doc.inner.json.Commit(transaction.json)
		if failure != nil {
			return nil, failure
		}
		return &EditCommit{
			Document:       &Document{inner: documentInner{json: commit.Document}},
			ChangeSet:      commit.ChangeSet,
			SourcePatch:    commit.SourcePatch,
			UntouchedProof: commit.UntouchedProof,
		}, nil
	case transaction.toml != nil:
		if doc.inner.toml == nil {
			return nil, &EditUnsupportedError{Family: "toml", Reason: "document union mismatch"}
		}
		commit, failure := doc.inner.toml.Commit(transaction.toml)
		if failure != nil {
			return nil, failure
		}
		proof := commit.UntouchedProof
		return &EditCommit{
			Document:       &Document{inner: documentInner{toml: commit.Document}},
			ChangeSet:      commit.ChangeSet,
			SourcePatch:    commit.SourcePatch,
			UntouchedProof: &proof,
		}, nil
	case transaction.yaml != nil:
		if doc.inner.yaml == nil {
			return nil, &EditUnsupportedError{Family: "yaml", Reason: "document union mismatch"}
		}
		commit, failure := doc.inner.yaml.Commit(transaction.yaml)
		if failure != nil {
			return nil, failure
		}
		proof := commit.UntouchedProof
		return &EditCommit{
			Document:       &Document{inner: documentInner{yaml: commit.Document}},
			ChangeSet:      commit.ChangeSet,
			SourcePatch:    commit.SourcePatch,
			UntouchedProof: &proof,
		}, nil
	case transaction.ini != nil:
		if doc.inner.ini == nil {
			return nil, &EditUnsupportedError{Family: "ini", Reason: "document union mismatch"}
		}
		commit, failure := doc.inner.ini.Commit(transaction.ini)
		if failure != nil {
			return nil, failure
		}
		proof := commit.UntouchedProof
		return &EditCommit{
			Document:       &Document{inner: documentInner{ini: commit.Document}},
			ChangeSet:      commit.ChangeSet,
			SourcePatch:    commit.SourcePatch,
			UntouchedProof: &proof,
		}, nil
	case transaction.properties != nil:
		if doc.inner.properties == nil {
			return nil, &EditUnsupportedError{Family: "java-properties",
				Reason: "document union mismatch"}
		}
		commit, failure := doc.inner.properties.Commit(transaction.properties)
		if failure != nil {
			return nil, failure
		}
		proof := commit.UntouchedProof
		return &EditCommit{
			Document:       &Document{inner: documentInner{properties: commit.Document}},
			ChangeSet:      commit.ChangeSet,
			SourcePatch:    commit.SourcePatch,
			UntouchedProof: &proof,
		}, nil
	case transaction.xml != nil:
		if doc.inner.xml == nil {
			return nil, &EditUnsupportedError{Family: "xml", Reason: "document union mismatch"}
		}
		commit, failure := doc.inner.xml.Commit(transaction.xml)
		if failure != nil {
			return nil, failure
		}
		return &EditCommit{
			Document:       &Document{inner: documentInner{xml: commit.Document}},
			ChangeSet:      commit.ChangeSet,
			SourcePatch:    commit.SourcePatch,
			UntouchedProof: commit.UntouchedProof,
		}, nil
	case transaction.plist != nil:
		if doc.inner.plist == nil {
			return nil, &EditUnsupportedError{Family: "plist", Reason: "document union mismatch"}
		}
		commit, failure := doc.inner.plist.Commit(transaction.plist)
		if failure != nil {
			return nil, failure
		}
		return &EditCommit{
			Document:       &Document{inner: documentInner{plist: commit.Document}},
			ChangeSet:      commit.ChangeSet,
			SourcePatch:    commit.SourcePatch,
			UntouchedProof: commit.UntouchedProof,
		}, nil
	case transaction.hcl != nil:
		if doc.inner.hcl == nil {
			return nil, &EditUnsupportedError{Family: "hcl", Reason: "document union mismatch"}
		}
		commit, failure := doc.inner.hcl.Commit(transaction.hcl)
		if failure != nil {
			return nil, failure
		}
		return &EditCommit{
			Document:       &Document{inner: documentInner{hcl: commit.Document}},
			ChangeSet:      commit.ChangeSet,
			SourcePatch:    commit.SourcePatch,
			UntouchedProof: commit.UntouchedProof,
		}, nil
	}
	return nil, &EditUnsupportedError{Family: "", Reason: "empty edit transaction"}
}
