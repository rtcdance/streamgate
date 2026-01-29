package v1

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// NFTHandler handles NFT requests
type NFTHandler struct{}

// List lists all NFTs
func (h *NFTHandler) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"nfts": []interface{}{}})
}

// Get gets a specific NFT
func (h *NFTHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"nft": nil})
}

// Verify verifies NFT ownership
func (h *NFTHandler) Verify(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"verified": false})
}
