//go:build legaldev

package service

// A read-only harness against a real database (only SELECTs):
//
//	LEGAL_AMEND_DSN='host=172.20.0.15 user=postgres password=… dbname=WeKnora sslmode=disable' \
//	LEGAL_AMEND_DOC=68/2026/NĐ-CP LEGAL_AMEND_ARTICLES=3,4 \
//	go test -tags legaldev ./internal/application/service/ -run TestLegalAmendDev -v
//
// It loads the chunks of those articles from the public KBs, resolves them
// as of the dates in LEGAL_AMEND_AT (default 2025-12-31,today) and prints
// each khoản's state and the model text.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestLegalAmendDev(t *testing.T) {
	dsn := os.Getenv("LEGAL_AMEND_DSN")
	if dsn == "" {
		t.Skip("LEGAL_AMEND_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// read-only session: any write fails
	db = db.Session(&gorm.Session{})
	if err := db.Exec(`SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY`).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	number := envOr("LEGAL_AMEND_DOC", "68/2026/NĐ-CP")
	articles := strings.Split(envOr("LEGAL_AMEND_ARTICLES", "3,4"), ",")
	var public []string
	if err := db.Raw(`SELECT id FROM knowledge_bases WHERE deleted_at IS NULL AND visibility = 'public'`).Scan(&public).Error; err != nil {
		t.Fatal(err)
	}
	var chunkIDs []string
	if err := db.Raw(`SELECT c.id FROM chunks c JOIN knowledges k ON k.id = c.knowledge_id
		WHERE c.deleted_at IS NULL AND k.deleted_at IS NULL AND k.metadata->>'doc_number' = ?
		AND c.knowledge_base_id IN ? AND jsonb_exists_any(c.metadata->'legal'->'article_nos', ?::text[])
		ORDER BY c.chunk_index`, number, public, "{"+strings.Join(articles, ",")+"}").Scan(&chunkIDs).Error; err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	start := time.Now()
	load, err := LoadLegalAmendments(ctx, db, public, chunkIDs, public, loc)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("loaded %d chunk(s) of %s in %v\n", len(load.Chunks), number, time.Since(start).Round(time.Millisecond))
	for key, list := range load.Edges {
		fmt.Printf("edges on %s: %d\n", key, len(list))
		for _, e := range list {
			fmt.Printf("  %-14s %-22s %-10s eff=%s tier=%s from=%q to=%q new=%q quote=%d runes unresolved=%q rejected=%q withheld=%v\n",
				e.Op, e.Provision.String(), e.Amender.Number, e.EffFrom.text(), e.Tier, e.From, e.To, e.NewLabel,
				len([]rune(e.QuoteText)), e.Unresolved, e.Rejected, e.TextWithheld)
		}
	}
	for aid, cat := range load.Catalogue {
		for _, c := range cat {
			fmt.Printf("catalogue: %s amended by %s (eff %s)\n", aid, c.Amender.Number, c.EffFrom.text())
		}
	}
	allowed := map[string]bool{}
	for _, id := range public {
		allowed[id] = true
	}
	for _, at := range strings.Split(envOr("LEGAL_AMEND_AT", "2025-12-31,"+time.Now().In(loc).Format("2006-01-02")), ",") {
		asOf := legalDay(at, loc).At
		fmt.Printf("\n===== as of %s =====\n", asOf.Format("02/01/2006"))
		res := load.Resolve(asOf, allowed)
		for _, c := range load.Chunks {
			r := res[c.ID]
			fmt.Printf("--- chunk %s (%v) identity=%q state=%s\n", c.ID, c.Legal.ArticleNos, c.IdentityProblem, r.State)
			for _, u := range r.Units {
				fmt.Printf("  %-22s %-28s %s\n", u.Provision.String(), u.State, strings.Join(u.Reasons, " | "))
			}
			if os.Getenv("LEGAL_AMEND_TEXT") != "" {
				fmt.Println(r.ModelContent)
			}
			if at != "2025-12-31" {
				old := LegalCurrentWording(ctx, db, public, []string{c.ID}, asOf)
				oldText, changed := old[c.ID]
				if !changed {
					oldText = c.Content
				}
				fmt.Printf("  same as LegalCurrentWording: %v (old changed it: %v)\n", oldText == r.ModelContent, changed)
				if os.Getenv("LEGAL_AMEND_TEXT") != "" && oldText != r.ModelContent {
					fmt.Println("  --- old model_content ---")
					fmt.Println(oldText)
				}
			}
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
