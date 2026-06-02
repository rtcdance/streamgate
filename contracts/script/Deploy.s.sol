// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script} from "forge-std/Script.sol";
import {console} from "forge-std/console.sol";
import {ContentRegistry} from "../src/ContentRegistry.sol";
import {StreamNFT} from "../src/StreamNFT.sol";
import {NFTGate} from "../src/NFTGate.sol";

contract Deploy is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");

        vm.startBroadcast(deployerPrivateKey);

        ContentRegistry contentRegistry = new ContentRegistry();
        console.log("ContentRegistry deployed at:", address(contentRegistry));

        StreamNFT streamNFT = new StreamNFT();
        console.log("StreamNFT deployed at:", address(streamNFT));

        NFTGate nftGate = new NFTGate();
        console.log("NFTGate deployed at:", address(nftGate));

        vm.stopBroadcast();

        console.log("=== Deployment Complete ===");
        console.log("ContentRegistry:", address(contentRegistry));
        console.log("StreamNFT:", address(streamNFT));
        console.log("NFTGate:", address(nftGate));
    }
}