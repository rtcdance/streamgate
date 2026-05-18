package models

import "time"

type GatingRule struct {
	ID              string    `json:"id"`
	ContentID       string    `json:"content_id"`
	ContractAddress string    `json:"contract_address"`
	TokenID         string    `json:"token_id"`
	ChainID         int64     `json:"chain_id"`
	Standard        string    `json:"standard"`
	MinBalance      int       `json:"min_balance"`
	IsActive        bool      `json:"is_active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type GatingRuleStandard string

const (
	GatingStandardERC721  GatingRuleStandard = "erc721"
	GatingStandardERC1155 GatingRuleStandard = "erc1155"
	GatingStandardAny     GatingRuleStandard = "any"
)
