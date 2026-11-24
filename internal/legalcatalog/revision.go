package legalcatalog

// ImporterRevision is the behaviour revision of the catalogue importer. The
// vbpl-sync image compiles cmd/legal-catalog-import at build time, so a fix
// in the source keeps running old code at night until the image is rebuilt
// (an image built before the 2026-11-21 fix wiped the "toàn văn" relations
// three nights in a row). The importer records its revision in every run it
// tracks (legal_sync_runs summary "importer_revision"); the backend's
// SuperAdmin monitor reports a vbpl_sync run with a lower revision, or none,
// as a stale image. scripts/check-images.sh (deploy/image-check) is the
// automatic net for every image built from the repo, vbpl-sync included (a
// source stamp in the image label, no number to bump); this revision also
// catches an image rebuilt from an old checkout after the source was bumped.
// The vbpl-sync stamp is narrowed on purpose to the importer's own code and
// the legal code it drives (scripts/image-sources.sh): a behaviour change
// outside these paths needs ImporterRevision bumped, which stays the second
// net.
//
// Bump it whenever the behaviour of cmd/legal-catalog-import,
// internal/legalcatalog, the relation write paths (repository
// SyncRelations/ReplaceRelations, mergeRelations) or
// service.BuildLegalTextRelations changes, then rebuild the vbpl-sync image
// (deploy/vbpl-sync/README.md). 3: relations updated by difference, mass
// removal refused. 4: a reopened request keeps its decision, an adoption
// moves the requesters and logs what it drops, text edges stored by
// difference. 5: a copy whose parse failed holds nothing (heldInPublicKB,
// PublicKnowledgeByExternalIDs). 6: a replacement in flight (the portal's
// bytes already held by a copy still parsing) is not queued again.
const ImporterRevision = 6
