package repository

import (
	"context"

	"gorm.io/gorm"
)

// txContextKey carries the open transaction of runInTransaction, so the
// repositories a service calls inside it (billing orders and the
// subscriptions they extend) write in one transaction.
type txContextKey struct{}

// connFromContext is the transaction ctx carries, else db; either bound to ctx.
func connFromContext(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txContextKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx)
	}
	return db.WithContext(ctx)
}

// runInTransaction runs fn with a ctx that carries a transaction: committed
// when fn returns nil, rolled back otherwise. Inside an open transaction it
// nests (a savepoint).
func runInTransaction(ctx context.Context, db *gorm.DB, fn func(ctx context.Context) error) error {
	return connFromContext(ctx, db).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txContextKey{}, tx))
	})
}
