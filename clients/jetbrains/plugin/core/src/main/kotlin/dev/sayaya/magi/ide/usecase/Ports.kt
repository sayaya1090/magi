package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import java.io.Closeable
import java.nio.file.Path

/**
 * 이 층이 데몬에게 물을 수 있는 것 전부.
 *
 * 왜 인터페이스인가. 이 층의 규칙 — 언제 `steer` 이고 언제 `submit` 인지, 소켓 파일이 있는데
 * 안 듣는 것과 아예 없는 것이 어떻게 다른지 — 는 **바이트가 어떻게 건너가는지와 무관하다.**
 * 오늘은 AF_UNIX 지만 Gateway 나 WSL 에서는 소켓이 다른 호스트에 있고, 그때 갈아 끼워야 하는
 * 것은 전송뿐이어야 한다. 규칙이 `SocketChannel` 을 알고 있으면 규칙까지 따라 움직인다.
 *
 * **와이어 DTO 를 도메인 엔티티로 옮기지 않는다.** 클린 아키텍처의 통상 처방이지만 여기서
 * 그러면 데몬 계약의 두 번째 표현이 생기고, 이 저장소는 그 결함을 이미 겪었다 — 같은 규칙을
 * 두 곳에 적었더니 sanitize 가 갈렸고 문서가 코드와 갈렸다. 이 플러그인에는 "턴이란
 * 무엇인가"에 대한 독자적 규칙이 없다. **데몬의 어휘가 곧 도메인이다.**
 */
interface Daemon : Closeable {

    /**
     * 요청 하나와 그 답. 락스텝이므로 **이 호출이 끝나기 전에는 같은 [Daemon] 으로 다른 교환을
     * 시작할 수 없다** — 구현이 잠근다.
     */
    fun exchange(request: Request): Response

    /**
     * 이 연결을 **스트림으로 넘긴다.** 요청 한 줄을 보내고, 그 뒤로는 데몬이 미는 프레임을
     * [each] 에 넘긴다. [each] 가 false 를 돌려주거나 데몬이 연결을 닫으면 끝난다.
     *
     * 부른 순간부터 **이 연결로 다른 교환을 할 수 없다.** 락스텝이 성립하지 않기 때문이고,
     * 데몬 쪽도 같은 규칙이다 — `watch` 와 `transcript` 만 연결을 통째로 넘겨받는다. 그래서
     * 부르는 쪽은 **스트림 전용 연결**을 따로 열어야 한다.
     */
    fun stream(request: Request, each: (Response) -> Boolean)
}

/**
 * 소켓 경로 하나에 대해 전송이 답할 수 있는 것들.
 *
 * [DaemonLifecycle] 이 transport 의 `SocketPath` 를 직접 부르지 않게 하려고
 * [unusable] 과 [present] 까지 여기로 올렸다. 둘 다 실은 전송의 질문이다 — "이 경로로 애초에
 * 열 수 있나"는 AF_UNIX 주소 길이 문제이고, "파일이 그 자리에 있나"는 파일시스템이다. 규칙 쪽에
 * 남겨 두면 예외 하나짜리 경계가 되고, 예외 하나짜리 경계는 곧 예외 둘이 된다.
 */
interface Daemons {

    /** 붙는다. 못 붙으면 던지고, **그 예외가 곧 "내가 데몬이 될 차례"라는 답**이다. */
    fun connect(socket: Path): Daemon

    /** 붙을 수 있는지만 본다. 파일의 존재로 판정하면 안 되는 이유는 설계 문서 §2. */
    fun alive(socket: Path): Boolean

    /** 소켓 파일이 그 자리에 있는가. [alive] 가 거짓일 때 "나갔다"와 "죽었다"를 가른다. */
    fun present(socket: Path): Boolean

    /** 이 경로로는 열 수 없다는 이유, 열 수 있으면 null. */
    fun unusable(socket: Path): String?
}
