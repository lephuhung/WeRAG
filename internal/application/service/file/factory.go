package file

import (
	"fmt"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// NewFileServiceFromStorageConfig builds a provider-specific FileService from tenant storage config.
// provider can be empty; in that case it falls back to sec.DefaultProvider.
// Returns the resolved provider name together with the service.
func NewFileServiceFromStorageConfig(
	provider string,
	sec *types.StorageEngineConfig,
	localBaseDir string,
) (interfaces.FileService, string, error) {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" && sec != nil {
		p = strings.ToLower(strings.TrimSpace(sec.DefaultProvider))
	}
	if p == "" {
		return nil, "", fmt.Errorf("empty provider")
	}

	if localBaseDir == "" {
		localBaseDir = strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	}
	if localBaseDir == "" {
		localBaseDir = "/data/files"
	}

	switch p {
	case "local":
		baseDir := localBaseDir
		if sec != nil && sec.Local != nil {
			if safeBaseDir, err := secutils.SafeJoinUnderBase(baseDir, sec.Local.PathPrefix); err == nil {
				baseDir = safeBaseDir
			}
		}
		externalURL := strings.TrimSpace(os.Getenv("APP_EXTERNAL_URL"))
		return NewLocalFileService(baseDir, externalURL), p, nil

	case "minio":
		if sec == nil || sec.MinIO == nil {
			return nil, p, fmt.Errorf("missing minio config")
		}
		var endpoint, accessKeyID, secretAccessKey string
		if sec.MinIO.Mode == "remote" {
			endpoint = strings.TrimSpace(sec.MinIO.Endpoint)
			accessKeyID = strings.TrimSpace(sec.MinIO.AccessKeyID)
			secretAccessKey = strings.TrimSpace(sec.MinIO.SecretAccessKey)
		} else {
			endpoint = strings.TrimSpace(os.Getenv("MINIO_ENDPOINT"))
			accessKeyID = strings.TrimSpace(os.Getenv("MINIO_ACCESS_KEY_ID"))
			secretAccessKey = strings.TrimSpace(os.Getenv("MINIO_SECRET_ACCESS_KEY"))
		}
		bucketName := strings.TrimSpace(sec.MinIO.BucketName)
		if bucketName == "" {
			bucketName = strings.TrimSpace(os.Getenv("MINIO_BUCKET_NAME"))
		}
		if endpoint == "" || accessKeyID == "" || secretAccessKey == "" || bucketName == "" {
			return nil, p, fmt.Errorf("incomplete minio config")
		}
		svc, err := NewMinioFileService(endpoint, accessKeyID, secretAccessKey, bucketName, sec.MinIO.UseSSL)
		return svc, p, err

	default:
		return nil, p, fmt.Errorf("unsupported provider %q", p)
	}
}
