package migrate

import (
	"database/sql"
	"embed"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed 900_*.sql 901_*.sql
var testMigrateFS embed.FS

func TestNew(t *testing.T) {
	r := New(nil, testMigrateFS)
	require.NotNil(t, r)
}

func TestRunner_upMigrations_ValidFS(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	assert.NotEmpty(t, migrations)
}

func TestRunner_upMigrations_EmptyFS(t *testing.T) {
	r := New(nil, embed.FS{})
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	assert.Empty(t, migrations)
}

func TestRunner_upMigrations_SkipsDownFiles(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)

	for _, m := range migrations {
		assert.NotContains(t, m.FileName, ".down.",
			"upMigrations should skip .down.sql files")
	}
}

func TestRunner_upMigrations_SortedByVersion(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)

	for i := 1; i < len(migrations); i++ {
		assert.LessOrEqual(t, migrations[i-1].Version, migrations[i].Version,
			"migrations should be sorted by version")
	}
}

func TestRunner_upMigrations_VersionAndName(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	require.NotEmpty(t, migrations)

	for _, m := range migrations {
		assert.NotEmpty(t, m.Version, "version should not be empty")
		assert.NotEmpty(t, m.Name, "name should not be empty")
		assert.NotEmpty(t, m.FileName, "filename should not be empty")
		assert.Contains(t, m.FileName, ".sql")
	}
}

func TestRunner_Up_NilDB(t *testing.T) {
	r := New(nil, testMigrateFS)
	assert.Panics(t, func() {
		_ = r.Up()
	})
}

func TestRunner_Down_ZeroSteps(t *testing.T) {
	r := New(nil, testMigrateFS)
	err := r.Down(0)
	require.NoError(t, err)
}

func TestRunner_Down_NegativeSteps(t *testing.T) {
	r := New(nil, testMigrateFS)
	err := r.Down(-1)
	require.NoError(t, err)
}

func TestRunner_Down_NilDB(t *testing.T) {
	r := New(nil, testMigrateFS)
	assert.Panics(t, func() {
		_ = r.Down(1)
	})
}

func TestRunner_Up_ConnectionError(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	r := New(db, testMigrateFS)
	err = r.Up()
	require.Error(t, err)
}

func TestRunner_Down_ConnectionError(t *testing.T) {
	db, err := sql.Open("postgres", "host=localhost port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	r := New(db, testMigrateFS)
	err = r.Down(1)
	require.Error(t, err)
}

func TestMigration_Fields(t *testing.T) {
	m := Migration{
		Version:  "001",
		Name:     "create_users",
		Applied:  false,
		FileName: "001_create_users.sql",
	}
	assert.Equal(t, "001", m.Version)
	assert.Equal(t, "create_users", m.Name)
	assert.False(t, m.Applied)
	assert.Equal(t, "001_create_users.sql", m.FileName)
}

func TestRunner_upMigrations_Count(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	assert.Len(t, migrations, 2)
}

func TestRunner_upMigrations_VersionPrefix(t *testing.T) {
	r := New(nil, testMigrateFS)
	migrations, err := r.upMigrations()
	require.NoError(t, err)
	require.Len(t, migrations, 2)

	assert.Equal(t, "900", migrations[0].Version)
	assert.Equal(t, "901", migrations[1].Version)
}
