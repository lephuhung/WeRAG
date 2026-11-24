package legalcatalog

// ImporterRevision is the behaviour revision of the catalogue importer. The
// vbpl-sync image compiles cmd/legal-catalog-import at build time, so a fix
// in the source keeps running old code at night until the image is rebuilt
// (an image built before the 2026-11-21 fix wiped the "toàn văn" relations
// three nights in a row). The importer records its revision in every run it
// tracks (legal_sync_runs summary "importer_revision"); the backend's
// SuperAdmin monitor reports a vbpl_sync run with a lower revision, or none,
// as a stale image.
//
// Bump it whenever the behaviour of cmd/legal-catalog-import,
// internal/legalcatalog, the relation write paths (repository
// ReplaceRelations, mergeRelations) or service.BuildLegalTextRelations
// changes, then rebuild the vbpl-sync image (deploy/vbpl-sync/README.md).
const ImporterRevision = 2
