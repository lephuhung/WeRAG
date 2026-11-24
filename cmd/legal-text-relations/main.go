// legal-text-relations reads the relations the held texts of a catalogue
// source state (service.BuildLegalTextRelations) and stores them under the
// "toàn văn" label: the same backfill as POST
// /api/v1/system/legal-relations/rebuild-text, runnable from a shell. Of a
// vbpl.vn document only "succeeds" (a transitional clause naming the
// document it takes over from) is kept; a feed document keeps every text
// relation. The database comes from the same DB_* environment as the server.
//
//	legal-text-relations -source vbpl
//	legal-text-relations -ids 112057,a0fff6e0-…
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func main() {
	source := flag.String("source", "", "catalogue source to rebuild (vbpl, dangcongsan, tvpl)")
	ids := flag.String("ids", "", "comma-separated catalogue document ids instead of a whole source")
	flag.Parse()
	var docIDs []string
	for _, id := range strings.Split(*ids, ",") {
		if id = strings.TrimSpace(id); id != "" {
			docIDs = append(docIDs, id)
		}
	}
	if *source == "" && len(docIDs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: legal-text-relations -source <source> | -ids <id,...>")
		os.Exit(2)
	}
	if d := os.Getenv("DB_DRIVER"); d != "" && d != "postgres" {
		log.Fatalf("unsupported DB_DRIVER %q: Postgres only", d)
	}
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Warn)})
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	start := time.Now()
	stats, err := service.BuildLegalTextRelations(context.Background(), db, *source, docIDs)
	if stats != nil {
		fmt.Printf("documents=%d without_text=%d citations=%d unresolved=%d relations=%d in %s\n",
			stats.Documents, stats.WithoutText, stats.Citations, stats.Unresolved, stats.Relations,
			time.Since(start).Round(time.Second))
	}
	if err != nil {
		log.Fatalf("rebuild: %v", err)
	}
}
