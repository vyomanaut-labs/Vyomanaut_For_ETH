// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import {IERC20Permit} from "./interfaces/IERC20Permit.sol";

/// @title VyomaEscrow
/// @notice Stablecoin escrow that pays storage providers from proof, so a
///         posted payout survives the coordinator going away.
///
/// Money moves in four steps.
///   1. A data owner deposits stablecoin and holds a per-owner credit.
///   2. Each billing period the coordinator posts a payout list: which owners
///      are debited, and which accounts earn how much. The debits must equal
///      the payouts and no owner can be debited past their credit.
///   3. A challenge window follows. During it the guardian (a different key)
///      may veto the whole period, which returns every debit to its owner.
///   4. After the window anyone may call `claim`; the funds always go to the
///      account named in the payout, never to the caller. The coordinator
///      normally submits claims and pays the gas, so providers need no gas.
///
/// If the coordinator stops posting for `silenceTimeout`, anyone may return an
/// owner's remaining credit to that owner with `refund`. Periods that were
/// already posted stay claimable.
///
/// There is no admin withdrawal and no upgrade path. The coordinator can only
/// post periods bounded by owners' credits; the guardian can only veto a
/// period inside its window.
///
/// @dev The payout list is stored on chain, which keeps claims trivial and
///      needs no off-chain proof data. Posting costs gas linear in the number
///      of accounts paid, which suits tens to low hundreds of providers per
///      period; a very large network would replace the list with a Merkle root
///      behind the same claim interface.
contract VyomaEscrow {
    struct Period {
        uint256 total;
        uint256 unclaimed;
        uint64 claimableAt;
        bool vetoed;
        address[] owners;
        uint256[] debits;
    }

    IERC20Permit public immutable token;
    address public immutable coordinator;
    address public immutable guardian;
    /// @notice Seconds between a post and the first claim.
    uint64 public immutable challengeWindow;
    /// @notice Seconds without a post after which owners may be refunded.
    uint64 public immutable silenceTimeout;

    /// @notice Each owner's credit: deposited and not yet debited by a period.
    mapping(address owner => uint256) public credit;
    /// @notice Sum of all owners' credit.
    uint256 public totalCredit;
    /// @notice Sum of posted, unvetoed, not yet claimed payouts.
    uint256 public totalUnclaimed;
    /// @notice Highest period number posted so far.
    uint64 public lastPeriod;
    /// @notice Time of the last post (deployment time before the first).
    uint64 public lastPostAt;

    mapping(uint64 period => Period) private _periods;
    mapping(uint64 period => mapping(address account => uint256)) private _allocation;

    uint256 private _lock = 1;

    event Deposited(address indexed owner, uint256 amount);
    event PeriodPosted(uint64 indexed period, bytes32 ref, uint256 total, uint64 claimableAt);
    event Allocated(uint64 indexed period, address indexed account, uint256 amount);
    event PeriodVetoed(uint64 indexed period, uint256 restored);
    event Claimed(uint64 indexed period, address indexed account, uint256 amount, address indexed caller);
    event Refunded(address indexed owner, uint256 amount, address indexed caller);

    error ZeroAddress();
    error SameCoordinatorAndGuardian();
    error BadTimings();
    error NotCoordinator();
    error NotGuardian();
    error NotOwnerOrCoordinator();
    error Reentrancy();
    error ZeroAmount();
    error NothingReceived();
    error TransferFailed();
    error BadLists();
    error PeriodNotIncreasing(uint64 period, uint64 lastPosted);
    error InsufficientCredit(address owner, uint256 credit, uint256 debit);
    error TotalMismatch(uint256 debited, uint256 paidOut);
    error UnknownPeriod(uint64 period);
    error AlreadyVetoed(uint64 period);
    error PeriodIsVetoed(uint64 period);
    error WindowStillOpen(uint64 period, uint64 claimableAt);
    error WindowClosed(uint64 period);
    error NothingToClaim(uint64 period, address account);
    error CoordinatorStillActive(uint256 refundableAt);
    error NothingToRefund(address owner);

    modifier nonReentrant() {
        if (_lock != 1) revert Reentrancy();
        _lock = 2;
        _;
        _lock = 1;
    }

    constructor(
        address token_,
        address coordinator_,
        address guardian_,
        uint64 challengeWindow_,
        uint64 silenceTimeout_
    ) {
        if (token_ == address(0) || coordinator_ == address(0) || guardian_ == address(0)) revert ZeroAddress();
        if (coordinator_ == guardian_) revert SameCoordinatorAndGuardian();
        if (challengeWindow_ == 0 || silenceTimeout_ <= challengeWindow_) revert BadTimings();
        token = IERC20Permit(token_);
        coordinator = coordinator_;
        guardian = guardian_;
        challengeWindow = challengeWindow_;
        silenceTimeout = silenceTimeout_;
        lastPostAt = uint64(block.timestamp);
    }

    // ───────────────────────────── deposits ─────────────────────────────

    /// @notice Deposit after approving this contract. Credit is what actually
    ///         arrived, so a token that skims a fee cannot over-credit.
    function deposit(uint256 amount) external nonReentrant {
        _pull(msg.sender, amount);
    }

    /// @notice Deposit with an EIP-2612 permit signed by `owner`, so the owner
    ///         needs no gas. Only the owner or the coordinator may submit it.
    /// @dev A failed permit is ignored when the allowance already covers the
    ///      amount, so someone replaying the signature first cannot block it.
    function depositWithPermit(address owner, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s)
        external
        nonReentrant
    {
        if (msg.sender != owner && msg.sender != coordinator) revert NotOwnerOrCoordinator();
        try token.permit(owner, address(this), amount, deadline, v, r, s) {} catch {}
        _pull(owner, amount);
    }

    function _pull(address from, uint256 amount) private {
        if (amount == 0) revert ZeroAmount();
        uint256 balanceBefore = token.balanceOf(address(this));
        _safeTransferFrom(from, address(this), amount);
        uint256 received = token.balanceOf(address(this)) - balanceBefore;
        if (received == 0) revert NothingReceived();
        credit[from] += received;
        totalCredit += received;
        emit Deposited(from, received);
    }

    // ───────────────────────────── periods ──────────────────────────────

    /// @notice Post one billing period. Coordinator only.
    /// @param period   Strictly greater than every earlier period.
    /// @param ref      Free reference, for example the audit anchor epoch the
    ///                 payouts were computed from. Emitted, not interpreted.
    /// @param owners   Owners to debit, with `debits` in the same order.
    /// @param accounts Accounts to pay, with `amounts` in the same order.
    function postPeriod(
        uint64 period,
        bytes32 ref,
        address[] calldata owners,
        uint256[] calldata debits,
        address[] calldata accounts,
        uint256[] calldata amounts
    ) external {
        if (msg.sender != coordinator) revert NotCoordinator();
        if (period <= lastPeriod) revert PeriodNotIncreasing(period, lastPeriod);
        if (owners.length == 0 || owners.length != debits.length || accounts.length == 0
            || accounts.length != amounts.length) revert BadLists();

        uint256 debited = _debitOwners(period, owners, debits);
        uint256 paidOut = _allocate(period, accounts, amounts);
        if (paidOut != debited) revert TotalMismatch(debited, paidOut);

        Period storage p = _periods[period];
        uint64 claimableAt = uint64(block.timestamp) + challengeWindow;
        p.total = debited;
        p.unclaimed = debited;
        p.claimableAt = claimableAt;
        totalCredit -= debited;
        totalUnclaimed += debited;
        lastPeriod = period;
        lastPostAt = uint64(block.timestamp);
        emit PeriodPosted(period, ref, debited, claimableAt);
    }

    /// @dev Debits each owner's credit and records the debit so a veto can
    ///      restore it. Reverts if any owner would go below zero.
    function _debitOwners(uint64 period, address[] calldata owners, uint256[] calldata debits)
        private
        returns (uint256 debited)
    {
        Period storage p = _periods[period];
        for (uint256 i = 0; i < owners.length; i++) {
            uint256 d = debits[i];
            if (d == 0) revert ZeroAmount();
            uint256 c = credit[owners[i]];
            if (d > c) revert InsufficientCredit(owners[i], c, d);
            credit[owners[i]] = c - d;
            debited += d;
            p.owners.push(owners[i]);
            p.debits.push(d);
        }
    }

    /// @dev Records what each account may claim for the period.
    function _allocate(uint64 period, address[] calldata accounts, uint256[] calldata amounts)
        private
        returns (uint256 paidOut)
    {
        for (uint256 j = 0; j < accounts.length; j++) {
            uint256 a = amounts[j];
            if (a == 0) revert ZeroAmount();
            if (accounts[j] == address(0)) revert ZeroAddress();
            _allocation[period][accounts[j]] += a;
            paidOut += a;
            emit Allocated(period, accounts[j], a);
        }
    }

    /// @notice Cancel a period during its challenge window. Guardian only.
    ///         Every debit returns to its owner's credit.
    function vetoPeriod(uint64 period) external {
        if (msg.sender != guardian) revert NotGuardian();
        Period storage p = _periods[period];
        if (p.total == 0) revert UnknownPeriod(period);
        if (p.vetoed) revert AlreadyVetoed(period);
        if (block.timestamp >= p.claimableAt) revert WindowClosed(period);

        p.vetoed = true;
        for (uint256 i = 0; i < p.owners.length; i++) {
            credit[p.owners[i]] += p.debits[i];
        }
        totalCredit += p.total;
        totalUnclaimed -= p.unclaimed;
        p.unclaimed = 0;
        emit PeriodVetoed(period, p.total);
    }

    // ───────────────────────────── claims ───────────────────────────────

    /// @notice Pay `account` its allocation for `period`. Anyone may call; the
    ///         funds go to `account`, never to the caller.
    function claim(uint64 period, address account) external nonReentrant returns (uint256 paid) {
        paid = _claim(period, account);
        if (paid == 0) revert NothingToClaim(period, account);
    }

    /// @notice Pay several accounts in one transaction. Accounts with nothing
    ///         left to claim are skipped rather than reverting the batch.
    function claimMany(uint64 period, address[] calldata accounts) external nonReentrant returns (uint256 paidTotal) {
        for (uint256 i = 0; i < accounts.length; i++) {
            paidTotal += _claim(period, accounts[i]);
        }
    }

    function _claim(uint64 period, address account) private returns (uint256 amount) {
        Period storage p = _periods[period];
        if (p.total == 0) revert UnknownPeriod(period);
        if (p.vetoed) revert PeriodIsVetoed(period);
        if (block.timestamp < p.claimableAt) revert WindowStillOpen(period, p.claimableAt);

        amount = _allocation[period][account];
        if (amount == 0) return 0;
        _allocation[period][account] = 0;
        p.unclaimed -= amount;
        totalUnclaimed -= amount;
        _safeTransfer(account, amount);
        emit Claimed(period, account, amount, msg.sender);
    }

    // ───────────────────────────── refunds ──────────────────────────────

    /// @notice Return `owner`'s remaining credit to `owner` once the
    ///         coordinator has been silent for `silenceTimeout`. Anyone may
    ///         call, so an owner does not need gas.
    function refund(address owner) external nonReentrant returns (uint256 amount) {
        uint256 refundableAt = uint256(lastPostAt) + silenceTimeout;
        if (block.timestamp < refundableAt) revert CoordinatorStillActive(refundableAt);
        amount = credit[owner];
        if (amount == 0) revert NothingToRefund(owner);
        credit[owner] = 0;
        totalCredit -= amount;
        _safeTransfer(owner, amount);
        emit Refunded(owner, amount, msg.sender);
    }

    // ────────────────────────────── views ───────────────────────────────

    function periodInfo(uint64 period)
        external
        view
        returns (uint256 total, uint256 unclaimed, uint64 claimableAt, bool vetoed)
    {
        Period storage p = _periods[period];
        return (p.total, p.unclaimed, p.claimableAt, p.vetoed);
    }

    function periodDebits(uint64 period) external view returns (address[] memory owners, uint256[] memory debits) {
        Period storage p = _periods[period];
        return (p.owners, p.debits);
    }

    function allocationOf(uint64 period, address account) external view returns (uint256) {
        return _allocation[period][account];
    }

    /// @notice When `refund` becomes available if no further period is posted.
    function refundableAt() external view returns (uint256) {
        return uint256(lastPostAt) + silenceTimeout;
    }

    // ─────────────────────────── token helpers ──────────────────────────

    function _safeTransfer(address to, uint256 amount) private {
        (bool ok, bytes memory data) = address(token).call(abi.encodeCall(IERC20Permit.transfer, (to, amount)));
        if (!ok || (data.length != 0 && !abi.decode(data, (bool)))) revert TransferFailed();
    }

    function _safeTransferFrom(address from, address to, uint256 amount) private {
        (bool ok, bytes memory data) =
            address(token).call(abi.encodeCall(IERC20Permit.transferFrom, (from, to, amount)));
        if (!ok || (data.length != 0 && !abi.decode(data, (bool)))) revert TransferFailed();
    }
}
