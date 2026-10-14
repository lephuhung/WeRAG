// Command notecheck prints the "Lưu ý hiệu lực" block the backend would
// append to an answer read from stdin (DSN in WERAG_DSN).
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/service"
)

func main() {
	db, err := gorm.Open(postgres.Open(os.Getenv("WERAG_DSN")), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	answer, _ := io.ReadAll(os.Stdin)
	fmt.Printf("%q\n", service.LegalValidityNote(context.Background(), db.Debug(), string(answer)))
}
