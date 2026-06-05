// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

// Minimal ERC-721 with mint and ERC-165 support
contract DemoNFT {
    string public name = "StreamGate Demo";
    string public symbol = "SDEM";

    mapping(uint256 => address) private _owners;
    mapping(address => uint256) private _balances;
    mapping(uint256 => string) private _tokenURIs;

    uint256 private _nextTokenId = 1;

    event Transfer(address indexed from, address indexed to, uint256 indexed tokenId);
    event Approval(address indexed owner, address indexed approved, uint256 indexed tokenId);
    event ApprovalForAll(address indexed owner, address indexed operator, bool approved);

    function mint(address to) external returns (uint256) {
        uint256 tokenId = _nextTokenId++;
        _owners[tokenId] = to;
        _balances[to]++;
        emit Transfer(address(0), to, tokenId);
        return tokenId;
    }

    function balanceOf(address owner) external view returns (uint256) {
        require(owner != address(0), "ERC721: balance query for zero address");
        return _balances[owner];
    }

    function ownerOf(uint256 tokenId) public view returns (address) {
        address owner = _owners[tokenId];
        require(owner != address(0), "ERC721: owner query for nonexistent token");
        return owner;
    }

    function tokenURI(uint256 tokenId) external view returns (string memory) {
        require(_owners[tokenId] != address(0), "ERC721: URI query for nonexistent token");
        return _tokenURIs[tokenId];
    }

    function setTokenURI(uint256 tokenId, string memory uri) external {
        require(_owners[tokenId] != address(0), "ERC721: URI set for nonexistent token");
        _tokenURIs[tokenId] = uri;
    }

    function totalSupply() external view returns (uint256) {
        return _nextTokenId - 1;
    }

    function supportsInterface(bytes4 interfaceId) external pure returns (bool) {
        return
            interfaceId == 0x01ffc9a7 || // ERC165
            interfaceId == 0x80ac58cd || // ERC721
            interfaceId == 0x5b5e139f || // ERC721Metadata
            interfaceId == 0x780e9d63;   // ERC721Enumerable (partial)
    }

    function approve(address, uint256) external pure {
        revert("not implemented");
    }

    function getApproved(uint256) external pure returns (address) {
        return address(0);
    }

    function setApprovalForAll(address, bool) external pure {
        revert("not implemented");
    }

    function isApprovedForAll(address, address) external pure returns (bool) {
        return false;
    }

    function transferFrom(address from, address to, uint256 tokenId) external {
        require(ownerOf(tokenId) == from, "ERC721: transfer from incorrect owner");
        require(to != address(0), "ERC721: transfer to zero address");
        _balances[from]--;
        _balances[to]++;
        _owners[tokenId] = to;
        emit Transfer(from, to, tokenId);
    }
}
