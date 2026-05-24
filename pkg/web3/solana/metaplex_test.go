package solana

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetadataAccount_Deserialize_Minimal(t *testing.T) {
	data := buildMinimalMetadataData("TestNFT", "TNFT", "https://example.com/metadata.json", 500)
	var meta MetadataAccount
	err := meta.Deserialize(data)
	assert.NoError(t, err)
	assert.Equal(t, uint8(4), meta.Key)
	assert.Equal(t, "TestNFT", meta.Data.Name)
	assert.Equal(t, "TNFT", meta.Data.Symbol)
	assert.Equal(t, "https://example.com/metadata.json", meta.Data.URI)
	assert.Equal(t, uint16(500), meta.Data.SellerFeeBasisPoints)
	assert.False(t, meta.PrimarySaleHappened)
	assert.False(t, meta.IsMutable)
}

func TestMetadataAccount_Deserialize_WithCreators(t *testing.T) {
	data := buildMetadataDataWithCreators("TestNFT", "TNFT", "https://example.com", 100,
		[]creatorData{
			{verified: true, share: 50},
		},
		true, true,
	)

	var meta MetadataAccount
	err := meta.Deserialize(data)
	assert.NoError(t, err)
	assert.Len(t, meta.Data.Creators, 1)
	assert.True(t, meta.Data.Creators[0].Verified)
	assert.Equal(t, uint8(50), meta.Data.Creators[0].Share)
	assert.True(t, meta.PrimarySaleHappened)
	assert.True(t, meta.IsMutable)
}

func TestMetadataAccount_Deserialize_TooShort(t *testing.T) {
	var meta MetadataAccount
	err := meta.Deserialize([]byte{0x01, 0x02})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestMetadataAccount_Deserialize_TooManyCreators(t *testing.T) {
	data := buildMetadataPrefix("N", "S", "U", 0)
	hasCreators := []byte{1}
	creatorCount := []byte{101, 0, 0, 0}

	fullData := append(data, hasCreators...)
	fullData = append(fullData, creatorCount...)

	var meta MetadataAccount
	err := meta.Deserialize(fullData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too many creators")
}

func TestMetadataAccount_Deserialize_WithEditionNonce(t *testing.T) {
	data := buildMetadataPrefix("N", "S", "U", 0)
	hasCreators := []byte{0}
	primarySale := []byte{0}
	isMutable := []byte{0}
	hasEdition := []byte{1}
	editionNonce := []byte{42}

	fullData := append(data, hasCreators...)
	fullData = append(fullData, primarySale...)
	fullData = append(fullData, isMutable...)
	fullData = append(fullData, hasEdition...)
	fullData = append(fullData, editionNonce...)

	var meta MetadataAccount
	err := meta.Deserialize(fullData)
	assert.NoError(t, err)
	assert.Equal(t, uint8(42), meta.EditionNonce)
}

func TestMetaplexVerifier_CompareMetadata(t *testing.T) {
	mv := &MetaplexVerifier{}

	actual := &MetaplexMetadata{
		Name:        "TestNFT",
		Symbol:      "TNFT",
		Description: "A test NFT",
		SellerFee:   500,
		Image:       "https://example.com/img.png",
		Attributes: []MetadataAttribute{
			{TraitType: "Color", Value: "Blue"},
		},
	}

	t.Run("identical", func(t *testing.T) {
		expected := &MetaplexMetadata{
			Name:        "TestNFT",
			Symbol:      "TNFT",
			Description: "A test NFT",
			SellerFee:   500,
			Image:       "https://example.com/img.png",
			Attributes: []MetadataAttribute{
				{TraitType: "Color", Value: "Blue"},
			},
		}
		assert.True(t, mv.compareMetadata(actual, expected))
	})

	t.Run("name_mismatch", func(t *testing.T) {
		expected := &MetaplexMetadata{Name: "OtherNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png"}
		assert.False(t, mv.compareMetadata(actual, expected))
	})

	t.Run("symbol_mismatch", func(t *testing.T) {
		expected := &MetaplexMetadata{Name: "TestNFT", Symbol: "OTHER", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png"}
		assert.False(t, mv.compareMetadata(actual, expected))
	})

	t.Run("description_mismatch", func(t *testing.T) {
		expected := &MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "Different", SellerFee: 500, Image: "https://example.com/img.png"}
		assert.False(t, mv.compareMetadata(actual, expected))
	})

	t.Run("seller_fee_mismatch", func(t *testing.T) {
		expected := &MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 1000, Image: "https://example.com/img.png"}
		assert.False(t, mv.compareMetadata(actual, expected))
	})

	t.Run("image_mismatch", func(t *testing.T) {
		expected := &MetaplexMetadata{Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://other.com/img.png"}
		assert.False(t, mv.compareMetadata(actual, expected))
	})

	t.Run("attribute_count_mismatch", func(t *testing.T) {
		expected := &MetaplexMetadata{
			Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png",
			Attributes: []MetadataAttribute{
				{TraitType: "Color", Value: "Blue"},
				{TraitType: "Size", Value: "Large"},
			},
		}
		assert.False(t, mv.compareMetadata(actual, expected))
	})

	t.Run("attribute_trait_mismatch", func(t *testing.T) {
		expected := &MetaplexMetadata{
			Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png",
			Attributes: []MetadataAttribute{
				{TraitType: "Size", Value: "Blue"},
			},
		}
		assert.False(t, mv.compareMetadata(actual, expected))
	})

	t.Run("attribute_value_mismatch", func(t *testing.T) {
		expected := &MetaplexMetadata{
			Name: "TestNFT", Symbol: "TNFT", Description: "A test NFT", SellerFee: 500, Image: "https://example.com/img.png",
			Attributes: []MetadataAttribute{
				{TraitType: "Color", Value: "Red"},
			},
		}
		assert.False(t, mv.compareMetadata(actual, expected))
	})
}

func buildMetadataPrefix(name, symbol, uri string, sellerFee uint16) []byte {
	key := []byte{4}
	updateAuthority := make([]byte, 32)
	mint := make([]byte, 32)

	nameBytes := []byte(name)
	symbolBytes := []byte(symbol)
	uriBytes := []byte(uri)

	var data []byte
	data = append(data, key...)
	data = append(data, updateAuthority...)
	data = append(data, mint...)
	data = append(data, encodeBorshUint32(uint32(len(nameBytes)))...)
	data = append(data, nameBytes...)
	data = append(data, encodeBorshUint32(uint32(len(symbolBytes)))...)
	data = append(data, symbolBytes...)
	data = append(data, encodeBorshUint32(uint32(len(uriBytes)))...)
	data = append(data, uriBytes...)
	feeBytes := make([]byte, 2)
	feeBytes[0] = byte(sellerFee)
	feeBytes[1] = byte(sellerFee >> 8)
	data = append(data, feeBytes...)
	return data
}

type creatorData struct {
	verified bool
	share    uint8
}

func buildMetadataDataWithCreators(name, symbol, uri string, sellerFee uint16, creators []creatorData, primarySale, isMutable bool) []byte {
	data := buildMetadataPrefix(name, symbol, uri, sellerFee)

	if len(creators) > 0 {
		data = append(data, byte(1))
		data = append(data, encodeBorshUint32(uint32(len(creators)))...)
		for _, c := range creators {
			addr := make([]byte, 32)
			data = append(data, addr...)
			if c.verified {
				data = append(data, byte(1))
			} else {
				data = append(data, byte(0))
			}
			data = append(data, c.share)
		}
	} else {
		data = append(data, byte(0))
	}

	if primarySale {
		data = append(data, byte(1))
	} else {
		data = append(data, byte(0))
	}
	if isMutable {
		data = append(data, byte(1))
	} else {
		data = append(data, byte(0))
	}

	return data
}

func buildMinimalMetadataData(name, symbol, uri string, sellerFee uint16) []byte {
	return buildMetadataDataWithCreators(name, symbol, uri, sellerFee, nil, false, false)
}

func encodeBorshUint32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}
