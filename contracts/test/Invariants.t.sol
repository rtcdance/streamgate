// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {DemoNFT} from "../src/DemoNFT.sol";
import {ContentRegistry} from "../src/ContentRegistry.sol";

/// Handler driving DemoNFT with mint / approve / transfer sequences.
contract NFTHandler is Test {
    DemoNFT public nft;
    address[] public actors;
    uint256 public minted;

    constructor(DemoNFT _nft) {
        nft = _nft;
        actors.push(makeAddr("nft-actor-1"));
        actors.push(makeAddr("nft-actor-2"));
        actors.push(makeAddr("nft-actor-3"));
    }

    function actorCount() external view returns (uint256) {
        return actors.length;
    }

    function actorAt(uint256 i) external view returns (address) {
        return actors[i];
    }

    function mint(uint256 seed) public {
        address to = actors[seed % actors.length];
        vm.prank(to);
        nft.mint(to);
        minted++;
    }

    /// Approve then transfer, and assert the transfer cleared the approval.
    function approveAndTransfer(uint256 seed, uint256 tokenSeed) public {
        if (minted == 0) return;
        uint256 tokenId = (tokenSeed % minted) + 1;
        address from = nft.ownerOf(tokenId);
        address spender = actors[seed % actors.length];
        if (spender == from) return;

        vm.prank(from);
        nft.approve(spender, tokenId);
        vm.prank(spender);
        nft.transferFrom(from, spender, tokenId);

        require(nft.getApproved(tokenId) == address(0), "approval not cleared on transfer");
    }

    function sumBalances() external view returns (uint256 sum) {
        for (uint256 i = 0; i < actors.length; i++) {
            sum += nft.balanceOf(actors[i]);
        }
    }
}

contract NFTInvariantTest is Test {
    DemoNFT internal nft;
    NFTHandler internal handler;

    function setUp() public {
        nft = new DemoNFT();
        handler = new NFTHandler(nft);
        targetContract(address(handler));
    }

    /// Balances are conserved (every minted token is owned exactly once) and no
    /// transfer leaves a stale approval behind.
    function invariant_NFTBalancesAndApproval() public view {
        assertEq(handler.sumBalances(), nft.totalSupply(), "balances must sum to supply");

        uint256 minted = handler.minted();
        for (uint256 tokenId = 1; tokenId <= minted; tokenId++) {
            assertTrue(nft.ownerOf(tokenId) != address(0), "every minted token must have an owner");
        }
    }
}

/// Handler driving ContentRegistry through register / flag / remove.
contract RegistryHandler is Test {
    ContentRegistry public registry;
    address public operator;
    address public creator;

    string[] public registered;
    mapping(bytes32 => bool) private _removed;

    constructor(ContentRegistry _registry, address _operator, address _creator) {
        registry = _registry;
        operator = _operator;
        creator = _creator;
    }

    function registeredCount() external view returns (uint256) {
        return registered.length;
    }

    function wasRemoved(string memory id) external view returns (bool) {
        return _removed[keccak256(bytes(id))];
    }

    function _id(uint256 seed) internal view returns (string memory) {
        return string.concat("content-", vm.toString(seed % 12));
    }

    function register(uint256 seed) public {
        string memory id = _id(seed);
        if (registry.contentExists(id)) return; // registered ids are never reusable
        vm.prank(operator);
        registry.registerContent(id, creator, keccak256(bytes(id)), "ipfs://m");
        registered.push(id);
    }

    function flag(uint256 seed) public {
        string memory id = _id(seed);
        if (!registry.contentExists(id)) return;
        if (uint256(registry.getContent(id).status) != uint256(ContentRegistry.ContentStatus.Active)) return;
        vm.prank(operator);
        registry.updateContentStatus(id, ContentRegistry.ContentStatus.Flagged);
    }

    function unflag(uint256 seed) public {
        string memory id = _id(seed);
        if (!registry.contentExists(id)) return;
        if (uint256(registry.getContent(id).status) != uint256(ContentRegistry.ContentStatus.Flagged)) return;
        vm.prank(operator);
        registry.updateContentStatus(id, ContentRegistry.ContentStatus.Active);
    }

    function remove(uint256 seed) public {
        string memory id = _id(seed);
        if (!registry.contentExists(id)) return;
        if (uint256(registry.getContent(id).status) == uint256(ContentRegistry.ContentStatus.Removed)) return;
        vm.prank(creator);
        registry.removeContent(id);
        _removed[keccak256(bytes(id))] = true;
    }
}

contract RegistryInvariantTest is Test {
    ContentRegistry internal registry;
    RegistryHandler internal handler;
    address internal operator = makeAddr("registry-operator");
    address internal creator = makeAddr("registry-creator");

    function setUp() public {
        vm.prank(operator);
        registry = new ContentRegistry();
        handler = new RegistryHandler(registry, operator, creator);
        targetContract(address(handler));
    }

    /// Every registered id exists exactly once, the count never drifts from the
    /// number of unique registrations, and Removed is terminal.
    function invariant_RegistryUniqueAndTerminalState() public view {
        uint256 n = handler.registeredCount();
        assertEq(registry.getContentCount(), n, "count must equal unique registrations");

        for (uint256 i = 0; i < n; i++) {
            string memory id = handler.registered(i);
            assertTrue(registry.contentExists(id), "a registered id must never return to Nonexistent");

            if (handler.wasRemoved(id)) {
                assertEq(
                    uint256(registry.getContent(id).status),
                    uint256(ContentRegistry.ContentStatus.Removed),
                    "a removed record must stay removed"
                );
            }
        }
    }
}
