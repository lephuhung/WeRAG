// legal-catalog-import loads a vbpl.vn snapshot into the platform-wide legal
// catalogue (legal_documents / legal_document_relations) read by the
// check_legal_status agent tool.
//
// Input is the JSONL pair written by scripts/legal_catalog/prepare_vbpl.py:
// one document per line (vbpl metadata field names) and one raw portal
// relation per line. Documents are upserted by vbpl id, except a second
// copy of a catalogued document (legalcatalog.ResolveTwins), whose edges
// go to the catalogued row; the relation table is replaced as a whole
// with the edges whose both ends were imported.
// With -merge (a partial import, e.g. the local documents of a few
// provinces) only the edges touching the imported documents are replaced,
// and an edge may end at any document already in the catalogue.
// The database comes from the same DB_* environment as the server.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/legalcatalog"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

type vbplDocument struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	SoKyHieu         string `json:"so_ky_hieu"`
	LoaiVanBan       string `json:"loai_van_ban"`
	CoQuanBanHanh    string `json:"co_quan_ban_hanh"`
	NguoiKy          string `json:"nguoi_ky"`
	ChucDanh         string `json:"chuc_danh"`
	LinhVuc          string `json:"linh_vuc"`
	NgayBanHanh      string `json:"ngay_ban_hanh"`
	NgayCoHieuLuc    string `json:"ngay_co_hieu_luc"`
	NgayHetHieuLuc   string `json:"ngay_het_hieu_luc"`
	TinhTrangHieuLuc string `json:"tinh_trang_hieu_luc"`
	// MetadataOnly marks a row built from the portal's list alone (a
	// document outside the crawl scope, or one whose detail failed).
	MetadataOnly bool `json:"metadata_only"`
}

type vbplRelation struct {
	DocID        string `json:"doc_id"`
	OtherDocID   string `json:"other_doc_id"`
	Relationship string `json:"relationship"`
}

func main() {
	docsPath := flag.String("documents", "", "JSONL file of vbpl documents (required)")
	relsPath := flag.String("relations", "", "JSONL file of raw vbpl relations (required)")
	snapshot := flag.String("snapshot", "", "Snapshot date of the source, YYYY-MM-DD (required)")
	prune := flag.Bool("prune", false, "Delete vbpl documents absent from this import")
	report := flag.String("report", "", "Write the Ð twins left unreachable by number (TSV) to this file")
	merge := flag.Bool("merge", false,
		"Partial import: replace only the relations touching these documents, keep the rest of the catalogue")
	flag.Parse()
	if *docsPath == "" || *relsPath == "" || *snapshot == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *merge && *prune {
		log.Fatal("-merge and -prune exclude each other")
	}
	snap, err := time.Parse("2006-01-02", *snapshot)
	if err != nil {
		log.Fatalf("bad -snapshot: %v", err)
	}

	db, err := openDB()
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	ctx := context.Background()
	repo := repository.NewLegalCatalogRepository(db)

	var docs []*types.LegalDocument
	ids := map[string]bool{}
	listOnly := map[string]bool{}
	if err := readJSONL(*docsPath, func(raw []byte) error {
		var d vbplDocument
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		if strings.TrimSpace(d.ID) == "" || ids[d.ID] {
			return nil
		}
		ids[d.ID] = true
		if d.MetadataOnly {
			listOnly[d.ID] = true
		}
		docs = append(docs, &types.LegalDocument{
			ID:              d.ID,
			Source:          "vbpl",
			DocumentNumber:  legalcatalog.CleanVBPLNumber(d.SoKyHieu),
			NumberKey:       vietnamese_legal.CatalogNumberKey(legalcatalog.CleanVBPLNumber(d.SoKyHieu)),
			Title:           strings.TrimSpace(d.Title),
			DocType:         strings.TrimSpace(d.LoaiVanBan),
			Issuer:          strings.TrimSpace(d.CoQuanBanHanh),
			Signer:          strings.TrimSpace(d.NguoiKy),
			SignerTitle:     strings.TrimSpace(d.ChucDanh),
			Field:           strings.TrimSpace(d.LinhVuc),
			IssuedOn:        parseVNDate(d.NgayBanHanh),
			EffectiveOn:     parseVNDate(d.NgayCoHieuLuc),
			ExpiredOn:       parseVNDate(d.NgayHetHieuLuc),
			Status:          strings.TrimSpace(d.TinhTrangHieuLuc),
			SourceUpdatedAt: &snap,
		})
		return nil
	}); err != nil {
		log.Fatalf("read documents: %v", err)
	}

	// A list-only row never replaces a row already in the catalogue: that
	// one came from a full detail (an earlier snapshot or crawl) and the
	// list lacks signer and field. The document stays known for relations.
	if len(listOnly) > 0 {
		keep, err := existingIDs(ctx, db, listOnly)
		if err != nil {
			log.Fatalf("load existing documents: %v", err)
		}
		kept := docs[:0]
		for _, d := range docs {
			if !keep[d.ID] {
				kept = append(kept, d)
			}
		}
		log.Printf("%d list-only documents, %d of them kept as already catalogued", len(listOnly), len(docs)-len(kept))
		docs = kept
	}

	// One document under two vbpl ids (an Eth "Ð" or Cyrillic copy of a
	// Latin row) stays one row: a new twin is not imported, a catalogued
	// twin keeps its look-alike key, and their edges move to the surviving
	// row. Ð rows that are no twin of a holder stay as migration 000175 left
	// them (-report lists them).
	var catalogued []*types.LegalDocument
	if err := db.WithContext(ctx).Model(&types.LegalDocument{}).
		Select("id", "document_number", "number_key", "doc_type", "title", "issuer", "issued_on", "status", "reviewed_at").Order("id").
		Find(&catalogued).Error; err != nil {
		log.Fatalf("load catalogue keys: %v", err)
	}
	twins := legalcatalog.ResolveTwins(catalogued, docs)
	docs, alias := twins.Keep, twins.Alias
	log.Printf("%d twin documents attached to their catalogue row", len(alias))
	if err := reportTwins(*report, twins); err != nil {
		log.Fatalf("write twin report: %v", err)
	}

	// A merge may link the imported documents to any catalogued one.
	known := ids
	if *merge {
		others := map[string]bool{}
		for _, s := range alias {
			if !ids[s] {
				others[s] = true
			}
		}
		if err := readJSONL(*relsPath, func(raw []byte) error {
			var r vbplRelation
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			for _, id := range []string{r.DocID, r.OtherDocID} {
				if id != "" && !ids[id] {
					others[id] = true
				}
			}
			return nil
		}); err != nil {
			log.Fatalf("read relations: %v", err)
		}
		found, err := existingIDs(ctx, db, others)
		if err != nil {
			log.Fatalf("load existing documents: %v", err)
		}
		for id := range ids {
			found[id] = true
		}
		known = found
	}

	edges := newEdgeBuilder(ids, known, alias)
	if err := readJSONL(*relsPath, func(raw []byte) error {
		var r vbplRelation
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		edges.add(r)
		return nil
	}); err != nil {
		log.Fatalf("read relations: %v", err)
	}
	rels, skipped := edges.rels, edges.skipped

	// Reviewed rows are the curated truth: the portal never overwrites them.
	// Differing portal values are parked in source_pending for the reviewer.
	var reviewed []*types.LegalDocument
	if err := db.WithContext(ctx).Where("reviewed_at IS NOT NULL").Find(&reviewed).Error; err != nil {
		log.Fatalf("load reviewed documents: %v", err)
	}
	reviewedByID := make(map[string]*types.LegalDocument, len(reviewed))
	for _, d := range reviewed {
		reviewedByID[d.ID] = d
	}
	fresh := docs[:0:0]
	conflicts := 0
	for _, d := range docs {
		cur := reviewedByID[d.ID]
		if cur == nil {
			fresh = append(fresh, d)
			continue
		}
		var pending types.JSON
		if diff := reviewDiff(cur, d); len(diff) > 0 {
			raw, err := json.Marshal(diff)
			if err != nil {
				log.Fatalf("encode pending values: %v", err)
			}
			pending = types.JSON(raw)
			conflicts++
		}
		if err := repo.SetSourcePending(ctx, d.ID, pending); err != nil {
			log.Fatalf("park pending values: %v", err)
		}
	}
	if err := repo.UpsertDocuments(ctx, fresh); err != nil {
		log.Fatalf("upsert documents: %v", err)
	}
	log.Printf("kept %d reviewed documents, %d of them differ from the portal", len(reviewedByID), conflicts)
	if *prune {
		var existing []string
		if err := db.WithContext(ctx).Model(&types.LegalDocument{}).
			Where("source = ?", "vbpl").Pluck("id", &existing).Error; err != nil {
			log.Fatalf("list documents: %v", err)
		}
		gone := pruneCandidates(existing, ids, alias, reviewedByID)
		for start := 0; start < len(gone); start += 1000 {
			end := min(start+1000, len(gone))
			if err := db.WithContext(ctx).Where("id IN ?", gone[start:end]).
				Delete(&types.LegalDocument{}).Error; err != nil {
				log.Fatalf("prune documents: %v", err)
			}
		}
		log.Printf("pruned %d documents absent from this import", len(gone))
	}
	if *merge {
		if err := mergeRelations(ctx, db, ids, rels); err != nil {
			log.Fatalf("merge relations: %v", err)
		}
	} else {
		// A full import without a single edge is a broken or partial file,
		// not a catalogue without relations: replacing would wipe the table.
		if len(rels) == 0 {
			log.Fatal("no relation between imported documents: refusing to clear the relation table " +
				"(partial import? use -merge)")
		}
		if err := repo.ReplaceRelations(ctx, rels); err != nil {
			log.Fatalf("replace relations: %v", err)
		}
	}
	log.Printf("imported %d documents and %d relations (snapshot %s)", len(docs), len(rels), *snapshot)
	for reason, n := range skipped {
		log.Printf("skipped %d relation rows: %s", n, reason)
	}
}

// pruneCandidates are the catalogued vbpl rows a -prune import deletes:
// absent from the import, not reviewed (a SuperAdmin vouched for it) and
// not the surviving row of an imported twin (its document is in the
// import under the twin's id).
func pruneCandidates(existing []string, ids map[string]bool, alias map[string]string,
	reviewed map[string]*types.LegalDocument) []string {
	survivors := map[string]bool{}
	for twin, s := range alias {
		for hop := 0; ids[twin] && hop < 4 && s != ""; hop++ {
			survivors[s] = true
			s = alias[s]
		}
	}
	var gone []string
	for _, id := range existing {
		if !ids[id] && !survivors[id] && reviewed[id] == nil {
			gone = append(gone, id)
		}
	}
	return gone
}

// reportTwins logs the Ð twins kept out of the number lookup and the
// attached twins whose status is not their survivor's and, with a path,
// writes both as TSV sections for the SuperAdmin decision.
func reportTwins(path string, t legalcatalog.TwinResolution) error {
	log.Printf("%d Ð twins left unreachable by number (not the same document as a row of their folded key)", len(t.Unreachable))
	log.Printf("%d attached twins differ in status from their surviving row", len(t.StatusDiffers))
	if path == "" {
		return nil
	}
	clean := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	day := func(d *time.Time) string {
		if d == nil {
			return ""
		}
		return d.Format("2006-01-02")
	}
	var b strings.Builder
	b.WriteString("# unreachable Ð twins\nid\tdocument_number\tdoc_type\tissued_on\ttitle\tholders\n")
	for _, u := range t.Unreachable {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\n", u.Row.ID, clean(u.Row.DocumentNumber), clean(u.Row.DocType),
			day(u.Row.IssuedOn), clean(u.Row.Title), strings.Join(u.Holders, ","))
	}
	b.WriteString("\n# attached twins with another status\ntwin_id\ttwin_status\tsurvivor_id\tsurvivor_status\tdocument_number\ttitle\n")
	for _, a := range t.StatusDiffers {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\n", a.Twin.ID, clean(a.Twin.Status), a.Survivor.ID, clean(a.Survivor.Status),
			clean(a.Survivor.DocumentNumber), clean(a.Twin.Title))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// edgeBuilder turns raw vbpl relation rows into catalogue edges: labels
// without legal effect dropped, an edge kept only when it touches an
// imported document (ids) and both ends, after a twin is replaced by its
// surviving row (alias, from legalcatalog.ResolveTwins), are known; a self
// edge or a repeat (two twins listing the same edge) is dropped.
type edgeBuilder struct {
	ids, known map[string]bool
	alias      map[string]string
	seen       map[edgeKey]bool
	rels       []*types.LegalDocumentRelation
	skipped    map[string]int
}

type edgeKey struct{ from, to, rel string }

func newEdgeBuilder(ids, known map[string]bool, alias map[string]string) *edgeBuilder {
	return &edgeBuilder{ids: ids, known: known, alias: alias, seen: map[edgeKey]bool{}, skipped: map[string]int{}}
}

// survivor follows the alias chain (a twin of a placeholder replaced by
// its full copy) to the row that keeps the edges.
func (b *edgeBuilder) survivor(id string) string {
	for hop := 0; hop < 4; hop++ {
		s, ok := b.alias[id]
		if !ok || s == id {
			break
		}
		id = s
	}
	return id
}

func (b *edgeBuilder) add(r vbplRelation) {
	from, to, rel, ok := legalcatalog.VBPLRelationEdge(r.DocID, r.OtherDocID, r.Relationship)
	if !ok {
		b.skipped["label:"+r.Relationship]++
		return
	}
	if !b.ids[from] && !b.ids[to] {
		b.skipped["outside catalogue"]++
		return
	}
	from, to = b.survivor(from), b.survivor(to)
	if !b.known[from] || !b.known[to] || from == to {
		b.skipped["outside catalogue"]++
		return
	}
	k := edgeKey{from, to, string(rel)}
	if b.seen[k] {
		return
	}
	b.seen[k] = true
	b.rels = append(b.rels, &types.LegalDocumentRelation{
		FromID: from, ToID: to, Relation: rel, SourceLabel: r.Relationship,
	})
}

// openDB connects to the Postgres database the server uses (DB_* env).
// Deployments are Postgres only, so the importer builds without cgo.
func openDB() (*gorm.DB, error) {
	cfg := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Warn)}
	if d := os.Getenv("DB_DRIVER"); d != "" && d != "postgres" {
		return nil, fmt.Errorf("unsupported DB_DRIVER %q: Postgres only", d)
	}
	return gorm.Open(postgres.Open(fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"),
	)), cfg)
}

// mergeRelations replaces the edges touching the imported documents and
// leaves every other edge of the catalogue alone.
func mergeRelations(ctx context.Context, db *gorm.DB, ids map[string]bool, rels []*types.LegalDocumentRelation) error {
	all := make([]string, 0, len(ids))
	for id := range ids {
		all = append(all, id)
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(all); start += 1000 {
			batch := all[start:min(start+1000, len(all))]
			if err := tx.Where("from_id IN ? OR to_id IN ?", batch, batch).
				Delete(&types.LegalDocumentRelation{}).Error; err != nil {
				return err
			}
		}
		if len(rels) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rels, 1000).Error
	})
}

// existingIDs returns which of ids already have a catalogue row.
func existingIDs(ctx context.Context, db *gorm.DB, ids map[string]bool) (map[string]bool, error) {
	all := make([]string, 0, len(ids))
	for id := range ids {
		all = append(all, id)
	}
	out := map[string]bool{}
	for start := 0; start < len(all); start += 1000 {
		var found []string
		if err := db.WithContext(ctx).Model(&types.LegalDocument{}).
			Where("id IN ?", all[start:min(start+1000, len(all))]).Pluck("id", &found).Error; err != nil {
			return nil, err
		}
		for _, id := range found {
			out[id] = true
		}
	}
	return out, nil
}

func readJSONL(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		if err := fn(sc.Bytes()); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	return sc.Err()
}

// reviewDiff returns the portal values of the fields a reviewer curates
// that differ from the reviewed row, keyed by JSON field name.
func reviewDiff(cur, in *types.LegalDocument) map[string]string {
	diff := map[string]string{}
	cmp := func(name, a, b string) {
		if strings.TrimSpace(a) != strings.TrimSpace(b) {
			diff[name] = b
		}
	}
	day := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format("2006-01-02")
	}
	cmp("document_number", cur.DocumentNumber, in.DocumentNumber)
	cmp("title", cur.Title, in.Title)
	cmp("doc_type", cur.DocType, in.DocType)
	cmp("issuer", cur.Issuer, in.Issuer)
	cmp("issued_on", day(cur.IssuedOn), day(in.IssuedOn))
	cmp("effective_on", day(cur.EffectiveOn), day(in.EffectiveOn))
	cmp("expired_on", day(cur.ExpiredOn), day(in.ExpiredOn))
	cmp("status", cur.Status, in.Status)
	return diff
}

// parseVNDate reads the portal's dd/mm/yyyy dates; anything else (empty,
// typos such as year 3024) is dropped rather than guessed.
func parseVNDate(s string) *time.Time {
	t, err := time.Parse("02/01/2006", strings.TrimSpace(s))
	if err != nil || t.Year() < 1900 || t.Year() > 2100 {
		return nil
	}
	return &t
}
