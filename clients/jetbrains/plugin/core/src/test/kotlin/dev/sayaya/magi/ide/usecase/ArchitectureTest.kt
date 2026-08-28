package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 규칙 층이 전송을 모르는 상태를 얼려 둔다.
 *
 * 경위. 처음에는 `Companion`·`Assist`·`DaemonLifecycle` 셋이 구체 클래스 `DaemonClient` 를 직접
 * 잡고 있었다. 돌아가는 데는 문제가 없었다 — 이 시험이 막는 것은 오늘의 고장이 아니라 **원격
 * 개발**이다. Gateway 나 WSL 에서는 소켓이 다른 호스트에 있어 전송이 진짜로 달라지는데, 그때
 * "언제 steer 이고 언제 submit 인가" 같은 규칙까지 `SocketChannel` 을 안다는 이유로 따라
 * 움직이면 갈아 끼울 수가 없다.
 *
 * 시험이 소스 텍스트를 읽는 이유. 리플렉션으로는 **import 가 안 보인다.** 인터페이스만 쓰는 코드와
 * 구현을 직접 부르는 코드가 런타임에는 구분이 안 되고, 여기서 막고 싶은 것은 정확히 그
 * 구분이다. 그래서 규칙을 컴파일된 것이 아니라 적힌 것에서 읽는다.
 */
class ArchitectureTest {

    private val usecase = File("src/main/kotlin/dev/sayaya/magi/ide/usecase")

    @Test
    fun `usecase 는 transport 를 import 하지 않는다`() {
        val offenders = usecase.listFiles { f -> f.name.endsWith(".kt") }.orEmpty()
            .flatMap { f ->
                f.readLines()
                    .filter { it.startsWith("import ") && "ide.transport" in it }
                    .map { "${f.name}: ${it.trim()}" }
            }
        assertEquals(
            emptyList<String>(),
            offenders,
            "규칙 층이 전송을 import 했다. 필요한 것이 있으면 Ports.kt 의 Daemons 에 질문을 " +
                "하나 더 내고 transport 가 답하게 할 것 — 예외를 하나 두면 곧 둘이 된다.",
        )
    }

    /**
     * 그리고 시험 자체가 아무것도 안 보고 통과하는 일이 없게 한다. 디렉토리 이름이 바뀌거나
     * 패키지가 옮겨 가면 위 시험은 빈 목록을 얻어 **조용히 초록**이 된다. 실제로 이 저장소에서
     * 겪은 결함이다(해시 골든의 `a` 를 깨뜨렸는데 다섯 골든 중 어느 것도 `a` 를 안 써서 안 깨졌다).
     */
    @Test
    fun `시험이 실제로 파일을 보고 있다`() {
        val names = usecase.listFiles { f -> f.name.endsWith(".kt") }.orEmpty().map { it.name }.toSet()
        assertEquals(
            setOf("Assist.kt", "Companion.kt", "DaemonLifecycle.kt", "McpName.kt", "Ports.kt"),
            names,
            "usecase 의 파일 목록이 예상과 다르다 — 옮겼으면 이 시험의 경로도 같이 옮길 것",
        )
    }
}
