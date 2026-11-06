// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import {AuditAnchor} from "../src/AuditAnchor.sol";
import {VyomaEscrow} from "../src/VyomaEscrow.sol";

interface ScriptVm {
    function startBroadcast() external;
    function stopBroadcast() external;
    function envAddress(string calldata name) external view returns (address);
    function envOr(string calldata name, uint256 defaultValue) external view returns (uint256);
}

/// @notice Deploys both contracts. The signer comes from the command line
///         (`--private-key` or `--account`), never from the environment.
///
///   COORDINATOR_ADDR          address allowed to anchor and post periods
///   GUARDIAN_ADDR             address allowed to veto (must differ)
///   CHALLENGE_WINDOW_SECONDS  default 120
///   SILENCE_TIMEOUT_SECONDS   default 900
contract Deploy {
    ScriptVm private constant vm = ScriptVm(address(uint160(uint256(keccak256("hevm cheat code")))));

    /// @dev USDT on Whitechain Sepolia: the only token that counts for the prize.
    address public constant USDT = 0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7;

    function run() external returns (AuditAnchor anchorContract, VyomaEscrow escrowContract) {
        address coordinator = vm.envAddress("COORDINATOR_ADDR");
        address guardian = vm.envAddress("GUARDIAN_ADDR");
        uint64 window = uint64(vm.envOr("CHALLENGE_WINDOW_SECONDS", uint256(120)));
        uint64 silence = uint64(vm.envOr("SILENCE_TIMEOUT_SECONDS", uint256(900)));

        vm.startBroadcast();
        anchorContract = new AuditAnchor(coordinator);
        escrowContract = new VyomaEscrow(USDT, coordinator, guardian, window, silence);
        vm.stopBroadcast();
    }
}
