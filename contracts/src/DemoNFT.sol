// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {ERC721} from "openzeppelin-contracts/contracts/token/ERC721/ERC721.sol";
import {ERC721URIStorage} from "openzeppelin-contracts/contracts/token/ERC721/extensions/ERC721URIStorage.sol";

/// StreamGate demo ERC-721.
///
/// DEMO ONLY. `mint` is permissionless by design — the backend dev-mint
/// endpoint relies on it, and production deployments must not expose a mint
/// path. The contract is built on OpenZeppelin's ERC721 + ERC721URIStorage so
/// that the standard is implemented for real: both `safeTransferFrom`
/// overloads perform the ERC-721 receiver check, approvals are cleared on
/// transfer, and balances are conserved.
///
/// ERC721Enumerable is deliberately NOT inherited, so the contract never
/// advertises 0x780e9d63 (tokenByIndex / tokenOfOwnerByIndex do not exist).
contract DemoNFT is ERC721, ERC721URIStorage {
    uint256 private _nextTokenId = 1;

    constructor() ERC721("StreamGate Demo", "SDEM") {}

    /// Permissionless demo mint. DEMO ONLY — see the contract doc comment.
    function mint(address to) external returns (uint256) {
        uint256 tokenId = _nextTokenId++;
        _safeMint(to, tokenId);
        return tokenId;
    }

    function setTokenURI(uint256 tokenId, string memory uri) external {
        require(_ownerOf(tokenId) == msg.sender, "DemoNFT: caller is not the token owner");
        _setTokenURI(tokenId, uri);
    }

    function totalSupply() external view returns (uint256) {
        return _nextTokenId - 1;
    }

    function tokenURI(uint256 tokenId)
        public
        view
        override(ERC721, ERC721URIStorage)
        returns (string memory)
    {
        return super.tokenURI(tokenId);
    }

    function supportsInterface(bytes4 interfaceId)
        public
        view
        override(ERC721, ERC721URIStorage)
        returns (bool)
    {
        return super.supportsInterface(interfaceId);
    }
}
