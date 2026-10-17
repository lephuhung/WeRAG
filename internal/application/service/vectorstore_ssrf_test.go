package service

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

// withSSRFWhitelist sets SSRF_WHITELIST for the duration of a test, resetting
// the cached singleton both before and after so neither this test nor a
// neighbour sees a stale whitelist (see ResetSSRFWhitelistForTest docs).
func withSSRFWhitelist(t *testing.T, whitelist string) {
	t.Helper()
	utils.ResetSSRFWhitelistForTest()
	require.NoError(t, os.Setenv("SSRF_WHITELIST", whitelist))
	t.Cleanup(func() {
		_ = os.Unsetenv("SSRF_WHITELIST")
		utils.ResetSSRFWhitelistForTest()
	})
}

func TestValidateConnectionAddrSSRF(t *testing.T) {
	// Whitelist a benign host so a missing address mapping is the only way
	// an engine can be rejected.
	withSSRFWhitelist(t, "vector.allowed.test")

	tests := []struct {
		name       string
		engineType types.RetrieverEngineType
		config     types.ConnectionConfig
		wantError  bool
	}{
		{
			name:       "sqlite skipped (no remote address)",
			engineType: types.SQLiteRetrieverEngineType,
			config:     types.ConnectionConfig{},
			wantError:  false,
		},
		{
			// postgres only runs on the app's own connection; it has no
			// DB-store address mapping.
			name:       "postgres fails closed",
			engineType: types.PostgresRetrieverEngineType,
			config:     types.ConnectionConfig{Addr: "postgres://u:p@vector.allowed.test:5432/db"},
			wantError:  true,
		},
		{
			name:       "removed engine fails closed",
			engineType: types.RetrieverEngineType("qdrant"),
			config:     types.ConnectionConfig{Host: "vector.allowed.test", Port: 6334},
			wantError:  true,
		},
		{
			name:       "unknown engine fails closed",
			engineType: types.RetrieverEngineType("some-future-engine"),
			config:     types.ConnectionConfig{Addr: "http://vector.allowed.test"},
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConnectionAddrSSRF(tt.engineType, tt.config)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidateConnectionAddrSSRF_Completeness guards against a future engine
// being added to validEngineTypes without an SSRF address mapping. Such an
// engine would fall into the fail-closed default branch and error even with a
// whitelisted address — this test catches that at build/test time. No engine
// is registerable today, so the loop is empty until one is added.
func TestValidateConnectionAddrSSRF_Completeness(t *testing.T) {
	withSSRFWhitelist(t, "any.allowed.test")

	// Derive the engine list from the live registry (GetVectorStoreTypes is
	// backed by validEngineTypes) rather than a hard-coded slice, so a newly
	// added registerable engine that lacks an SSRF address mapping fails this
	// test.
	for _, st := range types.GetVectorStoreTypes() {
		et := types.RetrieverEngineType(st.Type)
		if !types.IsValidEngineType(et) {
			continue // env-only / legacy engines are not user-registerable
		}
		// All address fields point at a whitelisted host, so the only way to
		// get an error is the fail-closed default branch (= missing mapping).
		err := validateConnectionAddrSSRF(et, types.ConnectionConfig{
			Addr:        "any.allowed.test",
			Host:        "any.allowed.test",
			GrpcAddress: "any.allowed.test",
		})
		require.NoErrorf(t, err,
			"engine %q is registerable but has no SSRF address mapping (fell into fail-closed default)", et)
	}
}

func TestTestRawConnection_Rejections(t *testing.T) {
	withSSRFWhitelist(t, "vector.allowed.test")
	repo := &mockVectorStoreRepo{}
	svc := NewVectorStoreService(repo, nil, nil, nil, nil)

	tests := []struct {
		name       string
		engineType types.RetrieverEngineType
		config     types.ConnectionConfig
	}{
		{
			// postgres is not a user-registerable vector store; raw-testing it
			// would otherwise dial the app's own DB host (credential oracle).
			name:       "postgres rejected by engine allowlist",
			engineType: types.PostgresRetrieverEngineType,
			config:     types.ConnectionConfig{Addr: "postgres://u:p@vector.allowed.test:5432/db"},
		},
		{
			name:       "sqlite rejected by engine allowlist",
			engineType: types.SQLiteRetrieverEngineType,
			config:     types.ConnectionConfig{},
		},
		{
			name:       "removed engine rejected by engine allowlist",
			engineType: types.RetrieverEngineType("elasticsearch"),
			config:     types.ConnectionConfig{Addr: "http://vector.allowed.test:9200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.TestRawConnection(context.Background(), tt.engineType, tt.config)
			require.Error(t, err)
		})
	}
}
