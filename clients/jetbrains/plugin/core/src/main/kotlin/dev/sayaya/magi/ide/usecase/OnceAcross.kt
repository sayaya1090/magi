package dev.sayaya.magi.ide.usecase

import java.util.concurrent.CompletableFuture

/**
 * **여럿이 동시에 부르면 한 번만 한다.** 그 한 번의 결과를 모두가 받는다.
 *
 * 코어 바이너리를 받아오는 일이 이 모양이다. 바이너리는 계정에 하나이고 자리도 하나인데
 * (`<config>/bin/<판>/magi`), 받으러 가는 것은 **프로젝트마다**다. 인텔리제이 창을 셋 켜 놓고
 * 플러그인을 깔면 셋이 나란히 같은 자리에 도착한다 — 아직 아무것도 안 받았으니 셋 다 「없다」를
 * 보고, 셋 다 사람에게 묻고, 셋 다 같은 파일을 같은 경로로 내려받는다. 실사용 보고: 받는다는
 * 대화가 여러 번 떴다.
 *
 * 이 자리에는 중재자가 없었다. 데몬 기동 쪽에는 있다 — 창이 둘이면 둘 다 띄우려 하지만 데몬의
 * `Listen` 이 소켓 경로를 선점해 하나만 서고 나머지는 「이미 듣고 있다」로 거절당한다. **그
 * 중재는 한 걸음 뒤에 있고**, 받아오기는 그 앞이라 아무것도 막지 않았다. 거절을 기억하는 자리도
 * 앱 수준인데(한 번 미룬 사람에게 프로젝트마다 모달을 들이밀지 않는다) 묻는 쪽만 아니었다.
 *
 * 기다리는 쪽이 그냥 물러나면 안 된다. 물러난 프로젝트는 이 IDE 가 사는 동안 데몬을 못 띄운다
 * — 백오프 재접속은 붙기만 하지 띄우지 않는다. 그래서 **같은 결과를 받아** 각자 제 데몬을
 * 띄운다. 소켓은 워크스페이스마다 다르므로 그쪽은 나뉘는 것이 맞다.
 *
 * 끝나면 자리를 비운다. 동시에 온 무리는 한 번을 나눠 갖지만, 몇 분 뒤 다시 필요해진 쪽은
 * (앞의 시도가 실패했든 파일이 지워졌든) 새로 시작할 수 있어야 한다.
 */
class OnceAcross<T> {

    private val lock = Any()
    private var flight: CompletableFuture<T>? = null

    /**
     * 도는 것이 있으면 그것을 주고, 없으면 [start] 로 하나 시작해서 준다.
     *
     * [start] 는 **락 밖에서** 불린다 — 사람에게 묻는 대화를 띄우는 일이 여기 들어올 수 있고,
     * 락을 쥔 채 그러면 다른 창이 그 대화가 닫힐 때까지 멎는다.
     */
    fun join(start: (CompletableFuture<T>) -> Unit): CompletableFuture<T> {
        val mine: CompletableFuture<T>
        synchronized(lock) {
            flight?.let { return it }
            mine = CompletableFuture()
            flight = mine
        }
        // 끝나면(값이든 예외든) 자리를 비운다. whenComplete 를 **먼저** 걸어 둔다 — start 가
        // 그 자리에서 완료시켜 버릴 수도 있고, 그때 등록이 늦으면 자리가 영영 안 비워진다.
        mine.whenComplete { _, _ -> synchronized(lock) { if (flight === mine) flight = null } }
        runCatching { start(mine) }.onFailure { mine.completeExceptionally(it) }
        return mine
    }

    /** 지금 도는 것이 있나. 화면이 「받는 중」이라고 말할 수 있게. */
    fun inFlight(): Boolean = synchronized(lock) { flight != null }
}
