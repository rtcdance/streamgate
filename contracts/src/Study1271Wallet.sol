// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {IERC1271} from "openzeppelin-contracts/contracts/interfaces/IERC1271.sol";
import {ECDSA} from "openzeppelin-contracts/contracts/utils/cryptography/ECDSA.sol";
import {ERC721Holder} from "openzeppelin-contracts/contracts/token/ERC721/utils/ERC721Holder.sol";
import {ERC1155Holder} from "openzeppelin-contracts/contracts/token/ERC1155/utils/ERC1155Holder.sol";

/// @notice DEMO/TEST ONLY: single EOA owner wallet for StreamGate login and NFT fixtures.
/// @dev Not an audited production wallet. No relayed execution or ERC-4337 support.
contract Study1271Wallet is IERC1271, ERC721Holder, ERC1155Holder {
    address public owner;

    error Unauthorized();
    error InvalidOwner();
    error InvalidTarget();
    error ExecutionFailed(bytes reason);

    event OwnerChanged(address indexed previousOwner, address indexed newOwner);
    event Executed(address indexed target, uint256 value, bytes32 dataHash);

    modifier onlyOwner() {
        // Signing a login proof never authorizes a caller to move this wallet's assets.
        if (msg.sender != owner) revert Unauthorized();
        _;
    }

    constructor(address initialOwner) {
        _setOwner(initialOwner);
    }

    /// @notice Accept native currency for owner-directed calls.
    receive() external payable {}

    /// @notice Rotate the EOA signer; previously accepted proofs must be revalidated.
    function setOwner(address newOwner) external onlyOwner {
        _setOwner(newOwner);
    }

    /// @notice Execute exactly the target, value and calldata approved by the owner transaction.
    function execute(address target, uint256 value, bytes calldata data)
        external
        onlyOwner
        returns (bytes memory result)
    {
        if (target == address(0)) revert InvalidTarget();
        // CALL preserves the wallet as asset owner. Never delegatecall untrusted target code.
        (bool success, bytes memory output) = target.call{value: value}(data);
        if (!success) revert ExecutionFailed(output);
        emit Executed(target, value, keccak256(data));
        return output;
    }

    /// @notice Validate an already hashed message under the current owner's policy.
    /// @dev The caller supplies the EIP-191 digest; hashing it again would change the signed message.
    function isValidSignature(bytes32 digest, bytes calldata signature) external view returns (bytes4) {
        address recovered;
        ECDSA.RecoverError err;
        if (signature.length == 64) {
            bytes32 r;
            bytes32 vs;
            // ERC-2098 packs parity into s. Read only after the exact 64-byte bounds check;
            // OpenZeppelin enforces canonical s and valid recovery for both encodings.
            assembly ("memory-safe") {
                r := calldataload(signature.offset)
                vs := calldataload(add(signature.offset, 32))
            }
            (recovered, err,) = ECDSA.tryRecover(digest, r, vs);
        } else {
            (recovered, err,) = ECDSA.tryRecoverCalldata(digest, signature);
        }
        return err == ECDSA.RecoverError.NoError && recovered == owner
            ? IERC1271.isValidSignature.selector
            : bytes4(0xffffffff);
    }

    function _setOwner(address newOwner) private {
        // This fixture deliberately supports one EOA controller, not recursive contract owners.
        if (newOwner == address(0) || newOwner == address(this) || newOwner.code.length != 0) {
            revert InvalidOwner();
        }
        address previousOwner = owner;
        owner = newOwner;
        emit OwnerChanged(previousOwner, newOwner);
    }
}
