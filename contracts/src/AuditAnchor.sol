// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @title AuditAnchor
/// @notice A public, append-only record of one 32-byte fingerprint per epoch of
///         the coordinator's audit history. The coordinator publishes the root
///         of a Merkle tree over every finished audit receipt of the epoch; a
///         provider (or anyone holding a receipt) can later prove the receipt
///         was part of that history, and that the coordinator did not rewrite
///         it afterwards.
/// @dev    The tree is the RFC 6962 tree built by the Go package
///         internal/receiptleaf: leaf = SHA-256(0x00 || record),
///         node = SHA-256(0x01 || left || right). verifyInclusion is a
///         convenience for demos and tests; the authoritative check lives in
///         the verifier tool, which reads `roots` straight from an RPC node.
///         Nothing here stores receipts, only fingerprints.
contract AuditAnchor {
    struct Anchor {
        bytes32 root;
        uint32 leafCount;
        uint64 timestamp;
    }

    /// @notice The only address allowed to publish roots.
    address public immutable coordinator;

    /// @notice The highest epoch anchored so far (0 before the first anchor).
    uint64 public lastEpoch;

    mapping(uint64 epoch => Anchor) private _anchors;

    event Anchored(uint64 indexed epoch, bytes32 root, uint32 leafCount);

    error ZeroCoordinator();
    error NotCoordinator();
    error EpochNotIncreasing(uint64 epoch, uint64 lastAnchored);
    error EmptyAnchor();

    constructor(address coordinator_) {
        if (coordinator_ == address(0)) revert ZeroCoordinator();
        coordinator = coordinator_;
    }

    /// @notice Publish the root for `epoch`. Epochs strictly increase (gaps are
    ///         allowed), so an epoch can be anchored once and never replaced.
    function anchor(uint64 epoch, bytes32 root, uint32 leafCount) external {
        if (msg.sender != coordinator) revert NotCoordinator();
        if (epoch <= lastEpoch) revert EpochNotIncreasing(epoch, lastEpoch);
        if (root == bytes32(0) || leafCount == 0) revert EmptyAnchor();

        lastEpoch = epoch;
        _anchors[epoch] = Anchor({root: root, leafCount: leafCount, timestamp: uint64(block.timestamp)});
        emit Anchored(epoch, root, leafCount);
    }

    /// @notice Read an anchor. An epoch that was never anchored reads as zeros.
    function roots(uint64 epoch) external view returns (bytes32 root, uint32 leafCount, uint64 timestamp) {
        Anchor storage a = _anchors[epoch];
        return (a.root, a.leafCount, a.timestamp);
    }

    /// @notice Check an RFC 6962 inclusion proof against the stored root and
    ///         leaf count of `epoch` (RFC 9162 section 2.1.3.2).
    /// @param leafHash SHA-256(0x00 || record) of the receipt.
    function verifyInclusion(uint64 epoch, bytes32 leafHash, uint32 index, bytes32[] calldata proof)
        external
        view
        returns (bool)
    {
        Anchor storage a = _anchors[epoch];
        if (a.timestamp == 0 || index >= a.leafCount) return false;

        uint256 fn = index;
        uint256 sn = uint256(a.leafCount) - 1;
        bytes32 r = leafHash;
        for (uint256 i = 0; i < proof.length; i++) {
            if (sn == 0) return false;
            if (fn & 1 == 1 || fn == sn) {
                r = sha256(abi.encodePacked(bytes1(0x01), proof[i], r));
                if (fn & 1 == 0) {
                    while (fn & 1 == 0 && fn != 0) {
                        fn >>= 1;
                        sn >>= 1;
                    }
                }
            } else {
                r = sha256(abi.encodePacked(bytes1(0x01), r, proof[i]));
            }
            fn >>= 1;
            sn >>= 1;
        }
        return sn == 0 && r == a.root;
    }
}
