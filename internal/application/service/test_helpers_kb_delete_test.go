package service

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/textproto"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type kbDeleteKBRepo struct {
	fakeKBRepo
	deletedID string
}

func (r *kbDeleteKBRepo) DeleteKnowledgeBase(_ context.Context, id string) error {
	r.deletedID = id
	delete(r.rows, id)
	return nil
}

func (r *kbDeleteKBRepo) GetKBScopeByID(ctx context.Context, id string) (*types.KBScope, error) {
	return nil, nil
}

func (r *kbDeleteKBRepo) ListVisibleKnowledgeBases(ctx context.Context, tenantID uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (r *kbDeleteKBRepo) ListPublicKnowledgeBasesExcept(ctx context.Context, tenantID uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (r *kbDeleteKBRepo) ListForeignKnowledgeBasesByTenantID(ctx context.Context, tenantID uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

type processSyncTenantRepo struct {
	interfaces.TenantRepository
	tenant *types.Tenant
}

func (r *processSyncTenantRepo) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return r.tenant, nil
}

// bytesToFileHeader wraps a []byte into a *multipart.FileHeader so it can be
// consumed by KnowledgeService.CreateKnowledgeFromFile.
func bytesToFileHeader(data []byte, filename string) (*multipart.FileHeader, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	partHeader.Set("Content-Type", "application/octet-stream")

	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, fmt.Errorf("create multipart part: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("write data to part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	reader := multipart.NewReader(&buf, writer.Boundary())
	form, err := reader.ReadForm(int64(len(data)) + 1024)
	if err != nil {
		return nil, fmt.Errorf("read multipart form: %w", err)
	}
	files := form.File["file"]
	if len(files) == 0 {
		return nil, fmt.Errorf("no file in multipart form")
	}
	return files[0], nil
}
