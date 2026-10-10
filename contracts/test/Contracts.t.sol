// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {ContentRegistry} from "../src/ContentRegistry.sol";

contract ContentRegistryTest is Test {
    ContentRegistry public registry;
    address public creator = makeAddr("creator");
    address public user = makeAddr("user");

    function setUp() public {
        vm.prank(creator);
        registry = new ContentRegistry();
    }

    function testRegisterContent() public {
        vm.prank(creator);
        registry.registerContent("content-1", "QmHash123", "ipfs://metadata/1");

        ContentRegistry.ContentRecord memory record = registry.getContent("content-1");
        assertEq(record.creator, creator);
        assertEq(record.contentHash, "QmHash123");
        assertEq(record.metadataURI, "ipfs://metadata/1");
        assertEq(uint256(record.status), uint256(ContentRegistry.ContentStatus.Active));
    }

    function testRegisterContentDuplicateReverts() public {
        vm.prank(creator);
        registry.registerContent("content-1", "QmHash123", "ipfs://metadata/1");

        vm.prank(creator);
        vm.expectRevert(abi.encodeWithSelector(ContentRegistry.ContentAlreadyExists.selector, "content-1"));
        registry.registerContent("content-1", "QmHash456", "ipfs://metadata/2");
    }

    function testUpdateContentStatus() public {
        vm.prank(creator);
        registry.registerContent("content-1", "QmHash123", "ipfs://metadata/1");

        vm.prank(creator);
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Flagged);

        ContentRegistry.ContentRecord memory record = registry.getContent("content-1");
        assertEq(uint256(record.status), uint256(ContentRegistry.ContentStatus.Flagged));
    }

    function testUnauthorizedUpdateReverts() public {
        vm.prank(creator);
        registry.registerContent("content-1", "QmHash123", "ipfs://metadata/1");

        vm.prank(user);
        vm.expectRevert(
            abi.encodeWithSelector(ContentRegistry.UnauthorizedCaller.selector, user, creator)
        );
        registry.updateContentStatus("content-1", ContentRegistry.ContentStatus.Removed);
    }

    function testContentExists() public {
        assertFalse(registry.contentExists("content-1"));

        vm.prank(creator);
        registry.registerContent("content-1", "QmHash123", "ipfs://metadata/1");

        assertTrue(registry.contentExists("content-1"));
    }
}
