// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {DemoNFT} from "../src/DemoNFT.sol";
import {IERC721Receiver} from "openzeppelin-contracts/contracts/token/ERC721/IERC721Receiver.sol";
import {IERC721Errors} from "openzeppelin-contracts/contracts/interfaces/draft-IERC6093.sol";

contract AcceptingReceiver is IERC721Receiver {
    uint256 public calls;
    address public lastOperator;
    uint256 public lastTokenId;

    function onERC721Received(address operator, address, uint256 tokenId, bytes calldata)
        external
        returns (bytes4)
    {
        calls++;
        lastOperator = operator;
        lastTokenId = tokenId;
        return IERC721Receiver.onERC721Received.selector;
    }
}

contract WrongReturnReceiver {
    function onERC721Received(address, address, uint256, bytes calldata) external pure returns (bytes4) {
        return 0xdeadbeef;
    }
}

contract RevertingReceiver {
    function onERC721Received(address, address, uint256, bytes calldata) external pure returns (bytes4) {
        revert("receiver says no");
    }
}

/// DemoNFT standard-conformance suite.
///
/// Covers what the old hand-rolled contract could not: both `safeTransferFrom`
/// overloads really perform the ERC-721 receiver check (accept / wrong return /
/// revert), transfers clear approvals, and balances are conserved.
contract DemoNFTTest is Test {
    DemoNFT public nft;
    address public owner = makeAddr("owner");
    address public other = makeAddr("other");

    event Transfer(address indexed from, address indexed to, uint256 indexed tokenId);

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
        // 0x780e9d63 = ERC721Enumerable: tokenByIndex/tokenOfOwnerByIndex do not
        // exist, so advertising it would lie to ERC-165 probes.
        assertFalse(nft.supportsInterface(0x780e9d63));
    }

    // --- ERC-721 core ---

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
        vm.expectRevert(abi.encodeWithSelector(IERC721Errors.ERC721InsufficientApproval.selector, other, tokenId));
        nft.transferFrom(owner, other, tokenId);
    }

    function testTransferClearsApproval() public {
        uint256 tokenId = _mintTo(owner);
        vm.prank(owner);
        nft.approve(other, tokenId);
        assertEq(nft.getApproved(tokenId), other);

        vm.prank(owner);
        nft.transferFrom(owner, other, tokenId);
        assertEq(nft.getApproved(tokenId), address(0), "approval must be cleared on transfer");
    }

    // --- safeTransferFrom overloads + receiver checks ---

    function testSafeTransferFrom_ToEOA() public {
        uint256 tokenId = _mintTo(owner);
        vm.prank(owner);
        nft.safeTransferFrom(owner, other, tokenId);
        assertEq(nft.ownerOf(tokenId), other);
    }

    function testSafeTransferFrom_ToAcceptingReceiver() public {
        AcceptingReceiver receiver = new AcceptingReceiver();
        uint256 tokenId = _mintTo(owner);

        vm.prank(owner);
        nft.safeTransferFrom(owner, address(receiver), tokenId);

        assertEq(nft.ownerOf(tokenId), address(receiver));
        assertEq(receiver.calls(), 1);
        assertEq(receiver.lastOperator(), owner);
        assertEq(receiver.lastTokenId(), tokenId);
    }

    function testSafeTransferFrom_WithData_ToAcceptingReceiver() public {
        AcceptingReceiver receiver = new AcceptingReceiver();
        uint256 tokenId = _mintTo(owner);

        vm.prank(owner);
        nft.safeTransferFrom(owner, address(receiver), tokenId, hex"c0ffee");

        assertEq(nft.ownerOf(tokenId), address(receiver));
        assertEq(receiver.calls(), 1);
    }

    function testSafeTransferFrom_ToWrongReturnReceiverReverts() public {
        WrongReturnReceiver receiver = new WrongReturnReceiver();
        uint256 tokenId = _mintTo(owner);

        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IERC721Errors.ERC721InvalidReceiver.selector, address(receiver)));
        nft.safeTransferFrom(owner, address(receiver), tokenId);
    }

    function testSafeTransferFrom_ToRevertingReceiverReverts() public {
        RevertingReceiver receiver = new RevertingReceiver();
        uint256 tokenId = _mintTo(owner);

        vm.prank(owner);
        vm.expectRevert(bytes("receiver says no"));
        nft.safeTransferFrom(owner, address(receiver), tokenId);
    }

    function testSafeTransferFrom_WithData_ToWrongReturnReceiverReverts() public {
        WrongReturnReceiver receiver = new WrongReturnReceiver();
        uint256 tokenId = _mintTo(owner);

        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IERC721Errors.ERC721InvalidReceiver.selector, address(receiver)));
        nft.safeTransferFrom(owner, address(receiver), tokenId, "");
    }

    function testSafeMint_ToNonReceiverReverts() public {
        WrongReturnReceiver receiver = new WrongReturnReceiver();
        vm.expectRevert(abi.encodeWithSelector(IERC721Errors.ERC721InvalidReceiver.selector, address(receiver)));
        nft.mint(address(receiver));
    }

    // --- fuzz ---

    /// Every mint must conserve supply: after N distinct mints the supply and
    /// the sum of all balances both equal N.
    function testFuzz_MintConservesSupply(uint8 count) public {
        count = uint8(bound(count, 1, 40));
        uint256 sum;
        for (uint256 i = 0; i < count; i++) {
            address holder = address(uint160(uint256(keccak256(abi.encode("holder", i)))));
            vm.prank(holder);
            nft.mint(holder);
            sum += nft.balanceOf(holder);
        }
        assertEq(nft.totalSupply(), count);
        assertEq(sum, count);
    }
}
