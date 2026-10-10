// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script} from "forge-std/Script.sol";
import {console} from "forge-std/console.sol";
import {ContentRegistry} from "../src/ContentRegistry.sol";

contract Deploy is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");

        vm.startBroadcast(deployerPrivateKey);

        ContentRegistry contentRegistry = new ContentRegistry();
        console.log("ContentRegistry deployed at:", address(contentRegistry));

        vm.stopBroadcast();

        console.log("=== Deployment Complete ===");
        console.log("ContentRegistry:", address(contentRegistry));
    }
}
