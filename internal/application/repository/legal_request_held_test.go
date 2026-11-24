package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// A copy whose parse failed (or being deleted) holds nothing: the gate may
// file a request again; one still parsing holds the document.
func TestLegalRequestHeldCopiesFollowParseStatus(t *testing.T) {
	repo, db := newLegalRequestRepo(t)
	ctx := context.Background()
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases VALUES ('kb1','VBPL – Nghị định',0,NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id, knowledge_base_id, title, file_name, metadata, parse_status) VALUES
		('k1','kb1','53-2022-ND-CP','a.html','{"external_id":"d1"}','failed'),
		('k2','kb1','54-2022-ND-CP','b.html','{"external_id":"d2"}','processing'),
		('k3','kb1','55-2022-ND-CP','c.html','{"external_id":"d3"}','deleting')`).Error)

	// (ASCII titles: SQLite lower() folds ASCII only)
	for number, want := range map[string]bool{"53-2022-ND-CP": false, "54-2022-ND-CP": true, "55-2022-ND-CP": false} {
		got, err := repo.KnowledgeNamed(ctx, []string{"kb1"}, []string{number})
		require.NoError(t, err)
		require.Equal(t, want, got, number)
	}
	held, err := repo.PublicKnowledgeByExternalIDs(ctx, []string{"d1", "d2", "d3"})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"d2": true}, held)
}
