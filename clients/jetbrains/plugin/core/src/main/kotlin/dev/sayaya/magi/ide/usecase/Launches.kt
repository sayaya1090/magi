package dev.sayaya.magi.ide.usecase

/**
 * 자동 기동을 언제 허락하는가 — **한 워크스페이스의 예산**.
 *
 * 옛 `Restarts` 를 대신하고 그것은 지웠다. 둘 다 「폭주를 막는다」를 하려 했는데 규칙이 달랐다: 이쪽 판은
 * 「평생 3회, 60초 간격」이었고 VS Code 는 「60초 이동 구간에 3회」였다(2026-09-11 실측).
 * 둘 다 말이 되고 **같은 규칙이 아니다**, 그게 문제다. 이제 계약이 한 곳에 있다 —
 * `clients/contract/lifecycle-policy.json`, 그리고 `docs/CLIENT_LIFECYCLE` §5.
 *
 * **시계를 안 읽는다.** 모든 판정이 부르는 쪽이 준 [now] 로만 이뤄지므로 가상 시계로 전부
 * 잴 수 있다 — 예산이 60초짜리라 실제로 기다리며 재는 시험은 쓸 수가 없고, 안 재는 시험은
 * 없는 것과 같다.
 *
 * **두 셈이 따로다.** 이동 구간의 기동 수와 **연속 실패** 수는 다른 것을 센다. 앞의 것은
 * 「지금 너무 자주 띄우고 있다」(시간이 지우면 돌아온다), 뒤의 것은 「띄워 봐야 안 산다」
 * (시간이 지나도 안 지워진다 — 그러지 않으면 크래시 루프를 1분마다 용서하게 된다).
 */
class Launches(
    private val windowMs: Long = 60_000,
    private val spawnsPerWindow: Int = 3,
    private val failuresToBlock: Int = 3,
    /**
     * 끊긴 뒤 새 프로세스를 안 띄우는 시간. **부르는 쪽이 실제로 기다리는 시간과 같은 값이어야
     * 한다** — 그래서 `private` 이 아니다. 화면 쪽이 제 상수로 따로 5초를 적고 있었고, 둘 중
     * 하나만 고치는 날이 이 계약이 막으려는 그 날이다.
     */
    val graceMs: Long = 5_000,
    private val stableMs: Long = 60_000,
) {
    /** 왜 안 되는지까지 말한다 — 「안 됨」 하나로 접으면 화면이 사람에게 할 말이 없다. */
    enum class Verdict { Allow, Budget, Grace, Blocked }

    private val spawns = ArrayDeque<Long>()
    private var failures = 0
    private var lost: Long? = null
    private var stoppedByUser = false

    /**
     * 붙은 시각. **끊김이 실패인지 아닌지를 이것이 가른다.**
     *
     * 계약이 그렇게 적는다 — 「준비 후 60초 안정 구간 **이전의** 예상치 못한 종료를 연속 실패로
     * 계산한다」(`docs/CLIENT_LIFECYCLE` §5). 안 세면 떴다가 2초 만에 죽기를 되풀이하는 데몬이
     * 영원히 재시도된다: 준비 시한은 넘긴 적이 없으니 [failed] 로는 한 번도 안 세인다.
     * 계약의 「a momentary connection does not forgive a failure」가 이 자리를 잡았다.
     */
    private var readyAt: Long? = null

    /**
     * 지금 띄워도 되는가.
     *
     * [manual] 은 사람이 직접 시킨 것이다 — **예산으로 거절하지 않는다.** 예산은 아무도 안 시킨
     * 루프를 막으려고 있는 것이고, 사람이 시킨 것은 시킨 사람이 있다. 막힌 상태도 이것으로 풀린다.
     */
    @Synchronized
    fun may(now: Long, manual: Boolean = false): Verdict {
        if (manual) {
            clear()
            return Verdict.Allow
        }
        if (stoppedByUser || failures >= failuresToBlock) return Verdict.Blocked
        lost?.let { if (now - it < graceMs) return Verdict.Grace }
        trim(now)
        if (spawns.size >= spawnsPerWindow) return Verdict.Budget
        return Verdict.Allow
    }

    /** 실제로 프로세스를 띄웠다. 이동 구간이 세는 것은 **띄운 것**이지 시도한 것이 아니다. */
    @Synchronized
    fun spawned(now: Long) {
        trim(now)
        spawns.addLast(now)
        lost = null
    }

    /** 붙었다. **아직 아무것도 용서하지 않는다** — 그 판정은 [stable] 이 한다. */
    @Synchronized
    fun ready(now: Long) {
        lost = null
        readyAt = now
    }

    /**
     * 붙은 채로 [stableMs] 를 넘겼다. 여기서만 예산과 연속 실패가 지워진다.
     *
     * 붙자마자 지우면 **떴다가 2초 만에 죽는 데몬**이 매번 용서받아 영원히 재시도된다 —
     * 계약의 「순간 handshake 성공으로 초기화하지 않는다」가 그 자리다.
     */
    @Synchronized
    fun stable(now: Long) {
        clear()
    }

    /**
     * 폴이 「붙어 있다」를 보았다. 처음이면 [ready], 안정 구간을 넘겼으면 [stable].
     *
     * 클라이언트는 사건이 아니라 **폴**로 연결을 안다 — 그래서 「얼마나 붙어 있었나」의 판정을
     * 화면 쪽에 맡기면 두 편집기가 각자 제 「안정」을 지어낸다. 정책이 스스로 정한다.
     */
    @Synchronized
    fun connected(now: Long) {
        val since = readyAt
        if (since == null) ready(now) else if (now - since >= stableMs) stable(now)
    }

    /** 끊겼다. 유예가 여기서 시작한다 — 죽어 가는 데몬과 새 데몬이 소켓을 두고 다투지 않게. */
    @Synchronized
    fun lost(now: Long) {
        // 안정 구간 전에 죽은 것은 실패다. 오래 살다 죽은 것은 아니다 — 그건 크래시 루프가 아니고,
        // [stable] 이 이미 셈을 지운 뒤다.
        readyAt?.let { if (now - it < stableMs) failures++ }
        readyAt = null
        lost = now
    }

    /** 준비 시한을 넘겼거나, 안정 구간 전에 예상치 못하게 죽었다. */
    @Synchronized
    fun failed(now: Long) {
        failures++
        readyAt = null
        lost = now
    }

    /** 사람이 시킨 갱신 교체. **실패가 아니다** — 프로세스가 바뀐 것은 맞지만 아무것도 안 깨졌다. */
    @Synchronized
    fun replaced(now: Long) {
        lost = null
        // 교체는 프로세스가 바뀐 것이지 죽은 것이 아니다. 시계를 다시 세워야 다음 [lost] 가 이
        // 교체를 「방금 떴다 죽었다」로 세지 않는다.
        readyAt = now
    }

    /** 그 교체가 실패했다. 이건 실패로 센다 — 사람이 시킨 것은 교체지 실패가 아니다. */
    @Synchronized
    fun replaceFailed(now: Long) = failed(now)

    /** 사람이 이 컴패니언을 끝냈다. 되살리는 것은 그 사람과 말다툼하는 일이다. */
    @Synchronized
    fun userStopped(now: Long) {
        stoppedByUser = true
    }

    private fun clear() {
        spawns.clear()
        failures = 0
        lost = null
        readyAt = null
        stoppedByUser = false
    }

    private fun trim(now: Long) {
        while (spawns.isNotEmpty() && now - spawns.first() >= windowMs) spawns.removeFirst()
    }
}

/**
 * 다시 붙기까지 기다리는 시간 — 1·2·4·8·16·30초, 그다음은 30초.
 *
 * **지터를 먼저 얹고 상한을 나중에 씌운다.** 반대로 하면 30초 단계에 ±20% 가 붙어 36초까지
 * 가고, 계약이 적은 「최대 30초」가 거짓이 된다.
 *
 * [rand] 는 0..1 을 주는 것이면 무엇이든 되고, 시험은 양 끝(0.0·1.0)을 직접 넣어 경계를 잰다 —
 * 무작위로 백 번 굴려 「대충 맞다」고 말하는 시험은 경계를 못 짚는다.
 */
object Backoff {
    private val steps = longArrayOf(1_000, 2_000, 4_000, 8_000, 16_000, 30_000)
    const val CAP_MS = 30_000L
    const val JITTER = 0.2

    fun delayMs(attempt: Int, rand: Double): Long {
        val base = steps[minOf(maxOf(attempt, 1), steps.size) - 1]
        val spread = base * JITTER
        val jittered = base - spread + (2 * spread * rand.coerceIn(0.0, 1.0))
        return minOf(jittered.toLong(), CAP_MS)
    }
}
