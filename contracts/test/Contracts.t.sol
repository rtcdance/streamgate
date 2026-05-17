// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {ContentRegistry} from "../src/ContentRegistry.sol";
import {StreamNFT} from "../src/StreamNFT.sol";
import {NFTGate} from "../src/NFTGate.sol";

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

contract StreamNFTTest is Test {
    StreamNFT public streamNFT;
    address public deployer = makeAddr("deployer");
    address public recipient = makeAddr("recipient");

    function setUp() public {
        vm.prank(deployer);
        streamNFT = new StreamNFT();
    }

    function testMintStreamNFT() public {
        vm.prank(deployer);
        uint256 tokenId = streamNFT.mintStreamNFT(
            recipient,
            "content-1",
            "ipfs://token-uri/1",
            "https://stream.example.com/content-1/master.m3u8",
            3600,
            5000000,
            false
        );

        assertEq(tokenId, 1);
        assertEq(streamNFT.ownerOf(tokenId), recipient);
        assertEq(streamNFT.getStreamURL(tokenId), "https://stream.example.com/content-1/master.m3u8");
        assertFalse(streamNFT.isPremiumContent(tokenId));
    }

    function testMintDuplicateContentReverts() public {
        vm.startPrank(deployer);
        streamNFT.mintStreamNFT(
            recipient,
            "content-1",
            "ipfs://token-uri/1",
            "https://stream.example.com/hls.m3u8",
            3600,
            5000000,
            false
        );

        vm.expectRevert(abi.encodeWithSelector(StreamNFT.ContentAlreadyMinted.selector, "content-1"));
        streamNFT.mintStreamNFT(
            recipient,
            "content-1",
            "ipfs://token-uri/2",
            "https://stream.example.com/dash.mpd",
            3600,
            5000000,
            false
        );
        vm.stopPrank();
    }
}

contract NFTGateTest is Test {
    NFTGate public gate;
    StreamNFT public nft;
    address public owner = makeAddr("owner");
    address public holder = makeAddr("holder");
    address public nonHolder = makeAddr("nonHolder");

    function setUp() public {
        vm.startPrank(owner);
        gate = new NFTGate();
        nft = new StreamNFT();
        vm.stopPrank();
    }

    function testCreateAndCheckAccess() public {
        vm.prank(owner);
        nft.mintStreamNFT(
            holder,
            "content-1",
            "ipfs://token-uri/1",
            "https://stream.example.com/master.m3u8",
            3600,
            5000000,
            false
        );

        vm.prank(owner);
        gate.createGateRule(
            "rule-1",
            address(nft),
            1,
            1,
            NFTGate.NFTStandard.ERC721,
            block.timestamp,
            block.timestamp + 365 days
        );

        (bool granted, uint256 balance) = gate.checkAccess("rule-1", holder);
        assertTrue(granted);
        assertEq(balance, 1);
    }

    function testAccessDeniedForNonHolder() public {
        vm.prank(owner);
        nft.mintStreamNFT(
            holder,
            "content-1",
            "ipfs://token-uri/1",
            "https://stream.example.com/master.m3u8",
            3600,
            5000000,
            false
        );

        vm.prank(owner);
        gate.createGateRule(
            "rule-1",
            address(nft),
            1,
            1,
            NFTGate.NFTStandard.ERC721,
            block.timestamp,
            block.timestamp + 365 days
        );

        (bool granted, uint256 balance) = gate.checkAccess("rule-1", nonHolder);
        assertFalse(granted);
        assertEq(balance, 0);
    }

    function testExpiredRuleReverts() public {
        vm.prank(owner);
        gate.createGateRule(
            "rule-expired",
            address(nft),
            1,
            1,
            NFTGate.NFTStandard.ERC721,
            block.timestamp,
            block.timestamp + 1 hours
        );

        vm.warp(block.timestamp + 2 hours);

        vm.expectRevert(abi.encodeWithSelector(NFTGate.RuleNotFound.selector, "rule-expired"));
        gate.checkAccess("rule-expired", holder);
    }
}