package file

import (
	"testing"
)

func TestStorageClientsRejectUnsafeEndpointAtConstruction(t *testing.T) {
	const endpoint = "http://169.254.169.254/latest/meta-data"
	tests := map[string]func() error{
		"minio": func() error {
			_, err := newMinioClient(endpoint, "ak", "sk", "bucket", false)
			return err
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("expected unsafe endpoint to be rejected")
			}
		})
	}
}
