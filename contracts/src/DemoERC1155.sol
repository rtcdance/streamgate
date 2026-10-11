// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {ERC1155} from "@openzeppelin/contracts/token/ERC1155/ERC1155.sol";

/// Demo ERC-1155 fixture used by the Anvil integration suite. It exists only so
/// tests can mint an arbitrary quantity of a specific token id to a wallet and
/// assert quantity-based gating (balance 1 rejected / 2 accepted for min=2).
/// Permissionless mint keeps the fixture self-contained; it is never deployed
/// outside the test suite.
contract DemoERC1155 is ERC1155 {
    constructor() ERC1155("") {}

    function mint(address to, uint256 id, uint256 amount) external {
        _mint(to, id, amount, "");
    }
}
