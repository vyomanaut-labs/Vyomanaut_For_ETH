// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @dev The few Foundry cheat codes the tests use. Declared locally so the
///      repository needs no vendored library.
interface Vm {
    function prank(address caller) external;
    function startPrank(address caller) external;
    function stopPrank() external;
    function warp(uint256 timestamp) external;
    function expectRevert(bytes4 selector) external;
    function expectRevert(bytes calldata revertData) external;
    function expectRevert() external;
    function addr(uint256 privateKey) external pure returns (address);
    function sign(uint256 privateKey, bytes32 digest) external pure returns (uint8 v, bytes32 r, bytes32 s);
}

abstract contract TestBase {
    Vm internal constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    function assertEq(uint256 a, uint256 b, string memory why) internal pure {
        require(a == b, why);
    }

    function assertEq(address a, address b, string memory why) internal pure {
        require(a == b, why);
    }

    function assertTrue(bool cond, string memory why) internal pure {
        require(cond, why);
    }
}
