// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {IERC1271} from "openzeppelin-contracts/contracts/interfaces/IERC1271.sol";
import {IERC1155Receiver} from "openzeppelin-contracts/contracts/token/ERC1155/IERC1155Receiver.sol";
import {MessageHashUtils} from "openzeppelin-contracts/contracts/utils/cryptography/MessageHashUtils.sol";
import {Study1271Wallet} from "../src/Study1271Wallet.sol";
import {DemoNFT} from "../src/DemoNFT.sol";
import {DemoERC1155} from "../src/DemoERC1155.sol";

/// @dev Adversarial RPC fixtures, never wallets used for the public user path.
contract Study1271ResponseFixture {
    enum Mode {
        Variable,
        WrongMagic,
        RevertCall,
        Empty,
        Short,
        DirtyPadding
    }
    Mode public mode;
    bytes32 public immutable expectedDigest;
    bytes32 public immutable expectedSignatureHash;

    constructor(bytes32 digest, bytes memory signature) {
        expectedDigest = digest;
        expectedSignatureHash = keccak256(signature);
    }

    function setMode(Mode next) external {
        mode = next;
    }

    function isValidSignature(bytes32 digest, bytes calldata signature) external view returns (bytes4) {
        if (mode == Mode.RevertCall) revert("fixture policy unavailable");
        // Deliberately malformed ABI responses exercise strict backend decoding.
        if (mode == Mode.Empty) {
            assembly ("memory-safe") { return(0, 0) }
        }
        if (mode == Mode.Short) {
            assembly ("memory-safe") {
                mstore(0, shl(224, 0x1626ba7e))
                return(0, 4)
            }
        }
        if (mode == Mode.DirtyPadding) {
            assembly ("memory-safe") {
                mstore(0, or(shl(224, 0x1626ba7e), 1))
                return(0, 32)
            }
        }
        if (mode == Mode.WrongMagic) return 0xdeadbeef;
        return digest == expectedDigest && keccak256(signature) == expectedSignatureHash
            ? IERC1271.isValidSignature.selector
            : bytes4(0xffffffff);
    }
}

contract Study1271ExecutionTarget {
    error TargetRejected();

    function reject() external pure {
        revert TargetRejected();
    }

    function echo(bytes32 value) external pure returns (bytes32) {
        return value;
    }
}

contract Study1271WalletTest is Test {
    uint256 private constant OWNER_KEY = 0xa11ce;
    uint256 private constant NEXT_KEY = 0xb0b;
    uint256 private constant CURVE_ORDER = 0xfffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141;
    bytes4 private constant MAGIC = IERC1271.isValidSignature.selector;
    address private owner;
    address private nextOwner;
    Study1271Wallet private wallet;
    DemoNFT private nft;
    DemoERC1155 private multi;
    bytes32 private digest;

    function setUp() public {
        owner = vm.addr(OWNER_KEY);
        nextOwner = vm.addr(NEXT_KEY);
        wallet = new Study1271Wallet(owner);
        nft = new DemoNFT();
        multi = new DemoERC1155();
        digest = MessageHashUtils.toEthSignedMessageHash(bytes("SIWE contract subject fixture"));
    }

    function _signature(uint256 key, bytes32 hash) private pure returns (bytes memory) {
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(key, hash);
        return abi.encodePacked(r, s, v);
    }

    function testCanonicalSignature() public {
        assertEq(wallet.isValidSignature(digest, _signature(OWNER_KEY, digest)), MAGIC);
    }

    function testCompactSignature() public view {
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(OWNER_KEY, digest);
        bytes memory compact = abi.encodePacked(r, bytes32(uint256(s) | (uint256(v - 27) << 255)));
        assertEq(compact.length, 64);
        assertEq(wallet.isValidSignature(digest, compact), MAGIC);
    }

    function testWrongSignerAndDigestRejected() public {
        assertEq(wallet.isValidSignature(digest, _signature(NEXT_KEY, digest)), bytes4(0xffffffff));
        assertEq(wallet.isValidSignature(bytes32(uint256(1)), _signature(OWNER_KEY, digest)), bytes4(0xffffffff));
    }

    function testMalformedSignatureRejected() public view {
        assertEq(wallet.isValidSignature(digest, ""), bytes4(0xffffffff));
        assertEq(wallet.isValidSignature(digest, hex"1234"), bytes4(0xffffffff));
        assertEq(wallet.isValidSignature(digest, new bytes(64)), bytes4(0xffffffff));
        assertEq(wallet.isValidSignature(digest, new bytes(65)), bytes4(0xffffffff));
        assertEq(wallet.isValidSignature(digest, new bytes(8192)), bytes4(0xffffffff));
    }

    function testHighSAndInvalidVRejected() public view {
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(OWNER_KEY, digest);
        assertEq(
            wallet.isValidSignature(
                digest, abi.encodePacked(r, bytes32(CURVE_ORDER - uint256(s)), uint8(v == 27 ? 28 : 27))
            ),
            bytes4(0xffffffff)
        );
        assertEq(wallet.isValidSignature(digest, abi.encodePacked(r, s, uint8(0))), bytes4(0xffffffff));
    }

    function testRotationInvalidatesProofWithoutChangingNFT() public {
        uint256 token = nft.mint(address(wallet));
        bytes memory oldProof = _signature(OWNER_KEY, digest);
        assertEq(wallet.isValidSignature(digest, oldProof), MAGIC);
        vm.prank(owner);
        wallet.setOwner(nextOwner);
        assertEq(wallet.owner(), nextOwner);
        assertEq(wallet.isValidSignature(digest, oldProof), bytes4(0xffffffff));
        assertEq(wallet.isValidSignature(digest, _signature(NEXT_KEY, digest)), MAGIC);
        assertEq(nft.ownerOf(token), address(wallet));
        vm.prank(owner);
        vm.expectRevert(Study1271Wallet.Unauthorized.selector);
        wallet.execute(address(nft), 0, abi.encodeCall(nft.transferFrom, (address(wallet), owner, token)));
    }

    function testInvalidOwnerRejected() public {
        vm.expectRevert(Study1271Wallet.InvalidOwner.selector);
        new Study1271Wallet(address(0));
        vm.expectRevert(Study1271Wallet.InvalidOwner.selector);
        new Study1271Wallet(address(nft));
        vm.startPrank(owner);
        vm.expectRevert(Study1271Wallet.InvalidOwner.selector);
        wallet.setOwner(address(0));
        vm.expectRevert(Study1271Wallet.InvalidOwner.selector);
        wallet.setOwner(address(wallet));
        vm.expectRevert(Study1271Wallet.InvalidOwner.selector);
        wallet.setOwner(address(nft));
        vm.stopPrank();
        assertEq(wallet.owner(), owner);
    }

    function testERC721ReceivedAndExecutedByWallet() public {
        uint256 token = nft.mint(address(wallet));
        assertEq(nft.ownerOf(token), address(wallet));
        assertEq(nft.balanceOf(owner), 0);
        vm.prank(owner);
        wallet.execute(address(nft), 0, abi.encodeCall(nft.transferFrom, (address(wallet), nextOwner, token)));
        assertEq(nft.ownerOf(token), nextOwner);
        assertEq(nft.balanceOf(address(wallet)), 0);
    }

    function testERC721SafeTransferReceived() public {
        uint256 token = nft.mint(owner);
        vm.prank(owner);
        nft.safeTransferFrom(owner, address(wallet), token);
        assertEq(nft.ownerOf(token), address(wallet));
    }

    function testERC1155ReceivedAndExecutedByWallet() public {
        multi.mint(address(wallet), 7, 3);
        assertEq(multi.balanceOf(address(wallet), 7), 3);
        vm.prank(owner);
        wallet.execute(
            address(multi), 0, abi.encodeCall(multi.safeTransferFrom, (address(wallet), nextOwner, 7, 2, ""))
        );
        assertEq(multi.balanceOf(address(wallet), 7), 1);
        assertEq(multi.balanceOf(nextOwner, 7), 2);
    }

    function testERC1155BatchReceivedAndExecuted() public {
        uint256[] memory ids = new uint256[](2);
        uint256[] memory amounts = new uint256[](2);
        ids[0] = 4;
        ids[1] = 9;
        amounts[0] = 2;
        amounts[1] = 3;
        multi.mint(owner, ids[0], amounts[0]);
        multi.mint(owner, ids[1], amounts[1]);
        vm.prank(owner);
        multi.safeBatchTransferFrom(owner, address(wallet), ids, amounts, "");
        assertEq(multi.balanceOf(address(wallet), 9), 3);
        vm.prank(owner);
        wallet.execute(
            address(multi),
            0,
            abi.encodeCall(multi.safeBatchTransferFrom, (address(wallet), nextOwner, ids, amounts, ""))
        );
        assertEq(multi.balanceOf(address(wallet), 4), 0);
        assertEq(multi.balanceOf(address(wallet), 9), 0);
        assertEq(multi.balanceOf(nextOwner, 4), 2);
        assertEq(multi.balanceOf(nextOwner, 9), 3);
    }

    function testReceiverInterfaces() public view {
        assertTrue(wallet.supportsInterface(type(IERC1155Receiver).interfaceId));
        assertTrue(wallet.supportsInterface(0x01ffc9a7));
        assertFalse(wallet.supportsInterface(0xffffffff));
    }

    function testReceiveAndSendNativeValue() public {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        (bool funded,) = address(wallet).call{value: 1 ether}("");
        assertTrue(funded);
        uint256 previous = nextOwner.balance;
        vm.prank(owner);
        wallet.execute(nextOwner, 0.25 ether, "");
        assertEq(nextOwner.balance, previous + 0.25 ether);
        assertEq(address(wallet).balance, 0.75 ether);
    }

    function testExecutionReturnsData() public {
        Study1271ExecutionTarget target = new Study1271ExecutionTarget();
        vm.prank(owner);
        bytes memory output = wallet.execute(address(target), 0, abi.encodeCall(target.echo, (digest)));
        assertEq(abi.decode(output, (bytes32)), digest);
    }

    function testTargetFailurePreserved() public {
        Study1271ExecutionTarget target = new Study1271ExecutionTarget();
        vm.prank(owner);
        vm.expectRevert(
            abi.encodeWithSelector(
                Study1271Wallet.ExecutionFailed.selector,
                abi.encodeWithSelector(Study1271ExecutionTarget.TargetRejected.selector)
            )
        );
        wallet.execute(address(target), 0, abi.encodeCall(target.reject, ()));
    }

    function testInvalidTargetAndInsufficientFundsRejected() public {
        vm.startPrank(owner);
        vm.expectRevert(Study1271Wallet.InvalidTarget.selector);
        wallet.execute(address(0), 0, "");
        vm.expectRevert(abi.encodeWithSelector(Study1271Wallet.ExecutionFailed.selector, bytes("")));
        wallet.execute(nextOwner, 1, "");
        vm.stopPrank();
    }

    function testPublicProofCannotExecute() public {
        bytes memory proof = _signature(OWNER_KEY, digest);
        assertEq(wallet.isValidSignature(digest, proof), MAGIC);
        vm.prank(nextOwner);
        vm.expectRevert(Study1271Wallet.Unauthorized.selector);
        wallet.execute(address(nft), 0, proof);
    }

    function testFuzzUnauthorizedCannotExecuteOrRotate(address caller) public {
        vm.assume(caller != owner);
        vm.startPrank(caller);
        vm.expectRevert(Study1271Wallet.Unauthorized.selector);
        wallet.execute(nextOwner, 0, "");
        vm.expectRevert(Study1271Wallet.Unauthorized.selector);
        wallet.setOwner(nextOwner);
        vm.stopPrank();
    }

    function testFuzzSignedDigest(bytes32 hash) public {
        assertEq(wallet.isValidSignature(hash, _signature(OWNER_KEY, hash)), MAGIC);
        assertEq(wallet.isValidSignature(hash, _signature(NEXT_KEY, hash)), bytes4(0xffffffff));
    }

    function testVariableLengthAndAdversarialResponses() public {
        bytes memory proof = hex"01020304050607";
        Study1271ResponseFixture fixture = new Study1271ResponseFixture(digest, proof);
        assertEq(fixture.isValidSignature(digest, proof), MAGIC);
        assertEq(fixture.isValidSignature(digest, hex"01"), bytes4(0xffffffff));
        fixture.setMode(Study1271ResponseFixture.Mode.WrongMagic);
        assertEq(fixture.isValidSignature(digest, proof), bytes4(0xdeadbeef));
        fixture.setMode(Study1271ResponseFixture.Mode.RevertCall);
        vm.expectRevert(bytes("fixture policy unavailable"));
        fixture.isValidSignature(digest, proof);
        for (
            uint8 i = uint8(Study1271ResponseFixture.Mode.Empty);
            i <= uint8(Study1271ResponseFixture.Mode.DirtyPadding);
            i++
        ) {
            fixture.setMode(Study1271ResponseFixture.Mode(i));
            (bool ok, bytes memory output) =
                address(fixture).staticcall(abi.encodeCall(fixture.isValidSignature, (digest, proof)));
            assertTrue(ok);
            if (i == uint8(Study1271ResponseFixture.Mode.Empty)) assertEq(output.length, 0);
            if (i == uint8(Study1271ResponseFixture.Mode.Short)) assertEq(output.length, 4);
            if (i == uint8(Study1271ResponseFixture.Mode.DirtyPadding)) {
                assertEq(output.length, 32);
                assertEq(uint8(output[31]), 1);
            }
        }
    }
}
