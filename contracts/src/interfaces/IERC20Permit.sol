// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @notice The slice of ERC-20 plus EIP-2612 that the escrow uses.
interface IERC20Permit {
    function balanceOf(address account) external view returns (uint256);
    function allowance(address owner, address spender) external view returns (uint256);
    function transfer(address to, uint256 amount) external returns (bool);
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s)
        external;
}
