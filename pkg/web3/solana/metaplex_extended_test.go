package solana

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewMetaplexVerifier(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	require.NotNil(t, mv)
	assert.NotNil(t, mv.httpClient)
	assert.Equal(t, 5*time.Minute, mv.cacheTTL)
}

func TestMetaplexVerifier_Close(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	assert.NotPanics(t, func() {
		mv.Close()
	})
}

func TestMetaplexMetadata_Fields(t *testing.T) {
	meta := &MetaplexMetadata{
		Name:        "TestNFT",
		Symbol:      "TNFT",
		Description: "A test NFT",
		SellerFee:   500,
		Image:       "https://example.com/img.png",
		ExternalURL: "https://example.com",
		Attributes: []MetadataAttribute{
			{TraitType: "Color", Value: "Blue"},
		},
		Properties: MetadataProperties{
			Creators: []MetadataCreator{
				{Address: "addr1", Share: 50},
				{Address: "addr2", Share: 50},
			},
			Files: []MetadataFile{
				{URI: "https://example.com/img.png", Type: "image/png", CDN: true},
			},
		},
		Collection: &MetadataCollection{
			Name:   "TestCollection",
			Family: "TestFamily",
		},
		Uses: &MetadataUses{
			UseMethod: "burn",
			Remaining: 5,
			Total:     10,
		},
	}

	assert.Equal(t, "TestNFT", meta.Name)
	assert.Equal(t, "TNFT", meta.Symbol)
	assert.Equal(t, 500, meta.SellerFee)
	assert.Len(t, meta.Attributes, 1)
	assert.Len(t, meta.Properties.Creators, 2)
	assert.Len(t, meta.Properties.Files, 1)
	assert.NotNil(t, meta.Collection)
	assert.Equal(t, "TestCollection", meta.Collection.Name)
	assert.NotNil(t, meta.Uses)
	assert.Equal(t, uint64(5), meta.Uses.Remaining)
}

func TestMetaplexVerifier_CompareMetadata_TableDriven(t *testing.T) {
	mv := &MetaplexVerifier{}

	base := &MetaplexMetadata{
		Name:        "TestNFT",
		Symbol:      "TNFT",
		Description: "A test NFT",
		SellerFee:   500,
		Image:       "https://example.com/img.png",
		Attributes: []MetadataAttribute{
			{TraitType: "Color", Value: "Blue"},
		},
	}

	tests := []struct {
		name     string
		expected *MetaplexMetadata
		match    bool
	}{
		{
			"identical",
			&MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png", Attributes: []MetadataAttribute{{TraitType: "Color", Value: "Blue"}}},
			true,
		},
		{
			"name_mismatch",
			&MetaplexMetadata{Name: "Other", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png"},
			false,
		},
		{
			"symbol_mismatch",
			&MetaplexMetadata{Name: "TestNFT", Symbol: "OTHER", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png"},
			false,
		},
		{
			"description_mismatch",
			&MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "Different", SellerFee: 500, Image: "https://example.com/img.png"},
			false,
		},
		{
			"seller_fee_mismatch",
			&MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 1000, Image: "https://example.com/img.png"},
			false,
		},
		{
			"image_mismatch",
			&MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://other.com/img.png"},
			false,
		},
		{
			"attribute_count_mismatch",
			&MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png", Attributes: []MetadataAttribute{{TraitType: "Color", Value: "Blue"}, {TraitType: "Size", Value: "L"}}},
			false,
		},
		{
			"attribute_trait_mismatch",
			&MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png", Attributes: []MetadataAttribute{{TraitType: "Size", Value: "Blue"}}},
			false,
		},
		{
			"attribute_value_mismatch",
			&MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png", Attributes: []MetadataAttribute{{TraitType: "Color", Value: "Red"}}},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.match, mv.compareMetadata(base, tc.expected))
		})
	}
}

func TestMetadataAccount_Deserialize_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{"too short", []byte{0x01, 0x02}, true},
		{"minimal valid", buildMinimalMetadataData("N", "S", "U", 0), false},
		{"with creators", buildMetadataDataWithCreators("N", "S", "U", 100, []creatorData{{verified: true, share: 50}}, true, true), false},
		{"too many creators", func() []byte {
			data := buildMetadataPrefix("N", "S", "U", 0)
			data = append(data, byte(1))
			data = append(data, encodeBorshUint32(101)...)
			return data
		}(), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var meta MetadataAccount
			err := meta.Deserialize(tc.data)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMetadataAccount_Deserialize_WithEdition(t *testing.T) {
	data := buildMetadataPrefix("N", "S", "U", 0)
	data = append(data, byte(0))
	data = append(data, byte(0))
	data = append(data, byte(0))
	data = append(data, byte(1))
	data = append(data, byte(42))

	var meta MetadataAccount
	err := meta.Deserialize(data)
	require.NoError(t, err)
	assert.Equal(t, uint8(42), meta.EditionNonce)
}

func TestBorshReader_ReadBytes_PastEnd(t *testing.T) {
	r := &borshReader{data: []byte{0x01, 0x02}}
	result := r.readBytes(5)
	assert.Nil(t, result)
	assert.Error(t, r.err)
}

func TestBorshReader_ReadBorshString_TooLong(t *testing.T) {
	r := &borshReader{data: func() []byte {
		data := make([]byte, 4)
		data[0] = 0x01
		data[1] = 0x04
		return data
	}()}
	r.readBorshString()
	assert.Error(t, r.err)
}

func TestTokenInfo_Fields(t *testing.T) {
	info := &TokenInfo{
		ContractAddress: "metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s",
		TokenID:         "abc123",
		TokenType:       "Metaplex",
		URI:             "https://example.com/metadata.json",
		Metadata:        map[string]interface{}{"name": "Test"},
	}
	assert.Equal(t, "Metaplex", info.TokenType)
	assert.Equal(t, "Test", info.Metadata["name"])
}

func TestMetaplexVerifier_VerifyNFTOwnership_InvalidMint(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	_, err := mv.VerifyNFTOwnership(context.TODO(), "invalid-mint!", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_VerifyNFTOwnership_InvalidOwner(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	_, err := mv.VerifyNFTOwnership(context.TODO(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "invalid-owner!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid owner address")
}

func TestMetaplexVerifier_GetMetadata_InvalidMint(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	_, err := mv.GetMetadata(context.TODO(), "invalid!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_IsMetaplexNFT_InvalidMint(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	_, err := mv.IsMetaplexNFT(context.TODO(), "invalid!")
	require.Error(t, err)
}

func TestMetaplexVerifier_VerifyCreator_InvalidMint(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	_, err := mv.VerifyCreator(context.TODO(), "invalid!", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}

func TestMetaplexVerifier_VerifyCreator_InvalidCreator(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	_, err := mv.VerifyCreator(context.TODO(), "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU", "invalid!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid creator address")
}

func TestMetaplexVerifier_VerifyCollection_InvalidMint(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	_, err := mv.VerifyCollection(context.TODO(), "invalid!", "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU")
	require.Error(t, err)
}

func TestMetaplexVerifier_GetTokenInfo_InvalidMint(t *testing.T) {
	mv := NewMetaplexVerifier(nil, zap.NewNop(), nil)
	_, err := mv.GetTokenInfo(context.TODO(), "invalid!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint address")
}
