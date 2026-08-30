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
 * 소켓 하나에 붙어 보고 **무엇을 만났는지**. 관측이지 결정이 아니다 — 되살릴지 말지는
 * [DaemonLifecycle.Verdict] 가 정한다.
 *
 * **왜 `Boolean` 이 아닌가.** 예전엔 `alive(): Boolean` 이었고 붙어 보다 난 예외를 **전부**
 * false 로 접었다. 접힌 것을 [DaemonLifecycle] 이 "파일은 있는데 안 듣는다 = 죽임을 당했다"로
 * 폈다 — 못 물어본 것을 데몬에 대한 **긍정 진술**로 바꾼 셈이고, 그 판정에는 재기동이 달려 있다.
 * **errno 만으로는 못 가른다.** 처음엔 예외 종류로만 갈랐고, 그 표는 한 커널(macOS)에서만
 * 잰 것이었다. 같은 프로그램으로 두 커널에서 다시 쟀다(JDK 21 — macOS / Linux 컨테이너):
 *
 * | 만난 것 | 종류(`isOther`) | macOS 예외 | Linux 예외 | 갈래 |
 * |---|---|---|---|---|
 * | 듣고 있음 | 소켓 ✓ | 없음 | 없음 | [Listening] |
 * | 죽은 데몬, 소켓 파일 남음 | 소켓 ✓ | `ConnectException` | `ConnectException` | [Refused] |
 * | 없는 경로 | — | (`notExists` 가 먼저 잡는다) | (같음) | [Absent] |
 * | 소켓이 아닌 보통 파일 | 소켓 ✗ | `SocketException: … non-socket` | **`ConnectException`** | [CouldNotAsk] |
 * | 볼 수 없는 디렉토리 안 | 못 봄 | `BindException: Permission denied` | `BindException: Permission denied` | [CouldNotAsk] |
 * | 〃 (root — 디렉토리를 통과한다) | 소켓 ✗ | — | **`ConnectException`** | [CouldNotAsk] |
 *
 * 굵은 두 칸이 이 설계의 사유다. **리눅스는 소켓이 아닌 것에 붙어 볼 때 죽은 데몬과 똑같은
 * 말을 한다.** errno 로만 가르면 「데몬이었던 적이 없는 파일」이 「죽었다」가 되고, 그 판정에는
 * 재기동이 달려 있다 — 화면은 「소켓은 있는데 아무도 안 듣는다」고 거짓말한다. CI(우분투)가
 * 하루 넘게 그 시험으로 빨갰고, 이 기계(macOS)에서는 계속 초록이었다.
 *
 * 그래서 붙기 **전에** 파일 종류를 본다. 종류는 커널을 안 탄다 — 확실히 소켓이 아닌 것
 * (`isOther == false`: 보통 파일·디렉토리)은 거기서 [CouldNotAsk] 로 낸다. 못 보면(권한)
 * 판단하지 않고 붙어 보는 쪽으로 넘긴다: 모르는 것을 아는 척하지 않는다.
 *
 * 즉 **진짜 "아무도 안 듣는다"는 「소켓인데 `ConnectException`」 하나뿐**이고 나머지는 데몬에
 * 대해 아무것도 안 말한다. 갈래를 나누는 기준은 늘 같다: **받는 쪽이 할 일이 다르면 갈래다.**
 */
sealed interface Reach {

    /** 붙었다. */
    data object Listening : Reach

    /**
     * 소켓 파일이 **확실히** 없다. 질서 있게 나간 자리다.
     *
     * "확실히"가 [CouldNotAsk] 와 가르는 말이다. `Files.exists` 는 볼 수 없을 때도 false 라서
     * 그걸로 판정하면 「못 봤다」가 「나갔다」로 둔갑한다. `Files.notExists` 는 그 자리에서 false 를
     * 준다(실측: 볼 수 없는 자리에서 `exists=false, notExists=false`).
     */
    data object Absent : Reach

    /** 파일은 있는데 붙기를 **거절당했다** = 아무도 안 듣는다. 이것만이 "죽었다"이다. */
    data object Refused : Reach

    /**
     * 물어볼 수가 없었다. [why] 는 만난 것 그대로.
     *
     * **데몬에 대해 아무 말도 안 한다.** 살았는지 죽었는지 모르는 것이지 죽은 것이 아니다.
     */
    data class CouldNotAsk(val why: String) : Reach
}

/**
 * 소켓 경로 하나에 대해 전송이 답할 수 있는 것들.
 *
 * [DaemonLifecycle] 이 transport 의 `SocketPath` 와 `Files` 를 직접 부르지 않게 하려고
 * [unusable] 과 [reach] 의 파일 확인까지 여기로 내렸다. 둘 다 실은 전송의 질문이다 — "이 경로로
 * 애초에 열 수 있나"는 AF_UNIX 주소 길이 문제이고, "파일이 그 자리에 있나"는 파일시스템이다.
 * 규칙 쪽에 남겨 두면 예외 하나짜리 경계가 되고, 예외 하나짜리 경계는 곧 예외 둘이 된다.
 */
interface Daemons {

    /** 붙는다. 못 붙으면 던지고, **그 예외가 곧 "내가 데몬이 될 차례"라는 답**이다. */
    fun connect(socket: Path): Daemon

    /**
     * 붙어 보고 무엇을 만났는지. 파일의 존재만으로 판정하면 안 되는 이유는 설계 문서 §2.
     *
     * 파일 확인이 이 안에 있는 것은 **두 번 묻지 않기 위해서**다. 예전엔 `alive` 와 `present` 를
     * 따로 물어 그 사이에 파일이 사라질 수 있었고, 그러면 "붙는 데 실패했고 파일도 없다"가 되어
     * 죽은 데몬이 [Reach.Absent] 로 보였다.
     */
    fun reach(socket: Path): Reach

    /** 이 경로로는 열 수 없다는 이유, 열 수 있으면 null. */
    fun unusable(socket: Path): String?
}
