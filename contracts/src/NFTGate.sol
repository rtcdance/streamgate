// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {IERC721} from "openzeppelin-contracts/contracts/token/ERC721/IERC721.sol";
import {IERC1155} from "openzeppelin-contracts/contracts/token/ERC1155/IERC1155.sol";
import {Ownable} from "openzeppelin-contracts/contracts/access/Ownable.sol";

contract NFTGate is Ownable {
    constructor() Ownable(msg.sender) {}

    struct GateRule {
        address nftContract;
        uint256 tokenId;
        uint256 minBalance;
        NFTStandard standard;
        uint256 validFrom;
        uint256 validUntil;
        bool active;
    }

    enum NFTStandard {
        ERC721,
        ERC1155
    }

    mapping(string => GateRule) private _gateRules;
    string[] private _ruleIds;

    event GateRuleCreated(
        string indexed ruleId,
        address indexed nftContract,
        uint256 tokenId,
        NFTStandard standard,
        uint256 minBalance
    );

    event GateRuleUpdated(string indexed ruleId, bool active);
    event GateRuleRemoved(string indexed ruleId);
    event AccessChecked(
        string indexed ruleId,
        address indexed user,
        bool granted,
        uint256 balance
    );

    error RuleNotFound(string ruleId);
    error RuleAlreadyExists(string ruleId);
    error InvalidNFTContract(address nftContract);
    error AccessDenied(string ruleId, address user);

    modifier validRule(string calldata ruleId) {
        GateRule storage rule = _gateRules[ruleId];
        if (!rule.active) {
            revert RuleNotFound(ruleId);
        }
        if (block.timestamp < rule.validFrom || block.timestamp > rule.validUntil) {
            revert RuleNotFound(ruleId);
        }
        _;
    }

    function createGateRule(
        string calldata ruleId,
        address nftContract,
        uint256 tokenId,
        uint256 minBalance,
        NFTStandard standard,
        uint256 validFrom,
        uint256 validUntil
    ) external onlyOwner {
        if (_gateRules[ruleId].active) {
            revert RuleAlreadyExists(ruleId);
        }
        if (nftContract == address(0)) {
            revert InvalidNFTContract(nftContract);
        }

        _gateRules[ruleId] = GateRule({
            nftContract: nftContract,
            tokenId: tokenId,
            minBalance: minBalance,
            standard: standard,
            validFrom: validFrom,
            validUntil: validUntil,
            active: true
        });

        _ruleIds.push(ruleId);

        emit GateRuleCreated(ruleId, nftContract, tokenId, standard, minBalance);
    }

    function updateGateRuleStatus(string calldata ruleId, bool active) external onlyOwner {
        GateRule storage rule = _gateRules[ruleId];
        if (rule.nftContract == address(0)) {
            revert RuleNotFound(ruleId);
        }
        rule.active = active;
        emit GateRuleUpdated(ruleId, active);
    }

    function removeGateRule(string calldata ruleId) external onlyOwner {
        if (_gateRules[ruleId].nftContract == address(0)) {
            revert RuleNotFound(ruleId);
        }
        delete _gateRules[ruleId];
        emit GateRuleRemoved(ruleId);
    }

    function checkAccess(
        string calldata ruleId,
        address user
    ) external view validRule(ruleId) returns (bool granted, uint256 balance) {
        GateRule storage rule = _gateRules[ruleId];

        if (rule.standard == NFTStandard.ERC721) {
            if (rule.tokenId == 0) {
                balance = IERC721(rule.nftContract).balanceOf(user);
            } else {
                address owner = IERC721(rule.nftContract).ownerOf(rule.tokenId);
                balance = (owner == user) ? 1 : 0;
            }
        } else {
            balance = IERC1155(rule.nftContract).balanceOf(user, rule.tokenId);
        }

        granted = balance >= rule.minBalance;
        return (granted, balance);
    }

    function verifyAccess(
        string calldata ruleId,
        address user
    ) external validRule(ruleId) returns (bool) {
        (bool granted, uint256 balance) = this.checkAccess(ruleId, user);
        emit AccessChecked(ruleId, user, granted, balance);
        return granted;
    }

    function verifyBatchAccess(
        string[] calldata ruleIds,
        address user
    ) external returns (bool[] memory results) {
        results = new bool[](ruleIds.length);
        for (uint256 i = 0; i < ruleIds.length; i++) {
            try this.checkAccess(ruleIds[i], user) returns (
                bool granted,
                uint256
            ) {
                results[i] = granted;
            } catch {
                results[i] = false;
            }
        }
        return results;
    }

    function getGateRule(string calldata ruleId) external view returns (GateRule memory) {
        GateRule storage rule = _gateRules[ruleId];
        if (rule.nftContract == address(0)) {
            revert RuleNotFound(ruleId);
        }
        return rule;
    }

    function getRuleCount() external view returns (uint256) {
        return _ruleIds.length;
    }
}