// legal-catalog-import loads a vbpl.vn snapshot into the platform-wide legal
// catalogue (legal_documents / legal_document_relations) read by the
// check_legal_status agent tool.
//
// Input is the JSONL pair written by scripts/legal_catalog/prepare_vbpl.py:
// one document per line (vbpl metadata field names) and one raw portal
// relation per line. Documents are upserted by vbpl id; the relation table
// is replaced as a whole with the edges whose both ends were imported.
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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
	flag.Parse()
	if *docsPath == "" || *relsPath == "" || *snapshot == "" {
		flag.Usage()
		os.Exit(2)
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
	if err := readJSONL(*docsPath, func(raw []byte) error {
		var d vbplDocument
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		if strings.TrimSpace(d.ID) == "" || ids[d.ID] {
			return nil
		}
		ids[d.ID] = true
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

	type edgeKey struct{ from, to, rel string }
	seen := map[edgeKey]bool{}
	var rels []*types.LegalDocumentRelation
	skipped := map[string]int{}
	if err := readJSONL(*relsPath, func(raw []byte) error {
		var r vbplRelation
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		from, to, rel, ok := legalcatalog.VBPLRelationEdge(r.DocID, r.OtherDocID, r.Relationship)
		if !ok {
			skipped["label:"+r.Relationship]++
			return nil
		}
		if !ids[from] || !ids[to] || from == to {
			skipped["outside catalogue"]++
			return nil
		}
		k := edgeKey{from, to, string(rel)}
		if seen[k] {
			return nil
		}
		seen[k] = true
		rels = append(rels, &types.LegalDocumentRelation{
			FromID: from, ToID: to, Relation: rel, SourceLabel: r.Relationship,
		})
		return nil
	}); err != nil {
		log.Fatalf("read relations: %v", err)
	}

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
		var gone []string
		for _, id := range existing {
			// A reviewed row stays: a SuperAdmin vouched for it.
			if !ids[id] && reviewedByID[id] == nil {
				gone = append(gone, id)
			}
		}
		for start := 0; start < len(gone); start += 1000 {
			end := min(start+1000, len(gone))
			if err := db.WithContext(ctx).Where("id IN ?", gone[start:end]).
				Delete(&types.LegalDocument{}).Error; err != nil {
				log.Fatalf("prune documents: %v", err)
			}
		}
		log.Printf("pruned %d documents absent from this import", len(gone))
	}
	if err := repo.ReplaceRelations(ctx, rels); err != nil {
		log.Fatalf("replace relations: %v", err)
	}
	log.Printf("imported %d documents and %d relations (snapshot %s)", len(docs), len(rels), *snapshot)
	for reason, n := range skipped {
		log.Printf("skipped %d relation rows: %s", n, reason)
	}
}

func openDB() (*gorm.DB, error) {
	cfg := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Warn)}
	switch os.Getenv("DB_DRIVER") {
	case "postgres":
		return gorm.Open(postgres.Open(fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
			os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"),
			os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"),
		)), cfg)
	case "sqlite":
		path := os.Getenv("DB_PATH")
		if path == "" {
			path = "./data/weknora.db"
		}
		return gorm.Open(sqlite.Open(path), cfg)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q", os.Getenv("DB_DRIVER"))
	}
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
