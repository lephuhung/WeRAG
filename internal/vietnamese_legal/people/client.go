package people

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/topology"
)

// Config configures the external MongoDB people store (AIRAG MONGO_*).
type Config struct {
	Enabled    bool
	URI        string // full mongodb:// URI; when set, host/user/... are ignored
	Host       string
	Port       int
	User       string
	Password   string
	Database   string
	AuthSource string
	// QueryTimeout bounds each per-collection query (AIRAG QUERY_MAX_MS=5000).
	// mongo-driver v2 has no maxTimeMS find option, so this is a context
	// deadline per collection scan — same practical effect.
	QueryTimeout time.Duration
	// PerSchemaLimit caps documents read from one collection per lookup.
	PerSchemaLimit int64
}

// busyMessage is returned instead of "not found" when MongoDB is unreachable —
// the caller must relay it verbatim rather than claim the person does not exist.
const busyMessage = "⚠️ Hệ thống tra cứu dữ liệu đang bận hoặc tạm thời không kết nối được. " +
	"Vui lòng thử lại sau giây lát."

// errUnavailable marks infrastructure-level MongoDB failures (distinct from
// a successful query that simply matched nothing).
var errUnavailable = errors.New("mongodb unavailable")

// Service searches person records across the mapped MongoDB collections.
type Service struct {
	cfg    Config
	once   sync.Once
	client *mongo.Client
	db     *mongo.Database
}

// NewService builds the service. cfg.Enabled=false yields a disabled service
// whose Enabled() reports false and whose searches fail fast.
func NewService(cfg Config) *Service {
	if cfg.QueryTimeout <= 0 {
		cfg.QueryTimeout = 5 * time.Second
	}
	if cfg.PerSchemaLimit <= 0 {
		cfg.PerSchemaLimit = 10
	}
	return &Service{cfg: cfg}
}

// Enabled reports whether people search is configured on.
func (s *Service) Enabled() bool { return s != nil && s.cfg.Enabled }

func (s *Service) buildURI() string {
	if s.cfg.URI != "" {
		return s.cfg.URI
	}
	host := s.cfg.Host
	if host == "" {
		host = "localhost"
	}
	port := s.cfg.Port
	if port <= 0 {
		port = 27017
	}
	if s.cfg.User != "" && s.cfg.Password != "" {
		authSource := s.cfg.AuthSource
		if authSource == "" {
			authSource = "admin"
		}
		return fmt.Sprintf("mongodb://%s:%s@%s:%d/?authSource=%s",
			s.cfg.User, s.cfg.Password, host, port, authSource)
	}
	return fmt.Sprintf("mongodb://%s:%d/", host, port)
}

// database lazily connects the client (first call ~3s when the server is down).
func (s *Service) database(ctx context.Context) (*mongo.Database, error) {
	if !s.cfg.Enabled {
		return nil, errUnavailable
	}
	var initErr error
	s.once.Do(func() {
		uri := s.buildURI()
		opts := options.Client().
			ApplyURI(uri).
			SetServerSelectionTimeout(3 * time.Second).
			SetConnectTimeout(3 * time.Second).
			SetMaxPoolSize(50)
		client, err := mongo.Connect(opts)
		if err != nil {
			initErr = err
			return
		}
		// Force the handshake now (pymongo-style warmup) so a dead server is
		// classified as "unavailable" before any real query runs.
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
			initErr = err
			return
		}
		dbName := s.cfg.Database
		if dbName == "" {
			dbName = "people_db"
		}
		s.client = client
		s.db = client.Database(dbName)
		logger.GetLogger(ctx).Infof("[people] MongoDB connected (%s)", dbName)
	})
	if initErr != nil {
		return nil, fmt.Errorf("%w: %v", errUnavailable, initErr)
	}
	return s.db, nil
}

// Close releases the client (called on shutdown; safe when never connected).
func (s *Service) Close(ctx context.Context) {
	if s.client != nil {
		_ = s.client.Disconnect(ctx)
	}
}

// isConnError reports whether err is a connection/infrastructure failure —
// the "system busy" case, not an ordinary query error.
func isConnError(err error) bool {
	var sse topology.ServerSelectionError
	if errors.As(err, &sse) {
		return true
	}
	if errors.Is(err, mongo.ErrClientDisconnected) {
		return true
	}
	var nerr net.Error
	return errors.As(err, &nerr)
}

// isTimeout reports whether err is a per-query timeout — either our context
// deadline or a server-side maxTimeMS/execution limit (code 50).
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ce mongo.CommandError
	return errors.As(err, &ce) && ce.HasErrorCode(50)
}
