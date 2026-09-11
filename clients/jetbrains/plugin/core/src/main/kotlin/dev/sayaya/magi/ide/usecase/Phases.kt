package dev.sayaya.magi.ide.usecase

/**
 * 컴패니언에 대한 이 창의 상태 — `docs/CLIENT_LIFECYCLE` §3 의 그림을 **타입으로** 옮긴 것.
 *
 * ⚠ **지금까지 이 상태들은 문서에만 있었다.** 창은 같은 이야기를 불리언과 맵으로 흩어 들고 있다 —
 * `external`, `starting`, 예산의 `Verdict`, 그리고 「소켓이 돌아왔나」. 그 조각들이 서로 모순되는
 * 조합을 만들 수 있고(닫는 중인데 기동 중이라거나), 그런 조합은 아무 데서도 거절되지 않는다.
 *
 * 여기서 얻는 것은 셋이다: 전이가 **한 자리**에 적히고, 없는 전이가 **거절**되고, 닫힘이 **세대**를
 * 올려 늦게 끝난 비동기가 제 결과를 버릴 수 있게 된다(§3 의 「Closing 에 진입하면 … 세대 번호를
 * 증가시킵니다. 이전 비동기 완료는 결과를 폐기하고 자원을 닫습니다」).
 */
enum class Phase { Discovering, Starting, Ready, Backoff, Blocked, Closing, Closed }

/** 상태를 옮기는 사건. 창이 이미 아는 순간들이고, 새 개념을 만들지 않는다. */
enum class Move {
    /** 붙었다 — handshake 가 답했고 세대까지 확인했다. */
    Answered,

    /** 없고, 띄워도 된다. */
    Absent,

    /** 못 쓴다 — 바이너리 없음·호환 안 됨·상태를 모름. */
    Unusable,

    /** 띄우기가 실패했다. */
    LaunchFailed,

    /** 붙어 있던 전선이 끊겼다. */
    Lost,

    /** 다시 볼 때가 됐다(유예가 지났다). */
    Retry,

    /** 예산이 바닥났다. */
    Exhausted,

    /** 사람이 다시 하라고 했거나 설정이 바뀌었다. */
    Asked,

    /** 창이 닫힌다. */
    Close,

    /** 닫는 일이 끝났다. */
    Settled,
}

/**
 * §3 의 전이표. **여기 없는 조합은 없는 전이다.**
 *
 * `Close` 는 어디서나 받는다 — 창은 아무 때나 닫힌다. `Closed` 에서는 아무것도 안 받는다:
 * §3 의 「`Closed` 에서 같은 인스턴스를 재사용하지 않습니다. 새 창은 새 소유자입니다」가 그 말이고,
 * 다시 쓰이는 인스턴스는 닫으면서 취소한 일을 되살린다.
 */
object Phases {

    private val table: Map<Pair<Phase, Move>, Phase> = mapOf(
        (Phase.Discovering to Move.Answered) to Phase.Ready,
        (Phase.Discovering to Move.Absent) to Phase.Starting,
        (Phase.Discovering to Move.Unusable) to Phase.Blocked,
        (Phase.Starting to Move.Answered) to Phase.Ready,
        (Phase.Starting to Move.LaunchFailed) to Phase.Backoff,
        (Phase.Ready to Move.Lost) to Phase.Backoff,
        (Phase.Backoff to Move.Retry) to Phase.Discovering,
        (Phase.Backoff to Move.Exhausted) to Phase.Blocked,
        (Phase.Blocked to Move.Asked) to Phase.Discovering,
        (Phase.Closing to Move.Settled) to Phase.Closed,
    )

    /** [from] 에서 [move] 로 갈 수 있는 곳, 없으면 null. */
    fun next(from: Phase, move: Move): Phase? {
        if (from == Phase.Closed) return null
        if (move == Move.Close) return Phase.Closing
        return table[from to move]
    }
}

/**
 * 한 워크스페이스에 대한 이 창의 상태와 **세대**.
 *
 * 세대가 여기 있는 이유는 §3 이 그것을 닫힘에 묶어 두기 때문이다: 닫는 순간 번호가 오르고, 그
 * 번호를 들고 떠났던 비동기 작업은 돌아와서 자기 번호가 낡았음을 보고 **결과를 버린다.**
 * 「아직 닫혔나」를 물으면 닫히고-다시-열린 창을 못 가른다 — 번호는 그것을 가른다.
 *
 * 스레드 안전은 부르는 쪽 몫이 아니다: 창의 기동은 풀 스레드에서 돌고 닫힘은 EDT 에서 오므로
 * 여기서 잠근다.
 */
class Progress(initial: Phase = Phase.Discovering) {

    private val lock = Any()
    private var current = initial
    private var age = 0

    val phase: Phase get() = synchronized(lock) { current }

    /** 지금 세대. 떠나는 비동기 작업이 들고 가서 돌아올 때 [current] 와 견준다. */
    val generation: Int get() = synchronized(lock) { age }

    /** [g] 가 아직 이 창의 세대인가. 아니면 그 작업의 결과는 버린다. */
    fun still(g: Int): Boolean = synchronized(lock) { g == age && current != Phase.Closed }

    /**
     * 상태를 옮긴다. 없는 전이면 **안 옮기고 false** 를 돌려준다 — 조용히 아무 데로나 가는 것이
     * 이 타입이 막으려는 것이다.
     */
    fun on(move: Move): Boolean = synchronized(lock) {
        val to = Phases.next(current, move) ?: return false
        // 닫힘에 들어가는 그 순간 번호가 오른다. 이미 닫는 중이면 또 올리지 않는다 — 두 번 닫는
        // 것은 흔한 일이고(dispose 는 중복 호출된다) 그때마다 번호가 오르면 멀쩡히 도는 작업이
        // 제 결과를 버린다.
        if (to == Phase.Closing && current != Phase.Closing) age++
        current = to
        true
    }
}
