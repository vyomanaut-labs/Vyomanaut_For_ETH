// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import {IERC20Permit} from "../src/interfaces/IERC20Permit.sol";

/// @dev A 6-decimal ERC-20 with EIP-2612 permit, standing in for the real
///      stablecoin in local tests only. Deployments use the real token.
contract MockUSDT is IERC20Permit {
    string public constant name = "Mock USDT";
    string public constant version = "1";
    uint8 public constant decimals = 6;

    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;
    mapping(address => uint256) public nonces;

    bytes32 private constant PERMIT_TYPEHASH =
        keccak256("Permit(address owner,address spender,uint256 value,uint256 nonce,uint256 deadline)");

    function mint(address to, uint256 amount) external {
        balanceOf[to] += amount;
    }

    function approve(address spender, uint256 amount) external returns (bool) {
        allowance[msg.sender][spender] = amount;
        return true;
    }

    function transfer(address to, uint256 amount) external virtual returns (bool) {
        _move(msg.sender, to, amount);
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) external virtual returns (bool) {
        uint256 a = allowance[from][msg.sender];
        require(a >= amount, "allowance");
        if (a != type(uint256).max) allowance[from][msg.sender] = a - amount;
        _move(from, to, amount);
        return true;
    }

    function _move(address from, address to, uint256 amount) internal virtual {
        require(balanceOf[from] >= amount, "balance");
        balanceOf[from] -= amount;
        balanceOf[to] += amount;
    }

    function DOMAIN_SEPARATOR() public view returns (bytes32) {
        return keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
                keccak256(bytes(name)),
                keccak256(bytes(version)),
                block.chainid,
                address(this)
            )
        );
    }

    function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s)
        external
    {
        require(block.timestamp <= deadline, "permit expired");
        bytes32 structHash = keccak256(abi.encode(PERMIT_TYPEHASH, owner, spender, value, nonces[owner]++, deadline));
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", DOMAIN_SEPARATOR(), structHash));
        address signer = ecrecover(digest, v, r, s);
        require(signer != address(0) && signer == owner, "bad permit");
        allowance[owner][spender] = value;
    }
}

/// @dev Skims 1% of every transfer, like a fee-on-transfer token.
contract FeeToken is MockUSDT {
    uint256 private constant FEE_DENOMINATOR = 100;

    function _move(address from, address to, uint256 amount) internal override {
        require(balanceOf[from] >= amount, "balance");
        uint256 fee = amount / FEE_DENOMINATOR;
        balanceOf[from] -= amount;
        balanceOf[to] += amount - fee;
    }
}

interface IClaimer {
    function claim(uint64 period, address account) external returns (uint256);
}

/// @dev On the first outgoing transfer it calls back into the escrow, the way
///      a token with transfer hooks could.
contract ReentrantToken is MockUSDT {
    address public target;
    uint64 public period;
    address public account;
    bool public reentered;
    bool public reentryBlocked;

    function arm(address target_, uint64 period_, address account_) external {
        target = target_;
        period = period_;
        account = account_;
    }

    function transfer(address to, uint256 amount) external override returns (bool) {
        if (target != address(0) && !reentered) {
            reentered = true;
            try IClaimer(target).claim(period, account) {} catch {
                reentryBlocked = true;
            }
        }
        _move(msg.sender, to, amount);
        return true;
    }
}
