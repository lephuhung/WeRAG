package utils

import (
	"net/http"
	"testing"
	"time"
)

func TestEnvPositiveInt(t *testing.T) {
	const key = "WEKNORA_TEST_POSITIVE_INT"
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{name: "empty keeps default", value: "", want: 7},
		{name: "blank keeps default", value: "   ", want: 7},
		{name: "parsed", value: "21", want: 21},
		{name: "spaces trimmed", value: " 21 ", want: 21},
		{name: "unparseable keeps default", value: "abc", want: 7},
		{name: "zero keeps default", value: "0", want: 7},
		{name: "negative keeps default", value: "-3", want: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Always set the variable, empty included, so no case depends on
			// the ambient environment leaving it unset. EnvPositiveInt treats
			// empty as unset.
			t.Setenv(key, tt.value)
			if got := EnvPositiveInt(key, 7); got != tt.want {
				t.Fatalf("EnvPositiveInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWithUpstreamIdleConnPool(t *testing.T) {
	t.Run("nil transport is returned as nil", func(t *testing.T) {
		if got := WithUpstreamIdleConnPool(nil); got != nil {
			t.Fatalf("WithUpstreamIdleConnPool(nil) = %v, want nil", got)
		}
	})

	t.Run("defaults applied and transport returned in place", func(t *testing.T) {
		t.Setenv("WEKNORA_HTTP_MAX_IDLE_CONNS_PER_HOST", "")
		in := &http.Transport{}
		got := WithUpstreamIdleConnPool(in)
		if got != in {
			t.Fatal("expected the same *http.Transport back, not a copy")
		}
		if in.MaxIdleConnsPerHost != defaultMaxIdleConnsPerHost {
			t.Errorf("MaxIdleConnsPerHost = %d, want %d",
				in.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost)
		}
		if in.MaxIdleConns != defaultMaxIdleConns {
			t.Errorf("MaxIdleConns = %d, want %d", in.MaxIdleConns, defaultMaxIdleConns)
		}
		// Left at 0 an idle connection is kept forever, which a large pool
		// makes riskier against upstreams that close keep-alives early.
		if in.IdleConnTimeout != defaultIdleConnTimeout {
			t.Errorf("IdleConnTimeout = %s, want %s",
				in.IdleConnTimeout, defaultIdleConnTimeout)
		}
	})

	t.Run("caller's IdleConnTimeout is preserved", func(t *testing.T) {
		t.Setenv("WEKNORA_HTTP_MAX_IDLE_CONNS_PER_HOST", "")
		in := &http.Transport{IdleConnTimeout: 30 * time.Second}
		WithUpstreamIdleConnPool(in)
		if in.IdleConnTimeout != 30*time.Second {
			t.Fatalf("IdleConnTimeout = %s, want it left at 30s", in.IdleConnTimeout)
		}
	})

	t.Run("env override raises both caps", func(t *testing.T) {
		t.Setenv("WEKNORA_HTTP_MAX_IDLE_CONNS_PER_HOST", "100")
		in := WithUpstreamIdleConnPool(&http.Transport{})
		if in.MaxIdleConnsPerHost != 100 {
			t.Errorf("MaxIdleConnsPerHost = %d, want 100", in.MaxIdleConnsPerHost)
		}
		if in.MaxIdleConns != 400 {
			t.Errorf("MaxIdleConns = %d, want 400 (4x per-host)", in.MaxIdleConns)
		}
	})

	t.Run("small override keeps the total floor", func(t *testing.T) {
		t.Setenv("WEKNORA_HTTP_MAX_IDLE_CONNS_PER_HOST", "4")
		in := WithUpstreamIdleConnPool(&http.Transport{})
		if in.MaxIdleConnsPerHost != 4 {
			t.Errorf("MaxIdleConnsPerHost = %d, want 4", in.MaxIdleConnsPerHost)
		}
		if in.MaxIdleConns != defaultMaxIdleConns {
			t.Errorf("MaxIdleConns = %d, want the %d floor", in.MaxIdleConns, defaultMaxIdleConns)
		}
	})

	t.Run("invalid override keeps the default", func(t *testing.T) {
		t.Setenv("WEKNORA_HTTP_MAX_IDLE_CONNS_PER_HOST", "-1")
		in := WithUpstreamIdleConnPool(&http.Transport{})
		if in.MaxIdleConnsPerHost != defaultMaxIdleConnsPerHost {
			t.Errorf("MaxIdleConnsPerHost = %d, want %d",
				in.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost)
		}
	})
}
