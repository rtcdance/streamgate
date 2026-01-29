package benchmark_test

import (
	"testing"
)

func BenchmarkPostgres_Insert(b *testing.B) {
	b.Skip("PostgresDB doesn't have Insert method - skipping benchmark")
}

func BenchmarkPostgres_Query(b *testing.B) {
	b.Skip("PostgresDB requires database connection - skipping benchmark")
}

func BenchmarkPostgres_Update(b *testing.B) {
	b.Skip("PostgresDB doesn't have Update method - skipping benchmark")
}

func BenchmarkPostgres_Delete(b *testing.B) {
	b.Skip("PostgresDB doesn't have Delete method - skipping benchmark")
}

func BenchmarkPostgres_Transaction(b *testing.B) {
	b.Skip("PostgresDB requires database connection - skipping benchmark")
}

func BenchmarkPostgres_ConcurrentQueries(b *testing.B) {
	b.Skip("PostgresDB requires database connection - skipping benchmark")
}
