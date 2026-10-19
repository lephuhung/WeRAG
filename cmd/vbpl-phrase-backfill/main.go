// vbpl-phrase-backfill records phrase amendments ("Sửa đổi cụm từ “500 triệu
// đồng” thành “01 tỷ đồng” tại Điều 3 …") on vbpl chunks ingested before the
// converter read them, so the amendment history and the current wording
// shown to agents cover them without re-ingesting (no re-embedding, no
// graph re-extraction). Only metadata.legal.amends changes (jsonb_set, so a
// concurrent write to other metadata keys is kept): phrase entries are
// added, quoted-text entries kept. A chunk that already has phrase entries
// — ingested by the converter that reads them, which sees more context than
// this — is skipped unless -force.
//
// Dry run by default; -apply writes. The database comes from the same DB_*
// environment as the server.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/Tencent/WeKnora/internal/vbpl"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// phraseLeadSQL preselects chunks that may hold a phrase amendment; the Go
// parser decides.
const phraseLeadSQL = `(sửa đổi|thay thế|thay đổi|thay|bãi bỏ)\s+(các\s+)?(cụm\s+từ|từ\s+ngữ|từ)\s*[“"]`

type row struct {
	ID       string
	Content  string
	Metadata []byte
	Title    string
}

func main() {
	apply := flag.Bool("apply", false, "write the metadata (default: dry run)")
	kb := flag.String("kb", "", "only this knowledge base id")
	verbose := flag.Bool("v", false, "print every changed chunk")
	force := flag.Bool("force", false, "recompute chunks that already have phrase entries")
	flag.Parse()

	db, err := openDB()
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	q := db.Table("chunks c").Select("c.id, c.content, c.metadata, k.title").
		Joins("JOIN knowledges k ON k.id = c.knowledge_id").
		Where("c.deleted_at IS NULL AND k.deleted_at IS NULL").
		Where("c.metadata->'legal'->>'source' = 'vbpl'").
		Where("c.content ~* ?", phraseLeadSQL)
	if *kb != "" {
		q = q.Where("c.knowledge_base_id = ?", *kb)
	}
	var rows []row
	if err := q.Scan(&rows).Error; err != nil {
		log.Fatalf("select: %v", err)
	}
	var changed, added int
	for _, r := range rows {
		meta := map[string]interface{}{}
		if err := json.Unmarshal(r.Metadata, &meta); err != nil {
			log.Printf("chunk %s: metadata: %v", r.ID, err)
			continue
		}
		legal, _ := meta["legal"].(map[string]interface{})
		if legal == nil {
			continue
		}
		var path []string
		if hp, ok := legal["heading_path"].([]interface{}); ok {
			for _, h := range hp {
				if s, ok := h.(string); ok {
					path = append(path, s)
				}
			}
		}
		var kept []interface{}
		hasPhrases := false
		if old, ok := legal["amends"].([]interface{}); ok {
			for _, a := range old {
				if m, ok := a.(map[string]interface{}); ok && m["phrase"] == true {
					hasPhrases = true
					continue
				}
				kept = append(kept, a)
			}
		}
		if hasPhrases && !*force {
			continue
		}
		phrases := vbpl.PhraseAmendsInChunk(r.Content, path, r.Title)
		next := append([]interface{}{}, kept...)
		for _, a := range phrases {
			ref := vietnamese_legal.AmendRef{Document: a.Document, Article: a.Article, Khoan: a.Khoan, Diem: a.Diem, Phrase: true, From: a.From, To: a.To}
			b, _ := json.Marshal(ref)
			var m map[string]interface{}
			_ = json.Unmarshal(b, &m)
			next = append(next, m)
		}
		old, _ := legal["amends"].([]interface{})
		if reflect.DeepEqual(normalize(old), normalize(next)) {
			continue
		}
		changed++
		added += len(phrases)
		if *verbose {
			fmt.Printf("%s  %s\n", r.ID, r.Title)
			for _, a := range phrases {
				fmt.Printf("    %s | Điều %s khoản %s điểm %s | %q → %q\n", a.Document, a.Article, a.Khoan, a.Diem, a.From, a.To)
			}
		}
		if !*apply {
			continue
		}
		amends, _ := json.Marshal(next)
		if len(next) == 0 {
			amends = []byte("[]")
		}
		if err := db.Exec(`UPDATE chunks SET metadata = jsonb_set(metadata, '{legal,amends}', ?::jsonb) WHERE id = ?`,
			string(amends), r.ID).Error; err != nil {
			log.Fatalf("update %s: %v", r.ID, err)
		}
	}
	mode := "dry run"
	if *apply {
		mode = "applied"
	}
	fmt.Printf("%s: %d candidate chunks, %d changed, %d phrase amendments\n", mode, len(rows), changed, added)
}

// normalize makes nil and empty lists compare equal.
func normalize(v []interface{}) []interface{} {
	if len(v) == 0 {
		return nil
	}
	return v
}

func openDB() (*gorm.DB, error) {
	if d := os.Getenv("DB_DRIVER"); d != "" && d != "postgres" {
		return nil, fmt.Errorf("DB_DRIVER=%s: only postgres is supported", d)
	}
	return gorm.Open(postgres.Open(fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"),
	)), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
}
