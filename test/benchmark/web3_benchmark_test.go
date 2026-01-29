package benchmark_test

import (
	"context"
	"testing"

	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func BenchmarkWeb3_VerifyNFT(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	web3Service := service.NewWeb3Service(db)

	contractAddr := "0x1234567890123456789012345678901234567890"
	tokenID := "1"
	ownerAddr := "0x0987654321098765432109876543210987654321"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		web3Service.VerifyNFT(context.Background(), contractAddr, tokenID, ownerAddr)
	}
}

func BenchmarkWeb3_VerifySignature(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	web3Service := service.NewWeb3Service(db)

	message := "test message"
	signature := "0x1234567890abcdef"
	address := "0x0987654321098765432109876543210987654321"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		web3Service.VerifySignature(context.Background(), message, signature, address)
	}
}

func BenchmarkWeb3_GetBalance(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	web3Service := service.NewWeb3Service(db)

	address := "0x0987654321098765432109876543210987654321"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		web3Service.GetBalance(context.Background(), address)
	}
}

func BenchmarkWeb3_CallContractMethod(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	web3Service := service.NewWeb3Service(db)

	contractAddr := "0x1234567890123456789012345678901234567890"
	method := "balanceOf"
	params := []interface{}{"0x0987654321098765432109876543210987654321"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		web3Service.CallContractMethod(context.Background(), contractAddr, method, params)
	}
}

func BenchmarkWeb3_IsChainSupported(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	web3Service := service.NewWeb3Service(db)

	chainID := 1

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		web3Service.IsChainSupported(context.Background(), chainID)
	}
}

func BenchmarkWeb3_ConcurrentVerifications(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	web3Service := service.NewWeb3Service(db)

	contractAddr := "0x1234567890123456789012345678901234567890"
	tokenID := "1"
	ownerAddr := "0x0987654321098765432109876543210987654321"

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			web3Service.VerifyNFT(context.Background(), contractAddr, tokenID, ownerAddr)
		}
	})
}

func BenchmarkWeb3_MultiChainOperations(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	web3Service := service.NewWeb3Service(db)

	chains := []int{1, 137, 56}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, chainID := range chains {
			web3Service.IsChainSupported(context.Background(), chainID)
		}
	}
}
