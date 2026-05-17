// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Ownable} from "openzeppelin-contracts/contracts/access/Ownable.sol";

contract ContentRegistry is Ownable {
    struct ContentRecord {
        string contentId;
        address creator;
        string contentHash;
        string metadataURI;
        ContentStatus status;
        uint256 createdAt;
        uint256 updatedAt;
    }

    enum ContentStatus {
        Nonexistent,
        Active,
        Flagged,
        Removed
    }

    mapping(string => ContentRecord) private _contents;
    mapping(address => string[]) private _creatorContents;
    string[] private _contentIds;

    event ContentRegistered(
        string indexed contentId,
        address indexed creator,
        string contentHash,
        string metadataURI,
        uint256 timestamp
    );

    event ContentStatusUpdated(
        string indexed contentId,
        ContentStatus oldStatus,
        ContentStatus newStatus,
        uint256 timestamp
    );

    event ContentRemoved(string indexed contentId, uint256 timestamp);

    error ContentAlreadyExists(string contentId);
    error ContentNotFound(string contentId);
    error UnauthorizedCaller(address caller, address creator);

    function registerContent(
        string calldata contentId,
        string calldata contentHash,
        string calldata metadataURI
    ) external {
        if (_contents[contentId].status != ContentStatus.Nonexistent) {
            revert ContentAlreadyExists(contentId);
        }

        _contents[contentId] = ContentRecord({
            contentId: contentId,
            creator: msg.sender,
            contentHash: contentHash,
            metadataURI: metadataURI,
            status: ContentStatus.Active,
            createdAt: block.timestamp,
            updatedAt: block.timestamp
        });

        _creatorContents[msg.sender].push(contentId);
        _contentIds.push(contentId);

        emit ContentRegistered(contentId, msg.sender, contentHash, metadataURI, block.timestamp);
    }

    function updateContentStatus(
        string calldata contentId,
        ContentStatus newStatus
    ) external {
        ContentRecord storage record = _contents[contentId];
        if (record.status == ContentStatus.Nonexistent) {
            revert ContentNotFound(contentId);
        }
        if (msg.sender != record.creator && msg.sender != owner()) {
            revert UnauthorizedCaller(msg.sender, record.creator);
        }

        ContentStatus oldStatus = record.status;
        record.status = newStatus;
        record.updatedAt = block.timestamp;

        emit ContentStatusUpdated(contentId, oldStatus, newStatus, block.timestamp);
    }

    function removeContent(string calldata contentId) external {
        ContentRecord storage record = _contents[contentId];
        if (record.status == ContentStatus.Nonexistent) {
            revert ContentNotFound(contentId);
        }
        if (msg.sender != record.creator && msg.sender != owner()) {
            revert UnauthorizedCaller(msg.sender, record.creator);
        }

        record.status = ContentStatus.Removed;
        record.updatedAt = block.timestamp;

        emit ContentRemoved(contentId, block.timestamp);
    }

    function getContent(string calldata contentId) external view returns (ContentRecord memory) {
        ContentRecord memory record = _contents[contentId];
        if (record.status == ContentStatus.Nonexistent) {
            revert ContentNotFound(contentId);
        }
        return record;
    }

    function getContentsByCreator(address creator) external view returns (string[] memory) {
        return _creatorContents[creator];
    }

    function getContentCount() external view returns (uint256) {
        return _contentIds.length;
    }

    function contentExists(string calldata contentId) external view returns (bool) {
        return _contents[contentId].status != ContentStatus.Nonexistent;
    }
}