package index

import (
	"context"
	"time"

	"github.com/akynte/boundedcode/internal/store"
)

// Semantic indexing is the layer an analyzer cannot be.
//
// An Analyzer reads files and returns nodes and edges. That is the right shape
// for a language this process can read, and the wrong shape for a language
// whose meaning comes from a compiler: the answer arrives as a SCIP index,
// covering the whole repository at once, with symbol identities this process
// did not invent. internal/scipindex already imports exactly that into the
// same tables the analyzers write to, so a semantic indexer produces a SCIP
// file and hands it over — the graph gains resolved Python edges and nothing
// else in the system learns a new representation.
//
// It is enrichment. A repository with no semantic indexer, or one whose
// indexer failed, still has its filesystem layer and its analyzers; what it
// does not have is a claim to be semantically indexed, and SemanticReport is
// where that distinction is recorded rather than assumed.

// SemanticStatus is how far semantic indexing got.
type SemanticStatus string

const (
	// SemanticAvailable: the indexer ran, covered the repository's source and
	// produced a graph that is more than filesystem containment.
	SemanticAvailable SemanticStatus = "semantic_index_available"
	// SemanticPartial: it ran and produced something usable, but not over
	// everything — files it could not read, or a resolution gap it reported.
	// The graph is real and incomplete, and both halves matter.
	SemanticPartial SemanticStatus = "semantic_index_partial"
	// SemanticUnavailable: no semantic graph. The reason is always recorded:
	// "we did not index this" and "we indexed this and found nothing" are
	// different facts, and only one of them is about the repository.
	SemanticUnavailable SemanticStatus = "semantic_index_unavailable"
)

// SemanticReport is what a semantic indexer says about one repository.
//
// The counts are deliberately structural rather than a single score. A
// consumer deciding whether to trust symbol lookup wants to know whether
// there are cross-file edges at all; one deciding whether to re-index wants
// coverage; a person reading a run wants the reason it degraded.
type SemanticReport struct {
	Language string         `json:"language"`
	Indexer  string         `json:"indexer"`
	Version  string         `json:"version,omitempty"`
	Status   SemanticStatus `json:"status"`
	// Reason is required whenever Status is not available.
	Reason string `json:"reason,omitempty"`

	SourceFiles int `json:"source_files"`
	Documents   int `json:"documents"`
	Definitions int `json:"definitions"`
	References  int `json:"references"`

	Classes   int `json:"classes"`
	Functions int `json:"functions"`
	Imports   int `json:"imports"`

	CrossFileEdges   int `json:"cross_file_edges"`
	InheritanceEdges int `json:"inheritance_edges"`
	ContainmentEdges int `json:"containment_edges"`

	// ExcludedFiles are source files the indexer could not survive, left out
	// of a retry so the rest of the repository could be indexed.
	ExcludedFiles []string `json:"excluded_files,omitempty"`

	Duration time.Duration `json:"duration"`
}

// Covered is the share of the repository's source files the indexer produced
// a document for. It is a ratio rather than a count so no threshold anywhere
// has to know how big a particular repository is.
func (r SemanticReport) Covered() float64 {
	if r.SourceFiles == 0 {
		return 0
	}
	return float64(r.Documents) / float64(r.SourceFiles)
}

// ContainmentOnly reports the failure the status exists to name: an index
// that parsed nothing and left a graph indistinguishable from a directory
// listing.
func (r SemanticReport) ContainmentOnly() bool {
	return r.Definitions == 0 || r.CrossFileEdges+r.InheritanceEdges+r.References == 0
}

// SemanticIndexer enriches a repository with compiler-backed symbols.
//
// Applies is separate from Enrich so that a repository in another language
// costs one filesystem check rather than a process launch.
type SemanticIndexer interface {
	Name() string
	Applies(root string) bool
	Enrich(ctx context.Context, st *store.Store, repositoryID, root string) (SemanticReport, error)
}
