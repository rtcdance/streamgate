// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Ownable} from "openzeppelin-contracts/contracts/access/Ownable.sol";
import {Ownable2Step} from "openzeppelin-contracts/contracts/access/Ownable2Step.sol";

/// Platform content attestation registry.
///
/// This contract records that the platform operator (the contract owner, i.e.
/// the `registrar`) attested to a content's existence and to the SHA-256 digest
/// of its original bytes. It is NOT a copyright registry and NOT an uploader
/// identity proof: `creator` is the verified platform-side owner of the
/// content, passed in by the operator, and the operator is always recoverable
/// from the transaction sender — the two roles are never conflated.
///
/// State machine (terminal states are final):
///
///     Nonexistent --register--> Active
///     Active  <--- owner --->  Flagged        (onlyOwner, both directions)
///     Active | Flagged -- owner or creator --> Removed   (terminal)
///
/// A contentId can never return to Nonexistent, a Removed record can never be
/// revived, and a registered contentId can never be reused (tombstone).
contract ContentRegistry is Ownable2Step {
    enum ContentStatus {
        Nonexistent,
        Active,
        Flagged,
        Removed
    }

    struct ContentRecord {
        string contentId;
        address creator;
        bytes32 contentDigest;
        string metadataURI;
        ContentStatus status;
        uint256 createdAt;
        uint256 updatedAt;
    }

    uint256 public constant MAX_CONTENT_ID_BYTES = 128;
    uint256 public constant MAX_METADATA_URI_BYTES = 512;

    mapping(string => ContentRecord) private _contents;
    // Tombstone key set: a contentId, once registered, is used forever — even
    // after removal — so IDs can never be recycled.
    mapping(bytes32 => bool) private _registered;
    string[] private _contentIds;
    mapping(address => string[]) private _creatorContents;

    event ContentRegistered(
        bytes32 indexed contentKey,
        string contentId,
        address indexed creator,
        bytes32 contentDigest,
        string metadataURI,
        uint256 timestamp
    );

    event ContentStatusUpdated(
        bytes32 indexed contentKey,
        string contentId,
        ContentStatus oldStatus,
        ContentStatus newStatus,
        uint256 timestamp
    );

    error ContentAlreadyExists(string contentId);
    error ContentNotFound(string contentId);
    error UnauthorizedCaller(address caller, address creator);
    error IllegalStateTransition(ContentStatus from, ContentStatus to);
    error InvalidCreator();
    error InvalidContentId();
    error InvalidContentDigest();
    error MetadataURITooLong();
    error InvalidPagination();
    error RenounceDisabled();

    constructor() Ownable(msg.sender) {}

    /// Ownership can only be handed over via the two-step transfer; renouncing
    /// would permanently freeze the attestation role and is disabled.
    function renounceOwnership() public view override onlyOwner {
        revert RenounceDisabled();
    }

    function registerContent(
        string calldata contentId,
        address creator,
        bytes32 contentDigest,
        string calldata metadataURI
    ) external onlyOwner {
        uint256 idLen = bytes(contentId).length;
        if (idLen == 0 || idLen > MAX_CONTENT_ID_BYTES) {
            revert InvalidContentId();
        }
        if (creator == address(0)) {
            revert InvalidCreator();
        }
        if (contentDigest == bytes32(0)) {
            revert InvalidContentDigest();
        }
        if (bytes(metadataURI).length > MAX_METADATA_URI_BYTES) {
            revert MetadataURITooLong();
        }

        bytes32 key = keccak256(bytes(contentId));
        if (_registered[key]) {
            revert ContentAlreadyExists(contentId);
        }
        _registered[key] = true;

        _contents[contentId] = ContentRecord({
            contentId: contentId,
            creator: creator,
            contentDigest: contentDigest,
            metadataURI: metadataURI,
            status: ContentStatus.Active,
            createdAt: block.timestamp,
            updatedAt: block.timestamp
        });

        _contentIds.push(contentId);
        _creatorContents[creator].push(contentId);

        emit ContentRegistered(key, contentId, creator, contentDigest, metadataURI, block.timestamp);
    }

    /// Only Active <-> Flagged transitions are reachable here, and only the
    /// platform owner may perform them. Removal has its own entry point.
    function updateContentStatus(string calldata contentId, ContentStatus newStatus) external {
        ContentRecord storage record = _contents[contentId];
        ContentStatus current = record.status;
        if (current == ContentStatus.Nonexistent) {
            revert ContentNotFound(contentId);
        }
        if (current == ContentStatus.Removed) {
            revert IllegalStateTransition(current, newStatus);
        }
        if (newStatus != ContentStatus.Active && newStatus != ContentStatus.Flagged) {
            revert IllegalStateTransition(current, newStatus);
        }
        if (msg.sender != owner()) {
            revert UnauthorizedCaller(msg.sender, record.creator);
        }

        record.status = newStatus;
        record.updatedAt = block.timestamp;
        emit ContentStatusUpdated(keccak256(bytes(contentId)), contentId, current, newStatus, block.timestamp);
    }

    /// Removed is terminal: a removed record can never be updated or revived.
    function removeContent(string calldata contentId) external {
        ContentRecord storage record = _contents[contentId];
        ContentStatus current = record.status;
        if (current == ContentStatus.Nonexistent) {
            revert ContentNotFound(contentId);
        }
        if (current == ContentStatus.Removed) {
            revert IllegalStateTransition(current, ContentStatus.Removed);
        }
        if (msg.sender != record.creator && msg.sender != owner()) {
            revert UnauthorizedCaller(msg.sender, record.creator);
        }

        record.status = ContentStatus.Removed;
        record.updatedAt = block.timestamp;
        emit ContentStatusUpdated(keccak256(bytes(contentId)), contentId, current, ContentStatus.Removed, block.timestamp);
    }

    function getContent(string calldata contentId) external view returns (ContentRecord memory) {
        ContentRecord memory record = _contents[contentId];
        if (record.status == ContentStatus.Nonexistent) {
            revert ContentNotFound(contentId);
        }
        return record;
    }

    /// Paginated listing. `getContentCount()` is the total number of unique
    /// registrations; a Removed record still counts as registered (its
    /// existence is not a claim that it is playable).
    function getContentIds(uint256 offset, uint256 limit) external view returns (string[] memory page) {
        return _page(_contentIds, offset, limit);
    }

    function getContentsByCreator(address creator) external view returns (string[] memory) {
        return _creatorContents[creator];
    }

    function getContentsByCreatorPaged(address creator, uint256 offset, uint256 limit)
        external
        view
        returns (string[] memory page)
    {
        return _page(_creatorContents[creator], offset, limit);
    }

    function getContentCount() external view returns (uint256) {
        return _contentIds.length;
    }

    function contentExists(string calldata contentId) external view returns (bool) {
        return _contents[contentId].status != ContentStatus.Nonexistent;
    }

    function _page(string[] storage ids, uint256 offset, uint256 limit)
        private
        view
        returns (string[] memory page)
    {
        uint256 total = ids.length;
        if (offset > total) {
            revert InvalidPagination();
        }
        uint256 remaining = total - offset;
        uint256 size = remaining < limit ? remaining : limit;
        page = new string[](size);
        for (uint256 i = 0; i < size; i++) {
            page[i] = ids[offset + i];
        }
    }
}
