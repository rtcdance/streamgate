// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {ERC721} from "openzeppelin-contracts/contracts/token/ERC721/ERC721.sol";
import {ERC721URIStorage} from "openzeppelin-contracts/contracts/token/ERC721/extensions/ERC721URIStorage.sol";
import {ERC721Burnable} from "openzeppelin-contracts/contracts/token/ERC721/extensions/ERC721Burnable.sol";
import {Ownable} from "openzeppelin-contracts/contracts/access/Ownable.sol";

contract StreamNFT is ERC721, ERC721URIStorage, ERC721Burnable, Ownable {
    uint256 private _nextTokenId;

    struct StreamMetadata {
        string contentId;
        string streamURL;
        uint256 duration;
        uint256 qualityBitrate;
        bool isPremium;
    }

    mapping(uint256 => StreamMetadata) private _streamMetadata;
    mapping(string => uint256) private _contentIdToToken;

    event StreamNFTMinted(
        uint256 indexed tokenId,
        address indexed to,
        string contentId,
        string streamURL,
        bool isPremium
    );

    event StreamAccessGranted(
        uint256 indexed tokenId,
        address indexed grantee,
        uint256 expiresAt
    );

    error ContentAlreadyMinted(string contentId);
    error ContentNotFound(string contentId);
    error InvalidStreamURL(string url);

    constructor() ERC721("StreamGate NFT", "STREAM") Ownable(msg.sender) {}

    function mintStreamNFT(
        address to,
        string calldata contentId,
        string calldata uri_,
        string calldata streamURL,
        uint256 duration,
        uint256 qualityBitrate,
        bool isPremium
    ) external onlyOwner returns (uint256) {
        if (_contentIdToToken[contentId] != 0) {
            revert ContentAlreadyMinted(contentId);
        }
        if (bytes(streamURL).length == 0) {
            revert InvalidStreamURL(streamURL);
        }

        uint256 tokenId = ++_nextTokenId;
        _safeMint(to, tokenId);
        _setTokenURI(tokenId, uri_);

        _streamMetadata[tokenId] = StreamMetadata({
            contentId: contentId,
            streamURL: streamURL,
            duration: duration,
            qualityBitrate: qualityBitrate,
            isPremium: isPremium
        });

        _contentIdToToken[contentId] = tokenId;

        emit StreamNFTMinted(tokenId, to, contentId, streamURL, isPremium);
        return tokenId;
    }

    function getStreamMetadata(uint256 tokenId) external view returns (StreamMetadata memory) {
        _requireOwned(tokenId);
        return _streamMetadata[tokenId];
    }

    function getTokenByContentId(string calldata contentId) external view returns (uint256) {
        uint256 tokenId = _contentIdToToken[contentId];
        if (tokenId == 0) {
            revert ContentNotFound(contentId);
        }
        return tokenId;
    }

    function isPremiumContent(uint256 tokenId) external view returns (bool) {
        _requireOwned(tokenId);
        return _streamMetadata[tokenId].isPremium;
    }

    function getStreamURL(uint256 tokenId) external view returns (string memory) {
        _requireOwned(tokenId);
        return _streamMetadata[tokenId].streamURL;
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