// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {DemoNFT} from "../src/DemoNFT.sol";

/// TRANSITION_PLAN B2 verification:
///   1. setTokenURI is owner-only (was: anyone could rewrite any URI).
///   2. supportsInterface only claims implemented interfaces — ERC-721 is
///      claimed AND actually implemented (approve/setApprovalForAll work);
///      ERC-721 Enumerable is NOT claimed (never implemented).
contract DemoNFTTest is Test {
    DemoNFT public nft;
    address public owner = makeAddr("owner");
    address public other = makeAddr("other");

    function setUp() public {
        nft = new DemoNFT();
    }

    function _mintTo(address to) internal returns (uint256 tokenId) {
        vm.prank(to);
        tokenId = nft.mint(to);
    }

    // --- setTokenURI access control ---

    function testSetTokenURI_ByOwner() public {
        uint256 tokenId = _mintTo(owner);
        vm.prank(owner);
        nft.setTokenURI(tokenId, "ipfs://owned");
        assertEq(nft.tokenURI(tokenId), "ipfs://owned");
    }

    function testSetTokenURI_ByNonOwnerReverts() public {
        uint256 tokenId = _mintTo(owner);
        vm.prank(other);
        vm.expectRevert(bytes("DemoNFT: caller is not the token owner"));
        nft.setTokenURI(tokenId, "ipfs://hijacked");
    }

    // --- honest ERC-165 ---

    function testSupportsInterface_ClaimedInterfaces() public view {
        assertTrue(nft.supportsInterface(0x01ffc9a7)); // ERC165
        assertTrue(nft.supportsInterface(0x80ac58cd)); // ERC721 — implemented for real
        assertTrue(nft.supportsInterface(0x5b5e139f)); // ERC721Metadata
    }

    function testSupportsInterface_EnumerableNotClaimed() public view {
        // 0x780e9d63 = ERC721Enumerable: tokenByIndex/tokenOfOwnerByIndex do
        // not exist, so advertising it would lie to ERC-165 probes.
        assertFalse(nft.supportsInterface(0x780e9d63));
    }

    // --- ERC-721 core is real (backs the 0x80ac58cd claim) ---

    function testApproveAndTransferByApprovedSpender() public {
        uint256 tokenId = _mintTo(owner);
        vm.prank(owner);
        nft.approve(other, tokenId);
        assertEq(nft.getApproved(tokenId), other);

        vm.prank(other);
        nft.transferFrom(owner, other, tokenId);
        assertEq(nft.ownerOf(tokenId), other);
    }

    function testSetApprovalForAll_TransferByOperator() public {
        uint256 tokenId = _mintTo(owner);
        vm.prank(owner);
        nft.setApprovalForAll(other, true);
        assertTrue(nft.isApprovedForAll(owner, other));

        vm.prank(other);
        nft.transferFrom(owner, other, tokenId);
        assertEq(nft.ownerOf(tokenId), other);
    }

    function testUnapprovedTransferReverts() public {
        uint256 tokenId = _mintTo(owner);
        vm.prank(other);
        vm.expectRevert(bytes("ERC721: caller is not token owner or approved"));
        nft.transferFrom(owner, other, tokenId);
    }
}
