// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import {TestBase} from "./TestBase.sol";
import {VyomaEscrow} from "../src/VyomaEscrow.sol";
import {MockUSDT, FeeToken, ReentrantToken} from "./Mocks.sol";

contract VyomaEscrowTest is TestBase {
    MockUSDT internal usdt;
    VyomaEscrow internal escrow;

    address internal coordinator = address(0xC0DE);
    address internal guardian = address(0x6A12D);
    address internal stranger = address(0xBAD);
    address internal p1 = address(0x1001);
    address internal p2 = address(0x1002);
    address internal p3 = address(0x1003);

    uint256 internal constant ALICE_KEY = 0xA11CE;
    uint256 internal constant BOB_KEY = 0xB0B;
    address internal alice;
    address internal bob;

    uint64 internal constant WINDOW = 120;
    uint64 internal constant SILENCE = 900;
    uint256 internal constant T0 = 1_000_000;
    uint256 internal constant USDT = 1_000_000; // one whole token (6 decimals)

    bytes32 private constant PERMIT_TYPEHASH =
        keccak256("Permit(address owner,address spender,uint256 value,uint256 nonce,uint256 deadline)");

    function setUp() public {
        vm.warp(T0);
        alice = vm.addr(ALICE_KEY);
        bob = vm.addr(BOB_KEY);
        usdt = new MockUSDT();
        escrow = new VyomaEscrow(address(usdt), coordinator, guardian, WINDOW, SILENCE);
    }

    // ───────────────────────────── helpers ──────────────────────────────

    function _deposit(address owner, uint256 amount) internal {
        usdt.mint(owner, amount);
        vm.startPrank(owner);
        usdt.approve(address(escrow), amount);
        escrow.deposit(amount);
        vm.stopPrank();
    }

    function _one(address a) internal pure returns (address[] memory r) {
        r = new address[](1);
        r[0] = a;
    }

    function _one(uint256 a) internal pure returns (uint256[] memory r) {
        r = new uint256[](1);
        r[0] = a;
    }

    function _two(address a, address b) internal pure returns (address[] memory r) {
        r = new address[](2);
        r[0] = a;
        r[1] = b;
    }

    function _two(uint256 a, uint256 b) internal pure returns (uint256[] memory r) {
        r = new uint256[](2);
        r[0] = a;
        r[1] = b;
    }

    function _post(
        uint64 period,
        address[] memory owners,
        uint256[] memory debits,
        address[] memory accounts,
        uint256[] memory amounts
    ) internal {
        vm.prank(coordinator);
        escrow.postPeriod(period, bytes32(uint256(period)), owners, debits, accounts, amounts);
    }

    /// @dev One owner pays two providers: 100 / 60 / 40 USDT.
    function _standardPeriod(uint64 period) internal {
        _post(period, _one(alice), _one(100 * USDT), _two(p1, p2), _two(60 * USDT, 40 * USDT));
    }

    function _assertSolvent(string memory when_) internal view {
        assertEq(
            usdt.balanceOf(address(escrow)), escrow.totalCredit() + escrow.totalUnclaimed(), when_
        );
    }

    // ─────────────────────────── constructor ────────────────────────────

    function testConstructorValidation() public {
        vm.expectRevert(VyomaEscrow.ZeroAddress.selector);
        new VyomaEscrow(address(0), coordinator, guardian, WINDOW, SILENCE);
        vm.expectRevert(VyomaEscrow.ZeroAddress.selector);
        new VyomaEscrow(address(usdt), address(0), guardian, WINDOW, SILENCE);
        vm.expectRevert(VyomaEscrow.ZeroAddress.selector);
        new VyomaEscrow(address(usdt), coordinator, address(0), WINDOW, SILENCE);
        // the coordinator and the guardian must be different keys
        vm.expectRevert(VyomaEscrow.SameCoordinatorAndGuardian.selector);
        new VyomaEscrow(address(usdt), coordinator, coordinator, WINDOW, SILENCE);
        vm.expectRevert(VyomaEscrow.BadTimings.selector);
        new VyomaEscrow(address(usdt), coordinator, guardian, 0, SILENCE);
        vm.expectRevert(VyomaEscrow.BadTimings.selector);
        new VyomaEscrow(address(usdt), coordinator, guardian, WINDOW, WINDOW);
    }

    // ───────────────────────────── deposits ─────────────────────────────

    function testDepositCreditsTheOwner() public {
        _deposit(alice, 500 * USDT);
        assertEq(escrow.credit(alice), 500 * USDT, "alice credit");
        assertEq(escrow.totalCredit(), 500 * USDT, "total credit");
        assertEq(usdt.balanceOf(address(escrow)), 500 * USDT, "escrow balance");
        assertEq(usdt.balanceOf(alice), 0, "alice balance");
    }

    function testZeroDepositReverts() public {
        vm.prank(alice);
        vm.expectRevert(VyomaEscrow.ZeroAmount.selector);
        escrow.deposit(0);
    }

    function _permitSig(uint256 key, address owner, uint256 value, uint256 deadline)
        internal
        view
        returns (uint8 v, bytes32 r, bytes32 s)
    {
        bytes32 structHash =
            keccak256(abi.encode(PERMIT_TYPEHASH, owner, address(escrow), value, usdt.nonces(owner), deadline));
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", usdt.DOMAIN_SEPARATOR(), structHash));
        (v, r, s) = vm.sign(key, digest);
    }

    function testCoordinatorCanRelayAPermitDepositSoTheOwnerNeedsNoGas() public {
        usdt.mint(alice, 250 * USDT);
        (uint8 v, bytes32 r, bytes32 s) = _permitSig(ALICE_KEY, alice, 250 * USDT, T0 + 1 days);

        vm.prank(coordinator); // alice never sends a transaction
        escrow.depositWithPermit(alice, 250 * USDT, T0 + 1 days, v, r, s);

        assertEq(escrow.credit(alice), 250 * USDT, "credit");
        assertEq(usdt.balanceOf(alice), 0, "alice paid");
    }

    function testReplayedPermitCannotBlockTheDeposit() public {
        usdt.mint(alice, 250 * USDT);
        (uint8 v, bytes32 r, bytes32 s) = _permitSig(ALICE_KEY, alice, 250 * USDT, T0 + 1 days);

        // an attacker front-runs by submitting the signature to the token
        vm.prank(stranger);
        usdt.permit(alice, address(escrow), 250 * USDT, T0 + 1 days, v, r, s);

        vm.prank(coordinator);
        escrow.depositWithPermit(alice, 250 * USDT, T0 + 1 days, v, r, s);
        assertEq(escrow.credit(alice), 250 * USDT, "deposit still succeeds");
    }

    function testPermitDepositRejectsAStranger() public {
        usdt.mint(alice, 10 * USDT);
        (uint8 v, bytes32 r, bytes32 s) = _permitSig(ALICE_KEY, alice, 10 * USDT, T0 + 1 days);
        vm.prank(stranger);
        vm.expectRevert(VyomaEscrow.NotOwnerOrCoordinator.selector);
        escrow.depositWithPermit(alice, 10 * USDT, T0 + 1 days, v, r, s);
    }

    function testBadPermitWithoutAllowanceCannotPullFunds() public {
        usdt.mint(alice, 10 * USDT);
        // signed by the wrong key, and alice never approved the escrow
        (uint8 v, bytes32 r, bytes32 s) = _permitSig(BOB_KEY, alice, 10 * USDT, T0 + 1 days);
        vm.prank(coordinator);
        vm.expectRevert(VyomaEscrow.TransferFailed.selector);
        escrow.depositWithPermit(alice, 10 * USDT, T0 + 1 days, v, r, s);
        assertEq(usdt.balanceOf(alice), 10 * USDT, "alice funds untouched");
    }

    function testFeeOnTransferTokenCreditsOnlyWhatArrived() public {
        FeeToken fee = new FeeToken();
        VyomaEscrow e = new VyomaEscrow(address(fee), coordinator, guardian, WINDOW, SILENCE);
        fee.mint(alice, 1000 * USDT);
        vm.startPrank(alice);
        fee.approve(address(e), 1000 * USDT);
        e.deposit(1000 * USDT);
        vm.stopPrank();
        assertEq(e.credit(alice), 990 * USDT, "credit equals the amount received");
        assertEq(fee.balanceOf(address(e)), 990 * USDT, "balance");
    }

    // ───────────────────────────── posting ──────────────────────────────

    function testPostPeriodDebitsOwnersAndAllocates() public {
        _deposit(alice, 300 * USDT);
        _standardPeriod(1);

        assertEq(escrow.credit(alice), 200 * USDT, "alice debited");
        assertEq(escrow.totalCredit(), 200 * USDT, "total credit");
        assertEq(escrow.totalUnclaimed(), 100 * USDT, "unclaimed");
        assertEq(escrow.allocationOf(1, p1), 60 * USDT, "p1");
        assertEq(escrow.allocationOf(1, p2), 40 * USDT, "p2");
        (uint256 total, uint256 unclaimed, uint64 claimableAt, bool vetoed) = escrow.periodInfo(1);
        assertEq(total, 100 * USDT, "total");
        assertEq(unclaimed, 100 * USDT, "unclaimed");
        assertEq(claimableAt, T0 + WINDOW, "claimableAt");
        assertTrue(!vetoed, "not vetoed");
        assertEq(escrow.lastPeriod(), 1, "last period");
        _assertSolvent("after post");
    }

    /// @dev A stolen coordinator key tries to pay itself more than anyone deposited.
    function testStolenCoordinatorKeyCannotPostMoreThanCredit() public {
        _deposit(alice, 100 * USDT);
        _deposit(bob, 50 * USDT);

        vm.prank(coordinator);
        vm.expectRevert(
            abi.encodeWithSelector(VyomaEscrow.InsufficientCredit.selector, alice, 100 * USDT, 101 * USDT)
        );
        escrow.postPeriod(1, 0, _one(alice), _one(101 * USDT), _one(coordinator), _one(101 * USDT));

        // spreading the debit across owners does not help either
        vm.prank(coordinator);
        vm.expectRevert(
            abi.encodeWithSelector(VyomaEscrow.InsufficientCredit.selector, bob, 50 * USDT, 60 * USDT)
        );
        escrow.postPeriod(1, 0, _two(alice, bob), _two(100 * USDT, 60 * USDT), _one(coordinator), _one(160 * USDT));

        // an owner who deposited nothing cannot be debited at all
        vm.prank(coordinator);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.InsufficientCredit.selector, stranger, 0, 1));
        escrow.postPeriod(1, 0, _one(stranger), _one(1), _one(coordinator), _one(1));

        assertEq(escrow.credit(alice), 100 * USDT, "nothing moved");
        assertEq(escrow.totalUnclaimed(), 0, "nothing allocated");
    }

    function testPayoutsMustEqualDebits() public {
        _deposit(alice, 300 * USDT);
        vm.startPrank(coordinator);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.TotalMismatch.selector, 100 * USDT, 101 * USDT));
        escrow.postPeriod(1, 0, _one(alice), _one(100 * USDT), _one(p1), _one(101 * USDT));
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.TotalMismatch.selector, 100 * USDT, 99 * USDT));
        escrow.postPeriod(1, 0, _one(alice), _one(100 * USDT), _one(p1), _one(99 * USDT));
        vm.stopPrank();
    }

    function testPeriodsMustIncreaseAndCannotBeReposted() public {
        _deposit(alice, 500 * USDT);
        _standardPeriod(5);

        vm.startPrank(coordinator);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.PeriodNotIncreasing.selector, uint64(5), uint64(5)));
        escrow.postPeriod(5, 0, _one(alice), _one(10 * USDT), _one(p1), _one(10 * USDT));
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.PeriodNotIncreasing.selector, uint64(4), uint64(5)));
        escrow.postPeriod(4, 0, _one(alice), _one(10 * USDT), _one(p1), _one(10 * USDT));
        escrow.postPeriod(9, 0, _one(alice), _one(10 * USDT), _one(p1), _one(10 * USDT)); // gaps are fine
        vm.stopPrank();
    }

    function testOnlyCoordinatorCanPost() public {
        _deposit(alice, 100 * USDT);
        vm.prank(stranger);
        vm.expectRevert(VyomaEscrow.NotCoordinator.selector);
        escrow.postPeriod(1, 0, _one(alice), _one(10 * USDT), _one(stranger), _one(10 * USDT));
        vm.prank(guardian);
        vm.expectRevert(VyomaEscrow.NotCoordinator.selector);
        escrow.postPeriod(1, 0, _one(alice), _one(10 * USDT), _one(guardian), _one(10 * USDT));
    }

    function testPostRejectsMalformedLists() public {
        _deposit(alice, 100 * USDT);
        vm.startPrank(coordinator);
        address[] memory none = new address[](0);
        uint256[] memory noneU = new uint256[](0);
        vm.expectRevert(VyomaEscrow.BadLists.selector);
        escrow.postPeriod(1, 0, none, noneU, _one(p1), _one(1));
        vm.expectRevert(VyomaEscrow.BadLists.selector);
        escrow.postPeriod(1, 0, _one(alice), _one(1), none, noneU);
        vm.expectRevert(VyomaEscrow.BadLists.selector);
        escrow.postPeriod(1, 0, _one(alice), _two(1, 1), _one(p1), _one(2));
        vm.expectRevert(VyomaEscrow.ZeroAmount.selector);
        escrow.postPeriod(1, 0, _one(alice), _one(0), _one(p1), _one(0));
        vm.expectRevert(VyomaEscrow.ZeroAddress.selector);
        escrow.postPeriod(1, 0, _one(alice), _one(1), _one(address(0)), _one(1));
        vm.stopPrank();
    }

    // ────────────────────────────── claims ──────────────────────────────

    function testClaimBeforeTheWindowEndsReverts() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.warp(T0 + WINDOW - 1);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.WindowStillOpen.selector, uint64(1), uint64(T0 + WINDOW)));
        escrow.claim(1, p1);
    }

    function testClaimPaysTheAccountNotTheCaller() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.warp(T0 + WINDOW);

        vm.prank(stranger); // an attacker or a relayer submits the claim
        uint256 paid = escrow.claim(1, p1);

        assertEq(paid, 60 * USDT, "paid");
        assertEq(usdt.balanceOf(p1), 60 * USDT, "p1 received");
        assertEq(usdt.balanceOf(stranger), 0, "caller received nothing");
        assertEq(escrow.totalUnclaimed(), 40 * USDT, "unclaimed");
        _assertSolvent("after claim");
    }

    function testDoubleClaimReverts() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.warp(T0 + WINDOW);
        escrow.claim(1, p1);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.NothingToClaim.selector, uint64(1), p1));
        escrow.claim(1, p1);
        assertEq(usdt.balanceOf(p1), 60 * USDT, "paid exactly once");
    }

    function testAnAccountWithNoAllocationCannotClaim() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.warp(T0 + WINDOW);
        for (uint256 i = 0; i < 3; i++) {
            address who = i == 0 ? coordinator : (i == 1 ? guardian : stranger);
            vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.NothingToClaim.selector, uint64(1), who));
            escrow.claim(1, who);
        }
    }

    function testClaimManyPaysEveryoneAndSkipsThoseAlreadyPaid() public {
        _deposit(alice, 100 * USDT);
        _post(1, _one(alice), _one(100 * USDT), _two(p1, p2), _two(70 * USDT, 30 * USDT));
        vm.warp(T0 + WINDOW);
        escrow.claim(1, p1);

        address[] memory batch = new address[](3);
        batch[0] = p1; // already paid, skipped
        batch[1] = p2;
        batch[2] = p3; // never allocated, skipped
        uint256 paid = escrow.claimMany(1, batch);

        assertEq(paid, 30 * USDT, "batch paid");
        assertEq(usdt.balanceOf(p1), 70 * USDT, "p1 not paid twice");
        assertEq(usdt.balanceOf(p2), 30 * USDT, "p2");
        assertEq(usdt.balanceOf(address(escrow)), 0, "escrow empty");
        _assertSolvent("after batch");
    }

    function testClaimOfAnUnknownPeriodReverts() public {
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.UnknownPeriod.selector, uint64(7)));
        escrow.claim(7, p1);
    }

    function testTheSameAccountPaidTwiceInOnePeriodClaimsTheSum() public {
        _deposit(alice, 100 * USDT);
        _post(1, _one(alice), _one(100 * USDT), _two(p1, p1), _two(10 * USDT, 90 * USDT));
        vm.warp(T0 + WINDOW);
        assertEq(escrow.claim(1, p1), 100 * USDT, "sum");
    }

    // ─────────────────────────────── veto ───────────────────────────────

    function testGuardianVetoReturnsEveryDebitToItsOwner() public {
        _deposit(alice, 300 * USDT);
        _deposit(bob, 200 * USDT);
        _post(1, _two(alice, bob), _two(120 * USDT, 80 * USDT), _two(p1, p2), _two(150 * USDT, 50 * USDT));
        assertEq(escrow.credit(alice), 180 * USDT, "alice after post");
        assertEq(escrow.credit(bob), 120 * USDT, "bob after post");

        vm.warp(T0 + WINDOW - 1);
        vm.prank(guardian);
        escrow.vetoPeriod(1);

        assertEq(escrow.credit(alice), 300 * USDT, "alice restored");
        assertEq(escrow.credit(bob), 200 * USDT, "bob restored");
        assertEq(escrow.totalCredit(), 500 * USDT, "total credit restored");
        assertEq(escrow.totalUnclaimed(), 0, "nothing left to claim");
        _assertSolvent("after veto");
    }

    function testVetoedPeriodCanNeverBeClaimed() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.prank(guardian);
        escrow.vetoPeriod(1);
        vm.warp(T0 + 10 * WINDOW);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.PeriodIsVetoed.selector, uint64(1)));
        escrow.claim(1, p1);
    }

    function testVetoAfterTheWindowReverts() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.warp(T0 + WINDOW);
        vm.prank(guardian);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.WindowClosed.selector, uint64(1)));
        escrow.vetoPeriod(1);
    }

    function testOnlyTheGuardianCanVeto() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.prank(coordinator);
        vm.expectRevert(VyomaEscrow.NotGuardian.selector);
        escrow.vetoPeriod(1);
        vm.prank(stranger);
        vm.expectRevert(VyomaEscrow.NotGuardian.selector);
        escrow.vetoPeriod(1);
    }

    function testVetoTwiceAndVetoOfUnknownPeriodRevert() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.startPrank(guardian);
        escrow.vetoPeriod(1);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.AlreadyVetoed.selector, uint64(1)));
        escrow.vetoPeriod(1);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.UnknownPeriod.selector, uint64(2)));
        escrow.vetoPeriod(2);
        vm.stopPrank();
    }

    function testAVetoedPeriodNumberCannotBeReused() public {
        _deposit(alice, 200 * USDT);
        _standardPeriod(1);
        vm.prank(guardian);
        escrow.vetoPeriod(1);
        vm.prank(coordinator);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.PeriodNotIncreasing.selector, uint64(1), uint64(1)));
        escrow.postPeriod(1, 0, _one(alice), _one(10 * USDT), _one(p1), _one(10 * USDT));
    }

    // ───────────────────────────── refunds ──────────────────────────────

    function testRefundIsBlockedWhileTheCoordinatorIsActive() public {
        _deposit(alice, 100 * USDT);
        vm.warp(T0 + SILENCE - 1);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.CoordinatorStillActive.selector, T0 + SILENCE));
        escrow.refund(alice);

        // a fresh post resets the clock
        _standardPeriod(1);
        vm.warp(T0 + SILENCE + 100);
        vm.expectRevert(
            abi.encodeWithSelector(VyomaEscrow.CoordinatorStillActive.selector, T0 + SILENCE - 1 + SILENCE)
        );
        escrow.refund(alice);
    }

    function testRefundReturnsEachOwnersExactRemainingCredit() public {
        _deposit(alice, 700 * USDT);
        _deposit(bob, 300 * USDT);
        // alice is debited 200, bob 100; two providers earn 300 between them
        _post(1, _two(alice, bob), _two(200 * USDT, 100 * USDT), _two(p1, p2), _two(180 * USDT, 120 * USDT));

        vm.warp(block.timestamp + SILENCE);
        vm.prank(stranger); // anyone can submit; the money goes to the owner
        assertEq(escrow.refund(alice), 500 * USDT, "alice refund");
        assertEq(escrow.refund(bob), 200 * USDT, "bob refund");

        assertEq(usdt.balanceOf(alice), 500 * USDT, "alice received");
        assertEq(usdt.balanceOf(bob), 200 * USDT, "bob received");
        assertEq(usdt.balanceOf(stranger), 0, "caller received nothing");
        assertEq(escrow.totalCredit(), 0, "credit cleared");
        _assertSolvent("after refunds");

        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.NothingToRefund.selector, alice));
        escrow.refund(alice);
    }

    /// @dev The demo moment: the coordinator disappears after posting.
    function testPostedPeriodsStayClaimableAndOwnersAreRefundedWhenTheCoordinatorVanishes() public {
        _deposit(alice, 1000 * USDT);
        _standardPeriod(1); // 100 USDT to p1/p2, 900 left as credit
        // coordinator is never heard from again

        vm.warp(block.timestamp + SILENCE + 1 days);

        // providers are paid by a bystander, with no coordinator involved
        vm.prank(stranger);
        escrow.claim(1, p1);
        vm.prank(stranger);
        escrow.claim(1, p2);
        assertEq(usdt.balanceOf(p1), 60 * USDT, "p1 paid");
        assertEq(usdt.balanceOf(p2), 40 * USDT, "p2 paid");

        // and the owner recovers the unspent credit
        escrow.refund(alice);
        assertEq(usdt.balanceOf(alice), 900 * USDT, "owner refunded");
        assertEq(usdt.balanceOf(address(escrow)), 0, "escrow empty, nothing stranded");
    }

    function testRefundNeverTouchesFundsAlreadyAllocatedToProviders() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1); // all 100 allocated, credit 0
        vm.warp(block.timestamp + SILENCE);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.NothingToRefund.selector, alice));
        escrow.refund(alice);
        assertEq(usdt.balanceOf(address(escrow)), 100 * USDT, "allocation intact");
    }

    // ───────────────────────── no backdoors, safety ─────────────────────

    function testNeitherPrivilegedKeyCanTakeFundsByTheirOwnRole() public {
        _deposit(alice, 100 * USDT);
        _standardPeriod(1);
        vm.warp(T0 + WINDOW);

        vm.startPrank(coordinator);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.NothingToClaim.selector, uint64(1), coordinator));
        escrow.claim(1, coordinator);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.CoordinatorStillActive.selector, T0 + SILENCE));
        escrow.refund(alice);
        vm.stopPrank();

        vm.startPrank(guardian);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.NothingToClaim.selector, uint64(1), guardian));
        escrow.claim(1, guardian);
        vm.stopPrank();
    }

    function testReentrancyDuringAClaimIsBlocked() public {
        ReentrantToken evil = new ReentrantToken();
        VyomaEscrow e = new VyomaEscrow(address(evil), coordinator, guardian, WINDOW, SILENCE);
        evil.mint(alice, 100 * USDT);
        vm.startPrank(alice);
        evil.approve(address(e), 100 * USDT);
        e.deposit(100 * USDT);
        vm.stopPrank();

        vm.prank(coordinator);
        e.postPeriod(1, 0, _one(alice), _one(100 * USDT), _one(p1), _one(100 * USDT));
        vm.warp(T0 + WINDOW);

        evil.arm(address(e), 1, p1);
        e.claim(1, p1);

        assertTrue(evil.reentered(), "the token did try to re-enter");
        assertTrue(evil.reentryBlocked(), "the re-entrant claim was rejected");
        assertEq(evil.balanceOf(p1), 100 * USDT, "paid exactly once");
        assertEq(evil.balanceOf(address(e)), 0, "nothing extra left the escrow");
    }

    function testSolvencyHoldsAtEveryStepOfALongerScenario() public {
        _deposit(alice, 400 * USDT);
        _assertSolvent("1 deposit");
        _deposit(bob, 600 * USDT);
        _assertSolvent("2 deposit");
        _post(1, _two(alice, bob), _two(100 * USDT, 150 * USDT), _two(p1, p2), _two(200 * USDT, 50 * USDT));
        _assertSolvent("3 post");
        vm.warp(block.timestamp + 10);
        vm.prank(guardian);
        escrow.vetoPeriod(1);
        _assertSolvent("4 veto");
        _post(2, _one(alice), _one(40 * USDT), _one(p3), _one(40 * USDT));
        _assertSolvent("5 post");
        vm.warp(block.timestamp + WINDOW);
        escrow.claim(2, p3);
        _assertSolvent("6 claim");
        _deposit(alice, 10 * USDT);
        _assertSolvent("7 deposit");
        vm.warp(block.timestamp + SILENCE);
        escrow.refund(alice);
        escrow.refund(bob);
        _assertSolvent("8 refunds");
        assertEq(usdt.balanceOf(address(escrow)), 0, "escrow drained to exactly zero");
        assertEq(usdt.balanceOf(p3), 40 * USDT, "p3");
        assertEq(usdt.balanceOf(alice), 370 * USDT, "alice");
        assertEq(usdt.balanceOf(bob), 600 * USDT, "bob");
    }

    // ─────────────────────────────── fuzz ───────────────────────────────

    /// @dev Whatever the amounts, claims plus refunds return exactly what went in.
    function testFuzzConservation(uint96 depA, uint96 depB, uint96 debitSeed, uint96 splitSeed) public {
        uint256 a = uint256(depA) % (1e12) + 2;
        uint256 b = uint256(depB) % (1e12) + 2;
        _deposit(alice, a);
        _deposit(bob, b);

        uint256 dA = uint256(debitSeed) % a + 1; // 1..a
        uint256 dB = uint256(debitSeed >> 3) % b + 1; // 1..b
        uint256 total = dA + dB;
        uint256 first = uint256(splitSeed) % (total - 1) + 1; // 1..total-1
        _post(1, _two(alice, bob), _two(dA, dB), _two(p1, p2), _two(first, total - first));
        _assertSolvent("after post");

        vm.warp(T0 + WINDOW);
        address[] memory both = _two(p1, p2);
        assertEq(escrow.claimMany(1, both), total, "claimed everything posted");
        assertEq(usdt.balanceOf(p1) + usdt.balanceOf(p2), total, "providers received the total");
        _assertSolvent("after claims");

        vm.warp(block.timestamp + SILENCE);
        _refundOrExpectNothing(alice, a - dA);
        _refundOrExpectNothing(bob, b - dB);
        assertEq(usdt.balanceOf(alice), a - dA, "alice got her remainder");
        assertEq(usdt.balanceOf(bob), b - dB, "bob got his remainder");
        assertEq(usdt.balanceOf(address(escrow)), 0, "nothing stranded or created");
    }

    /// @dev An owner debited to exactly zero has nothing to refund.
    function _refundOrExpectNothing(address owner, uint256 remainder) internal {
        if (remainder == 0) {
            vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.NothingToRefund.selector, owner));
            escrow.refund(owner);
        } else {
            assertEq(escrow.refund(owner), remainder, "refund equals remainder");
        }
    }

    /// @dev No sequence of the coordinator's calls can create a claim larger
    ///      than the credit that was deposited.
    function testFuzzCoordinatorCannotOverspend(uint96 depA, uint96 attempt) public {
        uint256 a = uint256(depA) % 1e12 + 1;
        _deposit(alice, a);
        uint256 ask = a + 1 + (uint256(attempt) % 1e12);
        vm.prank(coordinator);
        vm.expectRevert(abi.encodeWithSelector(VyomaEscrow.InsufficientCredit.selector, alice, a, ask));
        escrow.postPeriod(1, 0, _one(alice), _one(ask), _one(coordinator), _one(ask));
    }
}
