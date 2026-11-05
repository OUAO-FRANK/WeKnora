package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCountOrphansNullInLinks(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var db *gorm.DB
			if dialect == "sqlite" {
				db = setupWikiPagesTestDB(t)
			} else {
				dsn := os.Getenv("WEKNORA_REPOSITORY_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("set WEKNORA_REPOSITORY_TEST_POSTGRES_DSN for PostgreSQL orphan counts")
				}
				admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := admin.DB()
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
				db = admin.Begin()
				require.NoError(t, db.Error)
				t.Cleanup(func() { require.NoError(t, db.Rollback().Error) })
				// A connection-local table isolates the fixture from any existing
				// wiki data and disappears when the transaction is rolled back.
				ddl := strings.Replace(wikiPagesTestDDL,
					"CREATE TABLE IF NOT EXISTS wiki_pages", "CREATE TEMP TABLE wiki_pages", 1)
				ddl = strings.ReplaceAll(ddl, "DATETIME", "TIMESTAMPTZ")
				ddl = strings.Replace(ddl, "in_links          TEXT", "in_links          JSONB", 1)
				require.NoError(t, db.Exec(ddl).Error)
			}

			repo := NewWikiPageRepository(db)
			ctx := context.Background()
			for _, tc := range []struct {
				slug    string
				links   any
				kind    string
				status  string
				otherKB bool
				deleted bool
			}{
				{slug: "sql-null", links: nil},
				{slug: "json-null", links: "null"},
				{slug: "empty", links: "[]"},
				{slug: "linked", links: `["entity/source"]`},
				{slug: "index", links: "null", kind: types.WikiPageTypeIndex},
				{slug: "archived", links: "null", status: types.WikiPageStatusArchived},
				{slug: "other-kb", links: "null", otherKB: true},
				{slug: "deleted", links: "null", deleted: true},
			} {
				kbID := "kb-null-inlinks"
				if tc.otherKB {
					kbID = "kb-other"
				}
				kind, status := tc.kind, tc.status
				if kind == "" {
					kind = types.WikiPageTypeEntity
				}
				if status == "" {
					status = types.WikiPageStatusPublished
				}
				page := makeWikiPage(kbID, tc.slug, kind, status)
				require.NoError(t, repo.Create(ctx, page))
				updates := map[string]any{"in_links": tc.links}
				if tc.deleted {
					updates["deleted_at"] = time.Now()
				}
				require.NoError(t, db.Model(&types.WikiPage{}).Where("id = ?", page.ID).Updates(updates).Error)
			}
			got, err := repo.CountOrphans(ctx, "kb-null-inlinks")
			require.NoError(t, err)
			require.Equal(t, int64(3), got, "SQL NULL, JSON null and an empty array all have no inbound links")
		})
	}
}
