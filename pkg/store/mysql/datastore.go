package mysql

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Datastore wraps GORM DB and provides transaction support
type Datastore struct {
	db *gorm.DB
}

// NewDatastore creates a new MySQL datastore
func NewDatastore(dsn string) (*Datastore, error) {
	// Configure GORM logger
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             500 * time.Millisecond, // Slow SQL threshold
			LogLevel:                  logger.Warn,            // Log level (Warn in production, Info in dev)
			IgnoreRecordNotFoundError: true,                   // Ignore ErrRecordNotFound
			Colorful:                  true,                   // Enable color
		},
	)

	// Open database connection
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: newLogger,
		// Disable default transaction for better performance
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Get underlying *sql.DB and configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get generic database object: %w", err)
	}

	// Connection pool settings
	sqlDB.SetMaxOpenConns(100)                  // Maximum open connections
	sqlDB.SetMaxIdleConns(10)                   // Maximum idle connections
	sqlDB.SetConnMaxLifetime(time.Hour)         // Connection max lifetime
	sqlDB.SetConnMaxIdleTime(10 * time.Minute) // Connection max idle time

	return &Datastore{db: db}, nil
}

// Close closes the database connection
func (ds *Datastore) Close() error {
	sqlDB, err := ds.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Transaction support using context
type contextTxKey struct{}

// ExecTx executes a function within a transaction
// If the function returns an error, the transaction is rolled back
// Otherwise, the transaction is committed
func (ds *Datastore) ExecTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return ds.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ctx = context.WithValue(ctx, contextTxKey{}, tx)
		return fn(ctx)
	})
}

// DB returns the GORM DB instance for the current context
// If a transaction is active in the context, it returns the transaction DB
// Otherwise, it returns the main DB
func (ds *Datastore) DB(ctx context.Context) *gorm.DB {
	tx, ok := ctx.Value(contextTxKey{}).(*gorm.DB)
	if ok {
		return tx.WithContext(ctx)
	}
	return ds.db.WithContext(ctx)
}

// GetDB returns the underlying GORM DB instance (for direct access if needed)
func (ds *Datastore) GetDB() *gorm.DB {
	return ds.db
}
