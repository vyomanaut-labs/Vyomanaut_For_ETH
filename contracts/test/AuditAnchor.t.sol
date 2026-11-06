// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import {TestBase} from "./TestBase.sol";
import {AuditAnchor} from "../src/AuditAnchor.sol";

contract AuditAnchorTest is TestBase {
    AuditAnchor internal anchorC;
    address internal coordinator = address(0xC0DE);
    address internal stranger = address(0xBAD);

    // Reference vectors: the RFC 6962 tree over the Certificate Transparency
    // test inputs, with proofs produced by an independent implementation.
    bytes32 internal constant ROOT_8 = 0x5dc9da79a70659a9ad559cb701ded9a2ab9d823aad2f4960cfe370eff4604328;
    bytes32 internal constant ROOT_5 = 0x4e3bbb1f7b478dcfe71fb631631519a3bca12c9aefca1612bfce4c13a86264d4;
    bytes32 internal constant ROOT_7 = 0xddb89be403809e325750d3d263cd78929c2942b7942a34b77e122c9594a74c8c;

    function setUp() public {
        vm.warp(1_000_000);
        anchorC = new AuditAnchor(coordinator);
    }

    function testAnchorStoresRootCountAndTimestamp() public {
        vm.prank(coordinator);
        anchorC.anchor(7, ROOT_8, 8);
        (bytes32 root, uint32 count, uint64 ts) = anchorC.roots(7);
        assertEq(uint256(root), uint256(ROOT_8), "root");
        assertEq(count, 8, "leaf count");
        assertEq(ts, 1_000_000, "timestamp");
        assertEq(anchorC.lastEpoch(), 7, "last epoch");
    }

    function testOnlyCoordinatorCanAnchor() public {
        vm.prank(stranger);
        vm.expectRevert(AuditAnchor.NotCoordinator.selector);
        anchorC.anchor(1, ROOT_8, 8);
    }

    function testEpochsMustStrictlyIncrease() public {
        vm.startPrank(coordinator);
        anchorC.anchor(5, ROOT_8, 8);

        vm.expectRevert(abi.encodeWithSelector(AuditAnchor.EpochNotIncreasing.selector, uint64(5), uint64(5)));
        anchorC.anchor(5, ROOT_5, 5);

        vm.expectRevert(abi.encodeWithSelector(AuditAnchor.EpochNotIncreasing.selector, uint64(3), uint64(5)));
        anchorC.anchor(3, ROOT_5, 5);

        anchorC.anchor(9, ROOT_5, 5); // gaps are allowed
        vm.stopPrank();
        assertEq(anchorC.lastEpoch(), 9, "gap accepted");

        // the first anchor was never replaced
        (bytes32 root,,) = anchorC.roots(5);
        assertEq(uint256(root), uint256(ROOT_8), "epoch 5 unchanged");
    }

    function testEpochZeroIsRejected() public {
        vm.prank(coordinator);
        vm.expectRevert(abi.encodeWithSelector(AuditAnchor.EpochNotIncreasing.selector, uint64(0), uint64(0)));
        anchorC.anchor(0, ROOT_8, 8);
    }

    function testEmptyAnchorIsRejected() public {
        vm.startPrank(coordinator);
        vm.expectRevert(AuditAnchor.EmptyAnchor.selector);
        anchorC.anchor(1, bytes32(0), 8);
        vm.expectRevert(AuditAnchor.EmptyAnchor.selector);
        anchorC.anchor(1, ROOT_8, 0);
        vm.stopPrank();
    }

    function testUnknownEpochReadsAsZeros() public view {
        (bytes32 root, uint32 count, uint64 ts) = anchorC.roots(42);
        assertEq(uint256(root), 0, "root");
        assertEq(count, 0, "count");
        assertEq(ts, 0, "ts");
    }

    function testConstructorRejectsZeroCoordinator() public {
        vm.expectRevert(AuditAnchor.ZeroCoordinator.selector);
        new AuditAnchor(address(0));
    }

    function testVerifyInclusionAgainstReferenceVectors() public {
        vm.startPrank(coordinator);
        anchorC.anchor(1, ROOT_8, 8);
        anchorC.anchor(2, ROOT_5, 5);
        anchorC.anchor(3, ROOT_7, 7);
        vm.stopPrank();

        // 8 leaves, index 5
        bytes32[] memory p = new bytes32[](3);
        p[0] = 0xbc1a0643b12e4d2d7c77918f44e0f4f79a838b6cf9ec5b5c283e1f4d88599e6b;
        p[1] = 0xca854ea128ed050b41b35ffc1b87b8eb2bde461e9e3b5596ece6b9d5975a0ae0;
        p[2] = 0xd37ee418976dd95753c1c73862b9398fa2a2cf9b4ff0fdfe8b30cd95209614b7;
        bytes32 leaf5 = 0x4271a26be0d8a84f0bd54c8c302e7cb3a3b5d1fa6780a40bcce2873477dab658;
        assertTrue(anchorC.verifyInclusion(1, leaf5, 5, p), "8 leaves, index 5");

        // 8 leaves, index 0
        bytes32[] memory p0 = new bytes32[](3);
        p0[0] = 0x96a296d224f285c67bee93c30f8a309157f0daa35dc5b87e410b78630a09cfc7;
        p0[1] = 0x5f083f0a1a33ca076a95279832580db3e0ef4584bdff1f54c8a360f50de3031e;
        p0[2] = 0x6b47aaf29ee3c2af9af889bc1fb9254dabd31177f16232dd6aab035ca39bf6e4;
        bytes32 leaf0 = 0x6e340b9cffb37a989ca544e6bb780a2c78901d3fb33738768511a30617afa01d;
        assertTrue(anchorC.verifyInclusion(1, leaf0, 0, p0), "8 leaves, index 0");

        // 5 leaves, last index (a lone right-hand leaf)
        bytes32[] memory p4 = new bytes32[](1);
        p4[0] = 0xd37ee418976dd95753c1c73862b9398fa2a2cf9b4ff0fdfe8b30cd95209614b7;
        bytes32 leaf4 = 0xbc1a0643b12e4d2d7c77918f44e0f4f79a838b6cf9ec5b5c283e1f4d88599e6b;
        assertTrue(anchorC.verifyInclusion(2, leaf4, 4, p4), "5 leaves, index 4");

        // 7 leaves, last index
        bytes32[] memory p6 = new bytes32[](2);
        p6[0] = 0x0ebc5d3437fbe2db158b9f126a1d118e308181031d0a949f8dededebc558ef6a;
        p6[1] = 0xd37ee418976dd95753c1c73862b9398fa2a2cf9b4ff0fdfe8b30cd95209614b7;
        bytes32 leaf6 = 0xb08693ec2e721597130641e8211e7eedccb4c26413963eee6c1e2ed16ffb1a5f;
        assertTrue(anchorC.verifyInclusion(3, leaf6, 6, p6), "7 leaves, index 6");
    }

    function testVerifyInclusionRejectsTampering() public {
        vm.prank(coordinator);
        anchorC.anchor(1, ROOT_8, 8);

        bytes32[] memory p = new bytes32[](3);
        p[0] = 0xbc1a0643b12e4d2d7c77918f44e0f4f79a838b6cf9ec5b5c283e1f4d88599e6b;
        p[1] = 0xca854ea128ed050b41b35ffc1b87b8eb2bde461e9e3b5596ece6b9d5975a0ae0;
        p[2] = 0xd37ee418976dd95753c1c73862b9398fa2a2cf9b4ff0fdfe8b30cd95209614b7;
        bytes32 leaf5 = 0x4271a26be0d8a84f0bd54c8c302e7cb3a3b5d1fa6780a40bcce2873477dab658;

        // wrong leaf (a rewritten receipt)
        assertTrue(!anchorC.verifyInclusion(1, bytes32(uint256(leaf5) ^ 1), 5, p), "wrong leaf");
        // wrong index
        assertTrue(!anchorC.verifyInclusion(1, leaf5, 4, p), "wrong index");
        // index outside the tree
        assertTrue(!anchorC.verifyInclusion(1, leaf5, 8, p), "index out of range");
        // never anchored epoch
        assertTrue(!anchorC.verifyInclusion(2, leaf5, 5, p), "unknown epoch");
        // truncated proof
        bytes32[] memory short = new bytes32[](2);
        short[0] = p[0];
        short[1] = p[1];
        assertTrue(!anchorC.verifyInclusion(1, leaf5, 5, short), "truncated proof");
        // altered sibling
        bytes32[] memory bad = new bytes32[](3);
        bad[0] = p[0];
        bad[1] = bytes32(uint256(p[1]) ^ 1);
        bad[2] = p[2];
        assertTrue(!anchorC.verifyInclusion(1, leaf5, 5, bad), "altered sibling");
        // over-long proof
        bytes32[] memory long = new bytes32[](4);
        long[0] = p[0];
        long[1] = p[1];
        long[2] = p[2];
        long[3] = p[2];
        assertTrue(!anchorC.verifyInclusion(1, leaf5, 5, long), "over-long proof");
    }

    /// @dev The first anchor also initialises storage slots; later anchors are
    ///      the steady-state cost that matters for budgeting.
    function testSteadyStateAnchorGas() public {
        vm.startPrank(coordinator);
        anchorC.anchor(1, ROOT_8, 8);
        uint256 g = gasleft();
        anchorC.anchor(2, ROOT_5, 5);
        uint256 used = g - gasleft();
        vm.stopPrank();
        assertTrue(used < 85_000, "steady-state anchor should cost under 85k gas");
    }
}
