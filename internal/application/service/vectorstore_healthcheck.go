package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
)

const connectionTestTimeout = 10 * time.Second

// TestConnection tests connectivity to a vector database.
// Returns the detected server version on success, empty string if unknown.
func (s *vectorStoreService) TestConnection(
	ctx context.Context,
	engineType types.RetrieverEngineType,
	config types.ConnectionConfig,
) (string, error) {
	switch engineType {
	case types.PostgresRetrieverEngineType:
		return testPostgresConnection(ctx, config)
	default:
		return "", errors.NewBadRequestError(
			fmt.Sprintf("connection test not supported for engine type: %s", engineType))
	}
}

func testPostgresConnection(ctx context.Context, config types.ConnectionConfig) (string, error) {
	testCtx, cancel := context.WithTimeout(ctx, connectionTestTimeout)
	defer cancel()

	if config.UseDefaultConnection {
		// Using the default app DB connection — always reachable if the app is running.
		// Cannot query version without a DB handle; return empty.
		return "", nil
	}

	db, err := sql.Open("pgx", config.Addr)
	if err != nil {
		return "", errors.NewBadRequestError("failed to create postgres connection: invalid configuration")
	}
	defer db.Close()

	if err := db.PingContext(testCtx); err != nil {
		logger.Warnf(ctx, "Postgres connection test failed: %v", err)
		return "", errors.NewBadRequestError("failed to connect to postgres: connection refused or authentication failed")
	}

	// Detect version
	var version string
	if err := db.QueryRowContext(testCtx, "SHOW server_version").Scan(&version); err != nil {
		logger.Warnf(ctx, "Postgres version detection failed: %v", err)
		return "", nil // connected but version unknown
	}

	return version, nil
}
