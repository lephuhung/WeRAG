// legal-provision-links imports the provision-level relations vbpl.vn
// editors tag on amended documents (type / new-types="<code>:<provision id
// in B>" on A's prov-article / prov-clause / prov-item paragraphs) into
// legal_provision_links (migration 000176), the second amendment source of
// the current-wording resolver (plan 2026-10-19-legal-amendment-kg §11).
//
// It reads every crawled page (<html dir>/<vbpl id>.html, the id being the
// catalogue id), places A's tagged provisions with the vbpl converter
// (paths match the chunks' provision_ids), finds B by scanning all pages
// for the source provision id (ids are global; vbpl's provision-target
// answer when B's page is not crawled), and dates each edge with the
// per-provision effFrom of prov_targets.sqlite when fetched, else B's
// catalogue effective_on.
//
// Dry run by default (counts only, nothing written); -out writes the rows
// as JSON lines; -apply upserts them (the migration must be applied). The
// catalogue is read from the database of the DB_* environment (as the
// server); -no-db skips it (no catalogue dates or B checks).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vbpl"
)

func main() {
	htmlDir := flag.String("html", "/home/orca/data/vbpl/html", "directory of crawled vbpl pages <id>.html")
	provTargets := flag.String("prov-targets", "", "prov_targets.sqlite (vbpl provision-target answers), opened read-only")
	gold := flag.String("gold", "", "gold_edges.jsonl to cross-check the counts against")
	out := flag.String("out", "", "write the rows as JSON lines to this file")
	apply := flag.Bool("apply", false, "upsert the rows into legal_provision_links (default: dry run)")
	noDB := flag.Bool("no-db", false, "do not read the catalogue")
	workers := flag.Int("workers", runtime.NumCPU(), "pages parsed in parallel")
	verbose := flag.Bool("v", false, "print unresolved and ambiguous edges")
	flag.Parse()

	var db *gorm.DB
	if !*noDB || *apply {
		var err error
		if db, err = openDB(); err != nil {
			log.Fatalf("database: %v", err)
		}
	}
	started := time.Now()
	pages, err := readPages(*htmlDir, *workers)
	if err != nil {
		log.Fatalf("pages: %v", err)
	}
	log.Printf("%d pages read in %v", len(pages), time.Since(started).Round(time.Second))

	var catalog map[string]catalogRow
	if db != nil && !*noDB {
		if catalog, err = readCatalog(db); err != nil {
			log.Fatalf("catalogue: %v", err)
		}
	}
	var targets map[string]provTarget
	if *provTargets != "" {
		if targets, err = readProvTargets(*provTargets); err != nil {
			log.Fatalf("prov-targets: %v", err)
		}
		log.Printf("%d provision-target answers", len(targets))
	}
	rows, st := buildRows(pages, catalog, targets, *verbose)
	st.print(os.Stdout)
	if *gold != "" {
		if err := crossCheck(os.Stdout, *gold, rows); err != nil {
			log.Fatalf("gold: %v", err)
		}
	}
	if *out != "" {
		if err := writeRows(*out, rows); err != nil {
			log.Fatalf("out: %v", err)
		}
		log.Printf("%d rows written to %s", len(rows), *out)
	}
	if *apply {
		if err := upsert(db, rows); err != nil {
			log.Fatalf("apply: %v", err)
		}
		log.Printf("%d rows upserted", len(rows))
	} else {
		log.Printf("dry run: nothing written to the database (-apply writes)")
	}
}

func openDB() (*gorm.DB, error) {
	if d := os.Getenv("DB_DRIVER"); d != "" && d != "postgres" {
		return nil, fmt.Errorf("DB_DRIVER=%s: only postgres is supported", d)
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	return gorm.Open(postgres.Open(fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"), port, os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"),
	)), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
}

// page is what one crawled page gives.
type page struct {
	id     string
	editor *vbpl.EditorPage
	places map[string]vbpl.ProvisionPlace
	err    error
}

func readPages(dir string, workers int) ([]*page, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.html"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if workers < 1 {
		workers = 1
	}
	pages := make([]*page, len(files))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				pages[i] = readPage(files[i])
			}
		}()
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	return pages, nil
}

func readPage(file string) *page {
	p := &page{id: strings.TrimSuffix(filepath.Base(file), ".html")}
	raw, err := os.ReadFile(file)
	if err != nil {
		p.err = err
		return p
	}
	if p.editor, err = vbpl.ReadEditorPage(raw); err != nil {
		p.err = err
		return p
	}
	// the converter's places, only needed where a provision id is: pages
	// without any are skipped
	if len(p.editor.Levels) == 0 {
		return p
	}
	doc, err := vbpl.ConvertBytes(raw)
	if err != nil {
		p.err = err
		return p
	}
	p.places = vbpl.ProvisionPlaces(doc)
	return p
}

type catalogRow struct {
	ID             string
	DocumentNumber string
	EffectiveOn    *time.Time
}

func readCatalog(db *gorm.DB) (map[string]catalogRow, error) {
	var rows []catalogRow
	if err := db.Raw(`SELECT id, document_number, effective_on FROM legal_documents`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]catalogRow, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// provTarget is vbpl's provision-target answer for one source provision
// of one target provision.
type provTarget struct {
	DocumentID string
	EffFrom    *time.Time
	EffTo      *time.Time
}

func readProvTargets(path string) (map[string]provTarget, error) {
	// read-only: another process may still be writing the file
	db, err := gorm.Open(sqlite.Open("file:"+path+"?mode=ro"), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		return nil, err
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	var rows []struct {
		Prov string
		Body string
	}
	if err := db.Raw(`SELECT prov, body FROM t WHERE body IS NOT NULL`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]provTarget{}
	for _, r := range rows {
		var body struct {
			SourceProvisions []struct {
				ID         string  `json:"id"`
				EffFrom    *string `json:"effFrom"`
				EffTo      *string `json:"effTo"`
				DocumentID string  `json:"documentId"`
			} `json:"sourceProvisions"`
		}
		if json.Unmarshal([]byte(r.Body), &body) != nil {
			continue
		}
		for _, s := range body.SourceProvisions {
			out[r.Prov+"|"+s.ID] = provTarget{DocumentID: s.DocumentID, EffFrom: vbplDate(s.EffFrom), EffTo: vbplDate(s.EffTo)}
		}
	}
	return out, nil
}

// vbplDate reads "2027-07-01T00:00:00".
func vbplDate(s *string) *time.Time {
	if s == nil || len(*s) < 10 {
		return nil
	}
	t, err := time.Parse("2006-01-02", (*s)[:10])
	if err != nil {
		return nil
	}
	return &t
}

// stats are the dry-run counts.
type stats struct {
	pages, pageErrors, pagesTagged int
	tags, malformed, noID          int
	phuLuc, otherLevel             int
	rows, dupTarget                int
	byCode, byAttr, byLevel        map[string]int
	bFrom, effSource, byYear       map[string]int
	bAmbiguous, bSelf, bNotInCat   int
	aNoPath, aNotInCat, aAppendix  int
	unresolved, ambiguous          []string
}

func (s *stats) print(w *os.File) {
	fmt.Fprintf(w, "pages %d (errors %d), with tags %d\n", s.pages, s.pageErrors, s.pagesTagged)
	fmt.Fprintf(w, "tags read %d; malformed %d; tagged paragraph without id %d; PHU-LUC (skipped) %d; on chapter/section (skipped) %d\n",
		s.tags, s.malformed, s.noID, s.phuLuc, s.otherLevel)
	fmt.Fprintf(w, "edges %d; same (A provision, code, B provision) on two pages (kept once) %d\n", s.rows, s.dupTarget)
	fmt.Fprintf(w, "  by code %s\n  by attr %s\n  by A level %s\n", sortedCounts(s.byCode), sortedCounts(s.byAttr), sortedCounts(s.byLevel))
	fmt.Fprintf(w, "  A: no path %d, in an appendix %d, not in catalogue %d\n", s.aNoPath, s.aAppendix, s.aNotInCat)
	fmt.Fprintf(w, "  B found via %s; ambiguous (id on several pages) %d; B = A %d; B not in catalogue %d\n",
		sortedCounts(s.bFrom), s.bAmbiguous, s.bSelf, s.bNotInCat)
	fmt.Fprintf(w, "  eff_from source %s\n  by eff year %s\n", sortedCounts(s.effSource), sortedCounts(s.byYear))
	for _, u := range s.unresolved {
		fmt.Fprintln(w, "  UNRESOLVED B", u)
	}
	for _, u := range s.ambiguous {
		fmt.Fprintln(w, "  AMBIGUOUS B", u)
	}
}

func sortedCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, m[k])
	}
	return strings.Join(parts, " ")
}

// buildRows turns the pages' tags into rows.
func buildRows(pages []*page, catalog map[string]catalogRow, targets map[string]provTarget, verbose bool) ([]types.LegalProvisionLink, *stats) {
	st := &stats{byCode: map[string]int{}, byAttr: map[string]int{}, byLevel: map[string]int{},
		bFrom: map[string]int{}, effSource: map[string]int{}, byYear: map[string]int{}}
	// every provision id → the pages holding it
	where := map[string][]*page{}
	for _, p := range pages {
		st.pages++
		if p.err != nil {
			st.pageErrors++
			log.Printf("page %s: %v", p.id, p.err)
			continue
		}
		for id := range p.editor.Levels {
			where[id] = append(where[id], p)
		}
	}
	seen := map[string]int{}
	var rows []types.LegalProvisionLink
	for _, p := range pages {
		if p.err != nil || len(p.editor.Tags) == 0 {
			continue
		}
		st.pagesTagged++
		st.tags += p.editor.Stats.Tags
		st.malformed += p.editor.Stats.Malformed
		st.noID += p.editor.Stats.NoID
		ids := make([]string, 0, len(p.editor.Tags))
		for id := range p.editor.Tags {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, aProv := range ids {
			level := p.editor.Levels[aProv]
			switch level {
			case "article", "clause", "item":
			default:
				st.otherLevel += len(p.editor.Tags[aProv])
				continue
			}
			place := p.places[aProv]
			for _, t := range p.editor.Tags[aProv] {
				if t.SourceID == vbpl.EditorSourcePhuLuc {
					st.phuLuc++
					continue
				}
				r := types.LegalProvisionLink{
					TargetDocID: p.id, TargetProvisionID: aProv, TargetLevel: level, TargetPath: place.Path,
					RelationCode: t.Code, Op: vbpl.EditorCodeOp(t.Code), TagAttr: t.Attr,
					SourceProvisionID: t.SourceID, Source: types.LegalProvisionLinkSourceEditor,
				}
				key := fmt.Sprintf("%s|%d|%s", aProv, t.Code, t.SourceID)
				if _, dup := seen[key]; dup {
					st.dupTarget++
					continue
				}
				// B: the page holding the source id (not A itself)
				pt, hasPT := targets[aProv+"|"+t.SourceID]
				var cands []*page
				self := false
				for _, q := range where[t.SourceID] {
					if q.id == p.id {
						self = true
						continue
					}
					cands = append(cands, q)
				}
				if len(cands) > 1 {
					cands = pickB(cands, catalog, pt.DocumentID)
				}
				switch {
				case len(cands) >= 1:
					if len(cands) > 1 {
						st.bAmbiguous++
						if verbose {
							var names []string
							for _, c := range cands {
								names = append(names, c.id)
							}
							st.ambiguous = append(st.ambiguous, fmt.Sprintf("%s %s ← %s on %v", p.id, place.Path, t.SourceID, names))
						}
					}
					b := cands[0]
					r.SourceDocID, r.SourceDocFrom = b.id, types.LegalLinkFromHTML
					r.SourcePath = b.places[t.SourceID].Path
				case hasPT && pt.DocumentID != "" && pt.DocumentID != p.id:
					r.SourceDocID, r.SourceDocFrom = pt.DocumentID, types.LegalLinkFromProvisionTarget
				case self:
					// an id only A holds: A acting on itself (or a copy);
					// kept unresolved
					st.bSelf++
				}
				if r.SourceDocID == "" && verbose {
					st.unresolved = append(st.unresolved, fmt.Sprintf("%s %s code %d ← %s", p.id, place.Path, t.Code, t.SourceID))
				}
				// date: vbpl's own per-provision date, else B's catalogue one
				switch {
				case hasPT && pt.EffFrom != nil:
					r.EffFrom, r.EffTo, r.EffFromSource = pt.EffFrom, pt.EffTo, types.LegalLinkFromProvisionTarget
				case r.SourceDocID != "" && catalog[r.SourceDocID].EffectiveOn != nil:
					r.EffFrom, r.EffFromSource = catalog[r.SourceDocID].EffectiveOn, types.LegalLinkFromCatalogue
				}
				if hasPT && r.EffTo == nil {
					r.EffTo = pt.EffTo
				}
				seen[key] = len(rows)
				rows = append(rows, r)
				st.count(r, catalog)
			}
		}
	}
	return rows, st
}

// pickB narrows the pages holding a source id: vbpl's own answer first,
// then the pages in the catalogue; one left is sure, several ambiguous.
func pickB(cands []*page, catalog map[string]catalogRow, ptDoc string) []*page {
	if ptDoc != "" {
		for _, c := range cands {
			if c.id == ptDoc {
				return []*page{c}
			}
		}
	}
	if catalog != nil {
		var in []*page
		for _, c := range cands {
			if _, ok := catalog[c.id]; ok {
				in = append(in, c)
			}
		}
		if len(in) > 0 {
			cands = in
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].id < cands[j].id })
	return cands
}

func (st *stats) count(r types.LegalProvisionLink, catalog map[string]catalogRow) {
	st.rows++
	st.byCode[fmt.Sprintf("%d:%s", r.RelationCode, r.Op)]++
	st.byAttr[r.TagAttr]++
	st.byLevel[r.TargetLevel]++
	from := r.SourceDocFrom
	if from == "" {
		from = "unresolved"
	}
	st.bFrom[from]++
	src := r.EffFromSource
	if src == "" {
		src = "unknown"
	}
	st.effSource[src]++
	if r.EffFrom != nil {
		st.byYear[r.EffFrom.Format("2006")]++
	} else {
		st.byYear["?"]++
	}
	switch {
	case r.TargetPath == "":
		st.aNoPath++
	case strings.HasPrefix(r.TargetPath, "phuluc/"):
		st.aAppendix++
	}
	if catalog != nil {
		if _, ok := catalog[r.TargetDocID]; !ok {
			st.aNotInCat++
		}
		if r.SourceDocID != "" {
			if _, ok := catalog[r.SourceDocID]; !ok {
				st.bNotInCat++
			}
		}
	}
}

// crossCheck compares the rows with gold_edges.jsonl (Python reader of
// the same tags) on (A provision, code, B provision), B's document and A's
// path.
func crossCheck(w *os.File, path string, rows []types.LegalProvisionLink) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	type goldEdge struct {
		Code       string  `json:"code"`
		TargetDoc  string  `json:"target_doc"`
		TargetProv string  `json:"target_prov"`
		TargetPath *string `json:"target_path"`
		SourceProv string  `json:"source_prov"`
		SourceDoc  *string `json:"source_doc"`
	}
	goldBy := map[string]goldEdge{}
	dec := json.NewDecoder(f)
	for dec.More() {
		var g goldEdge
		if err := dec.Decode(&g); err != nil {
			return err
		}
		goldBy[g.TargetProv+"|"+g.Code+"|"+g.SourceProv] = g
	}
	mine := map[string]types.LegalProvisionLink{}
	for _, r := range rows {
		mine[fmt.Sprintf("%s|%d|%s", r.TargetProvisionID, r.RelationCode, r.SourceProvisionID)] = r
	}
	var both, onlyGold, onlyMine, goldPhuLuc, docDiff, pathDiff, bDiff, bMineOnly, bGoldOnly int
	var samples []string
	for k, g := range goldBy {
		r, ok := mine[k]
		if !ok {
			if g.SourceProv == vbpl.EditorSourcePhuLuc {
				goldPhuLuc++
				continue
			}
			onlyGold++
			if len(samples) < 8 {
				samples = append(samples, "only gold: "+g.TargetDoc+" "+k)
			}
			continue
		}
		both++
		if r.TargetDocID != g.TargetDoc {
			docDiff++
		}
		gp := ""
		if g.TargetPath != nil {
			gp = *g.TargetPath
		}
		if gp != r.TargetPath {
			pathDiff++
			if len(samples) < 16 {
				samples = append(samples, fmt.Sprintf("path: %s %s gold=%q mine=%q", g.TargetDoc, g.TargetProv, gp, r.TargetPath))
			}
		}
		gb := ""
		if g.SourceDoc != nil {
			gb = *g.SourceDoc
		}
		switch {
		case gb == r.SourceDocID:
		case gb == "":
			bMineOnly++
		case r.SourceDocID == "":
			bGoldOnly++
		default:
			bDiff++
			if len(samples) < 24 {
				samples = append(samples, fmt.Sprintf("B: %s gold=%s mine=%s (%s)", k, gb, r.SourceDocID, r.SourceDocFrom))
			}
		}
	}
	for k, r := range mine {
		if _, ok := goldBy[k]; !ok {
			onlyMine++
			if len(samples) < 32 {
				samples = append(samples, "only mine: "+r.TargetDocID+" "+r.TargetPath+" "+k)
			}
		}
	}
	fmt.Fprintf(w, "gold cross-check: gold %d (PHU-LUC %d), mine %d; both %d, only gold %d, only mine %d\n",
		len(goldBy), goldPhuLuc, len(mine), both, onlyGold, onlyMine)
	fmt.Fprintf(w, "  on both: A page differs %d, A path differs %d; B differs %d, B only mine %d, B only gold %d\n",
		docDiff, pathDiff, bDiff, bMineOnly, bGoldOnly)
	for _, s := range samples {
		fmt.Fprintln(w, "  ", s)
	}
	return nil
}

func writeRows(path string, rows []types.LegalProvisionLink) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

// upsert writes the rows, keyed by (A provision, code, B provision).
func upsert(db *gorm.DB, rows []types.LegalProvisionLink) error {
	now := time.Now()
	for i := range rows {
		rows[i].CreatedAt, rows[i].UpdatedAt = now, now
	}
	return db.Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "target_provision_id"}, {Name: "relation_code"}, {Name: "source_provision_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"target_doc_id", "target_level", "target_path", "op", "tag_attr",
				"source_doc_id", "source_doc_from", "source_path", "eff_from", "eff_to", "eff_from_source", "source", "updated_at"}),
		}).CreateInBatches(rows, 500).Error
	})
}
