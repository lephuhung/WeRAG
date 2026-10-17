package container

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	postgresRepo "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	sqliteRetrieverRepo "github.com/Tencent/WeKnora/internal/application/repository/retriever/sqlite"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// NewEngineFactory returns an EngineFactory function closed over db.
// Registered in dig and injected into VectorStoreService for dynamic registry
// updates.
func NewEngineFactory(db *gorm.DB) interfaces.EngineFactory {
	return func(ctx context.Context, store types.VectorStore) (interfaces.RetrieveEngineService, error) {
		return createEngineServiceFromStore(ctx, store, db)
	}
}

// createEngineServiceFromStore creates a RetrieveEngineService from a VectorStore's config.
// This is the DB store counterpart of the env-based initialization in initRetrieveEngineRegistry.
// Postgres (and SQLite, for the unmaintained Lite build) are the only engines;
// both run on the app's own database connection.
func createEngineServiceFromStore(
	_ context.Context,
	store types.VectorStore,
	db *gorm.DB,
) (interfaces.RetrieveEngineService, error) {
	switch store.EngineType {
	case types.PostgresRetrieverEngineType:
		return createPostgresEngine(store, db)
	case types.SQLiteRetrieverEngineType:
		return createSQLiteEngine(store, db)
	default:
		return nil, fmt.Errorf("unsupported engine type: %s", store.EngineType)
	}
}

func createPostgresEngine(store types.VectorStore, db *gorm.DB) (interfaces.RetrieveEngineService, error) {
	if store.ConnectionConfig.UseDefaultConnection {
		repo := postgresRepo.NewPostgresRetrieveEngineRepository(db)
		return retriever.NewKVHybridRetrieveEngine(repo, types.PostgresRetrieverEngineType), nil
	}
	// Phase 1: only UseDefaultConnection is supported.
	// Custom connections require connection pool management and migration handling.
	return nil, fmt.Errorf("custom postgres connections not yet supported; use use_default_connection=true")
}

func createSQLiteEngine(_ types.VectorStore, db *gorm.DB) (interfaces.RetrieveEngineService, error) {
	repo := sqliteRetrieverRepo.NewSQLiteRetrieveEngineRepository(db)
	return retriever.NewKVHybridRetrieveEngine(repo, types.SQLiteRetrieverEngineType), nil
}
