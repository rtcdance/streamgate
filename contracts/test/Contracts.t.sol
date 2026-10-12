// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {ContentRegistry} from "../src/ContentRegistry.sol";

/// ContentRegistry state-machine and attestation suite.
///
/// The old contract allowed any status transition (a Removed record could be
/// revived, and IDs could be re-registered) and recorded the platform operator
/// as the creator. This suite pins the corrected behaviour.
contract ContentRegistryTest is Test {
    ContentRegistry public registry;
    address public operator = makeAddr("operator"); // platform attestor == contract owner
    address public creator = makeAddr("creator"); // verified content owner
    address public user = makeAddr("user");

    bytes32 public constant DIGEST = keccak256("original-media-bytes");
    string public constant URI = "ipfs://metadata/1";

    function setUp() public {
        vm.prank(operator);
        registry = new ContentRegistry();
    }

    function _register(string memory id) internal {
        vm.prank(operator);
        registry.registerContent(id, creator, DIGEST, URI);
    }

    // --- registration ---

    function testRegisterContent() public {
        _register("content-1");

        ContentRegistry.ContentRecord memory record = registry.getContent("content-1");
        assertEq(record.contentId, "content-1");
        assertEq(record.creator, creator, "creator must be the supplied owner, not the operator");
        assertEq(record.contentDigest, DIGEST);
        assertEq(record.metadataURI, URI);
        assertEq(uint256(record.status), uint256(ContentRegistry.ContentStatus.Active));
        assertEq(registry.getContentCount(), 1);
        assertTrue(registry.contentExists("content-1"));
    }

    function testRegisterContent_OnlyOperator() public {
        vm.prank(user);
        vm.expectRevert(abi.encodeWithSelector(bytes4(keccak256("OwnableUnauthorizedAccount(address)")), user));
        registry.registerContent("content-1", creator, DIGEST, URI);
    }

    function testRegisterContent_DuplicateReverts() public {
        _register("content-1");
        vm.prank(operator);
        vm.expectRevert(abi.encodeWithSelector(ContentRegistry.ContentAlreadyExists.selector, "content-1"));
        registry.registerContent("content-1", creator, keccak256("other"), URI);
    }

    function testRegisterContent_RejectsEmptyId() public {
        vm.prank(operator);
        vm.expectRevert(ContentRegistry.InvalidContentId.selector);
        registry.registerContent("", creator, DIGEST, URI);
    }

    function testRegisterContent_RejectsOverlongId() public {
        string memory longId = new string(registry.MAX_CONTENT_ID_BYTES() + 1);
        vm.prank(operator);
        vm.expectRevert(ContentRegistry.InvalidContentId.selector);
        registry.registerContent(longId, creator, DIGEST, URI);
    }

    function testRegisterContent_RejectsZeroCreator() public {
        vm.prank(operator);
        vm.expectRevert(ContentRegistry.InvalidCreator.selector);
        registry.registerContent("content-1", address(0), DIGEST, URI);
    }

    function testRegisterContent_RejectsZeroDigest() public {
        vm.prank(operator);
        vm.expectRevert(ContentRegistry.InvalidContentDigest.selector);
        registry.registerContent("content-1", creator, bytes32(0), URI);
    }

    function testRegisterContent_RejectsOverlongURI() public {
        string memory longURI = new string(registry.MAX_METADATA_URI_BYTES() + 1);
        vm.prank(operator);
        vm.expectRevert(ContentRegistry.MetadataURITooLong.selector);
        registry.registerContent("content-1", creator, DIGEST, longURI);
    }

    // --- state machine ---

    function testUpdateStatus_ActiveToFlaggedByOperator() public {
        _register("content-1");
        vm.prank(operator);
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Flagged);
        assertEq(
            uint256(registry.getContent("content-1").status),
            uint256(ContentRegistry.ContentStatus.Flagged)
        );
    }

    function testUpdateStatus_FlaggedToActiveByOperator() public {
        _register("content-1");
        vm.prank(operator);
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Flagged);
        vm.prank(operator);
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Active);
        assertEq(
            uint256(registry.getContent("content-1").status),
            uint256(ContentRegistry.ContentStatus.Active)
        );
    }

    function testUpdateStatus_NonOperatorReverts() public {
        _register("content-1");
        vm.prank(creator);
        vm.expectRevert(
            abi.encodeWithSelector(ContentRegistry.UnauthorizedCaller.selector, creator, creator)
        );
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Flagged);
    }

    function testUpdateStatus_ToRemovedViaUpdateReverts() public {
        _register("content-1");
        vm.prank(operator);
        vm.expectRevert(
            abi.encodeWithSelector(
                ContentRegistry.IllegalStateTransition.selector,
                ContentRegistry.ContentStatus.Active,
                ContentRegistry.ContentStatus.Removed
            )
        );
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Removed);
    }

    function testUpdateStatus_ToNonexistentReverts() public {
        _register("content-1");
        vm.prank(operator);
        vm.expectRevert(
            abi.encodeWithSelector(
                ContentRegistry.IllegalStateTransition.selector,
                ContentRegistry.ContentStatus.Active,
                ContentRegistry.ContentStatus.Nonexistent
            )
        );
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Nonexistent);
    }

    function testUpdateStatus_UnknownContentReverts() public {
        vm.prank(operator);
        vm.expectRevert(abi.encodeWithSelector(ContentRegistry.ContentNotFound.selector, "nope"));
        registry.updateContentStatus("nope", ContentRegistry.ContentStatus.Flagged);
    }

    // --- removal is terminal ---

    function testRemoveContent_ByCreator() public {
        _register("content-1");
        vm.prank(creator);
        registry.removeContent("content-1");
        assertEq(
            uint256(registry.getContent("content-1").status),
            uint256(ContentRegistry.ContentStatus.Removed)
        );
    }

    function testRemoveContent_ByOperator() public {
        _register("content-1");
        vm.prank(operator);
        registry.removeContent("content-1");
        assertEq(
            uint256(registry.getContent("content-1").status),
            uint256(ContentRegistry.ContentStatus.Removed)
        );
    }

    function testRemoveContent_ByStrangerReverts() public {
        _register("content-1");
        vm.prank(user);
        vm.expectRevert(abi.encodeWithSelector(ContentRegistry.UnauthorizedCaller.selector, user, creator));
        registry.removeContent("content-1");
    }

    function testRemoveContent_TwiceReverts() public {
        _register("content-1");
        vm.prank(creator);
        registry.removeContent("content-1");

        vm.prank(creator);
        vm.expectRevert(
            abi.encodeWithSelector(
                ContentRegistry.IllegalStateTransition.selector,
                ContentRegistry.ContentStatus.Removed,
                ContentRegistry.ContentStatus.Removed
            )
        );
        registry.removeContent("content-1");
    }

    /// The core defect: a Removed record could previously be revived via
    /// updateContentStatus.
    function testRemovedRecord_CannotBeRevived() public {
        _register("content-1");
        vm.prank(creator);
        registry.removeContent("content-1");

        vm.prank(operator);
        vm.expectRevert(
            abi.encodeWithSelector(
                ContentRegistry.IllegalStateTransition.selector,
                ContentRegistry.ContentStatus.Removed,
                ContentRegistry.ContentStatus.Active
            )
        );
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Active);
    }

    /// The second defect: a removed contentId could be re-registered.
    function testRemovedId_CannotBeReused() public {
        _register("content-1");
        vm.prank(creator);
        registry.removeContent("content-1");

        vm.prank(operator);
        vm.expectRevert(abi.encodeWithSelector(ContentRegistry.ContentAlreadyExists.selector, "content-1"));
        registry.registerContent("content-1", creator, keccak256("new-bytes"), URI);
    }

    // --- creator does not drift ---

    function testCreator_DoesNotDrift() public {
        _register("content-1");
        address attacker = makeAddr("attacker");
        vm.prank(operator);
        registry.registerContent("content-2", attacker, DIGEST, URI);

        assertEq(registry.getContent("content-1").creator, creator);
        assertEq(registry.getContent("content-2").creator, attacker);
    }

    // --- pagination / counts ---

    function testPagination_OffsetAndLimit() public {
        for (uint256 i = 0; i < 5; i++) {
            _register(string.concat("content-", vm.toString(i)));
        }
        assertEq(registry.getContentCount(), 5);

        string[] memory page = registry.getContentIds(1, 2);
        assertEq(page.length, 2);
        assertEq(page[0], "content-1");
        assertEq(page[1], "content-2");

        string[] memory tail = registry.getContentIds(3, 10);
        assertEq(tail.length, 2, "limit beyond the end must clamp");
        assertEq(tail[1], "content-4");

        string[] memory empty = registry.getContentIds(5, 5);
        assertEq(empty.length, 0);
    }

    function testPagination_OffsetBeyondEndReverts() public {
        _register("content-1");
        vm.expectRevert(ContentRegistry.InvalidPagination.selector);
        registry.getContentIds(2, 1);
    }

    function testPagination_CountIncludesRemoved() public {
        _register("content-1");
        vm.prank(creator);
        registry.removeContent("content-1");

        assertEq(registry.getContentCount(), 1, "count is unique registrations, not playable content");
        string[] memory page = registry.getContentIds(0, 10);
        assertEq(page.length, 1);
        assertEq(page[0], "content-1");
    }

    function testCreatorListing() public {
        _register("content-1");
        _register("content-2");
        string[] memory owned = registry.getContentsByCreator(creator);
        assertEq(owned.length, 2);

        string[] memory page = registry.getContentsByCreatorPaged(creator, 1, 5);
        assertEq(page.length, 1);
        assertEq(page[0], "content-2");
    }

    // --- ownership ---

    function testOwnership_TwoStepTransfer() public {
        address newOperator = makeAddr("newOperator");
        vm.prank(operator);
        registry.transferOwnership(newOperator);
        assertEq(registry.owner(), operator, "ownership must not move until accepted");
        assertEq(registry.pendingOwner(), newOperator);

        vm.prank(newOperator);
        registry.acceptOwnership();
        assertEq(registry.owner(), newOperator);
    }

    function testOwnership_RenounceDisabled() public {
        vm.prank(operator);
        vm.expectRevert(ContentRegistry.RenounceDisabled.selector);
        registry.renounceOwnership();
    }

    // --- fuzz ---

    function testFuzz_UniqueRegistrationsCountAndListing(uint8 count) public {
        count = uint8(bound(count, 1, 30));
        for (uint256 i = 0; i < count; i++) {
            _register(string.concat("c-", vm.toString(i)));
        }
        assertEq(registry.getContentCount(), count);

        string[] memory all = registry.getContentIds(0, count);
        assertEq(all.length, count);
        for (uint256 i = 0; i < count; i++) {
            assertTrue(registry.contentExists(all[i]), "every listed id must exist");
        }
    }
}
