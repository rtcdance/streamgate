package storage

import (
	"context"
	"database/sql"
	"embed"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewMigrator(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
	require.NotNil(t, m)
	assert.Equal(t, "schema_migrations", m.table)
}

func TestMigrator_SetTableName_Valid(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
	err := m.SetTableName("custom_migrations")
	assert.NoError(t, err)
	assert.Equal(t, "custom_migrations", m.table)
}

func TestMigrator_SetTableName_WithUnderscore(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
	err := m.SetTableName("my_custom_table")
	assert.NoError(t, err)
	assert.Equal(t, "my_custom_table", m.table)
}

func TestMigrator_SetTableName_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"with space", "my table"},
		{"with dash", "my-table"},
		{"with dot", "my.table"},
		{"with semicolon", "my;table"},
		{"with special", "my$table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
			err := m.SetTableName(tt.input)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid table name")
		})
	}
}

func TestMigrator_SetTableName_Alphanumeric(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
	err := m.SetTableName("Table123")
	assert.NoError(t, err)
	assert.Equal(t, "Table123", m.table)
}

//go:embed testdata
var testMigrationFS embed.FS

func TestMigrator_readMigrationFiles(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")

	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	assert.NotEmpty(t, files)
}

func TestMigrator_readMigrationFiles_EmbeddedFS(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "nonexistent")

	files, err := m.readMigrationFiles()
	assert.Error(t, err)
	assert.Nil(t, files)
}

func TestMigrator_readDownFiles(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")

	downFiles, err := m.readDownFiles()
	require.NoError(t, err)
	assert.NotEmpty(t, downFiles)
}

func TestMigrator_readDownFiles_NonexistentDir(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "nonexistent")

	_, err := m.readDownFiles()
	assert.Error(t, err)
}

func TestMigrator_Up_NilDB(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	assert.Panics(t, func() {
		_ = m.Up(context.Background())
	})
}

func TestMigrator_Down_NilDB(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	assert.Panics(t, func() {
		_ = m.Down(context.Background(), 0)
	})
}

func TestMigrator_Force_NilDB(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	assert.Panics(t, func() {
		_ = m.Force(context.Background(), 1)
	})
}

func TestMigrator_Version_NilDB(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	assert.Panics(t, func() {
		_, _, _ = m.Version(context.Background())
	})
}

func TestMigrationFile_Fields(t *testing.T) {
	f := migrationFile{
		Version: "001",
		Name:    "create_users",
		Content: "CREATE TABLE users (id SERIAL PRIMARY KEY);",
	}
	assert.Equal(t, "001", f.Version)
	assert.Equal(t, "create_users", f.Name)
	assert.Equal(t, "CREATE TABLE users (id SERIAL PRIMARY KEY);", f.Content)
}

func TestMigrator_readMigrationFiles_Sorting(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")

	files, err := m.readMigrationFiles()
	require.NoError(t, err)

	for i := 1; i < len(files); i++ {
		assert.LessOrEqual(t, files[i-1].Version, files[i].Version,
			"migration files should be sorted by version")
	}
}

func TestMigrator_readMigrationFiles_SkipsDownFiles(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")

	files, err := m.readMigrationFiles()
	require.NoError(t, err)

	for _, f := range files {
		assert.NotContains(t, f.Name, ".down.",
			"readMigrationFiles should skip .down.sql files")
	}
}

func TestMigrator_readDownFiles_OnlyDownFiles(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")

	downFiles, err := m.readDownFiles()
	require.NoError(t, err)

	for version, content := range downFiles {
		assert.NotEmpty(t, version)
		assert.NotEmpty(t, content)
	}
}

func TestRunEmbeddedMigrations_NilDB(t *testing.T) {
	assert.Panics(t, func() {
		_ = RunEmbeddedMigrations(context.Background(), nil, testMigrationFS, "testdata/migrations")
	})
}

func newFailingDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	return db
}

func TestMigrator_Up_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Force_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Force(context.Background(), 1)
	assert.Error(t, err)
}

func TestMigrator_Version_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, _, err := m.Version(context.Background())
	assert.Error(t, err)
}

func TestMigrator_ensureMigrationsTable_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.ensureMigrationsTable(context.Background())
	assert.Error(t, err)
}

func TestMigrator_getAppliedMigrations_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, err := m.getAppliedMigrations(context.Background())
	assert.Error(t, err)
}

func TestMigrator_hasDirtyMigration_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, err := m.hasDirtyMigration(context.Background())
	assert.Error(t, err)
}

func TestMigrator_ensureNoDirty_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.ensureNoDirty(context.Background(), map[string]bool{})
	assert.Error(t, err)
}

func TestMigrator_applyMigration_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.applyMigration(context.Background(), f)
	assert.Error(t, err)
}

func TestMigrator_rollbackMigration_ConnectionError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.rollbackMigration(context.Background(), f, map[string]string{"001": "DROP TABLE test"})
	assert.Error(t, err)
}

func TestMigrator_rollbackMigration_NoDownFile(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.rollbackMigration(context.Background(), f, map[string]string{})
	assert.Error(t, err)
}

func TestMigrator_Up_WithTimeout(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := m.Up(ctx)
	assert.Error(t, err)
}

func TestMigrator_Down_WithTimeout(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := m.Down(ctx, 0)
	assert.Error(t, err)
}

func TestMigrator_readMigrationFiles_SkipsDirectories(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	for _, f := range files {
		assert.NotContains(t, f.Name, "/")
	}
}

func TestMigrator_readMigrationFiles_SkipsNonSQL(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	for _, f := range files {
		assert.NotEqual(t, "readme", f.Name)
	}
}

func TestMigrator_readMigrationFiles_SkipsNoUnderscore(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	for _, f := range files {
		assert.Contains(t, f.Version, "0")
	}
}

func TestMigrator_readDownFiles_SkipsNonDown(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata/migrations")
	downFiles, err := m.readDownFiles()
	require.NoError(t, err)
	assert.NotEmpty(t, downFiles)
}

func TestMigrator_SetTableName_Empty(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
	err := m.SetTableName("")
	assert.NoError(t, err)
}

func TestMigrator_Up_AdvisoryLockFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "acquire migration lock")
}

func TestMigrator_Down_AdvisoryLockFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "acquire migration lock")
}

func TestMigrator_Force_EnsureTableFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Force(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ensure migrations table")
}

func TestMigrator_Version_EnsureTableFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, _, err := m.Version(context.Background())
	assert.Error(t, err)
}

func TestMigrator_hasDirtyMigration_QueryError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, err := m.hasDirtyMigration(context.Background())
	assert.Error(t, err)
}

func TestMigrator_ensureNoDirty_WithDirtyRows(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.ensureNoDirty(context.Background(), map[string]bool{"001": true})
	assert.Error(t, err)
}

func TestMigrator_rollbackMigration_MarkDirtyFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.rollbackMigration(context.Background(), f, map[string]string{})
	assert.Error(t, err)
}

func TestMigrator_applyMigration_InsertFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	f := migrationFile{Version: "001", Name: "test", Content: "SELECT 1"}
	err := m.applyMigration(context.Background(), f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "record migration start")
}

func TestMigrator_getAppliedMigrations_QueryError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	_, err := m.getAppliedMigrations(context.Background())
	assert.Error(t, err)
}

func TestMigrator_ensureMigrationsTable_ExecError(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.ensureMigrationsTable(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Up_DirtyMigrationCheckFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_Down_ReadFilesFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), embed.FS{}, "nonexistent")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Down_ReadDownFilesFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), embed.FS{}, "nonexistent")
	err := m.Down(context.Background(), 0)
	assert.Error(t, err)
}

func TestMigrator_Up_ReadFilesFailed(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), embed.FS{}, "nonexistent")
	err := m.Up(context.Background())
	assert.Error(t, err)
}

func TestMigrator_readMigrationFiles_NoSQLFiles(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata")
	files, err := m.readMigrationFiles()
	require.NoError(t, err)
	for _, f := range files {
		assert.NotContains(t, f.Name, "/")
	}
}

func TestMigrator_readDownFiles_NoDownFiles(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), testMigrationFS, "testdata")
	downFiles, err := m.readDownFiles()
	require.NoError(t, err)
	for version, content := range downFiles {
		assert.NotEmpty(t, version)
		assert.NotEmpty(t, content)
	}
}

func TestMigrator_SetTableName_WithNumbers(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop(), embed.FS{}, "migrations")
	err := m.SetTableName("table2024")
	assert.NoError(t, err)
	assert.Equal(t, "table2024", m.table)
}

func TestMigrator_Down_InvalidVersion(t *testing.T) {
	db := newFailingDB(t)
	defer db.Close()

	m := NewMigrator(db, zap.NewNop(), testMigrationFS, "testdata/migrations")
	err := m.Down(context.Background(), 999)
	assert.Error(t, err)
}
