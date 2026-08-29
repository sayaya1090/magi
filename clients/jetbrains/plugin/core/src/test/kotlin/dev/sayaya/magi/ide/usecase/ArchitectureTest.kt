package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
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

    /**
     * 두 모듈의 Kotlin 원본 전부. 아래 "달린 문장" 검사는 모듈 경계와 무관한 결함이라 둘 다 본다 —
     * `intellij` 에는 시험 소스 세트가 없고(SDK 를 받아야 돌아서), 그 결함이 실제로 거기서 났다.
     *
     * **뿌리마다 실제로 파일이 나왔는지 못박는다.** 없는 디렉토리에 `walkTopDown()` 을 걸면 예외가
     * 아니라 **빈 시퀀스**라, 경로가 한 칸만 밀려도 검사가 0개를 훑고 초록이 된다. 모듈 이름이
     * 바뀌거나 시험의 작업 디렉토리가 옮겨지면 그렇게 된다 — 그리고 그때 증상이 "규칙 통과"다.
     * 이 파일이 잡으라고 있는 결함과 정확히 같은 부류라(참이 아니면서 참처럼 보이는 것) 여기서
     * 봉합한다. 실측으로 확인했다: 뿌리를 틀린 이름으로 바꾸면 시험이 그대로 통과했다.
     */
    private fun sources(): List<File> {
        val roots = listOf(File("src/main/kotlin"), File("../intellij/src/main/kotlin"))
        val byRoot = roots.associateWith { d ->
            d.walkTopDown().filter { it.isFile && it.extension == "kt" }.toList()
        }
        byRoot.forEach { (root, files) ->
            assertTrue(files.isNotEmpty(), "$root 에서 .kt 를 하나도 못 찾았다 — 경로가 밀렸거나 모듈이 옮겨졌다")
        }
        return byRoot.values.flatten()
    }

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
            setOf("Activity.kt", "Assist.kt", "Authorship.kt", "Companion.kt", "Hand.kt", "DaemonLifecycle.kt", "Level.kt", "Markup.kt", "McpName.kt", "Palette.kt", "Ports.kt", "Problems.kt", "Transcript.kt"),
            names,
            "usecase 의 파일 목록이 예상과 다르다 — 옮겼으면 이 시험의 경로도 같이 옮길 것",
        )
    }

    /**
     * 이어지는 것처럼 보이는 `return@` 을 막는다.
     *
     * 실제로 난 결함이다. `Workspace.onDaemon` 이 이렇게 적혀 있었다 — `if (...)` 뒤에
     * `return@executeOnPooledThread` 로 줄이 끝나고, 다음 줄에 `trouble(...)` 이 더 들여쓴 채
     * 있었다. 눈에는 조건의 몸으로 보이는데 코틀린은 **별개 문장**으로 읽는다. 그래서 정상일
     * 때마다 "데몬 없음"을 말한 다음 이어서 성공했다 — 메시지가 정확히 거꾸로였다.
     *
     * 단위 시험으로는 안 잡혔다. 그 자리가 IntelliJ `Project` 를 요구해 시험이 없었고,
     * **샌드박스 IDE 를 실제로 띄워 로그를 읽고서야** 나왔다(폴 46회 전부 trouble 과 ok 가 같은
     * 밀리초에 찍혔다).
     *
     * 잡는 모양은 좁다: `return@…` 으로 줄이 끝나고 **다음 줄이 더 들여쓰였을 때**. 같은 들여쓰기면
     * 이어지는 척을 안 하므로 정상이다 — `MagiToolWindow` 의 낡은-제안 가드가 그 모양이고 의도대로다.
     */
    @Test
    fun `return 뒤에 이어지는 척하는 문장이 없다`() {
        // 라벨 있는 것과 없는 것 둘 다. 함수 안의 `if (x) return` 뒤에 더 들여쓴 줄도 같은 함정이고
        // 눈에 속는 정도도 같다. 넓혀도 이 트리에서 오탐 0으로 실측됐다.
        val dangling = Regex("""(return@\w+|(?<![\w.])return)\s*$""")
        fun indent(s: String) = s.length - s.trimStart().length
        val bad = sources().flatMap { f ->
            val lines = f.readLines()
            lines.indices.mapNotNull { i ->
                val here = lines[i]
                val next = lines.getOrNull(i + 1)
                when {
                    !dangling.containsMatchIn(here) -> null
                    next == null || next.isBlank() -> null
                    indent(next) > indent(here) -> "${f.name}:${i + 1}"
                    else -> null
                }
            }
        }
        assertEquals(
            emptyList<String>(),
            bad,
            "`return@…` 으로 줄을 끝내고 다음 줄을 더 들여쓰면 그 줄은 조건 밖의 문장이다. " +
                "의도가 '조건일 때만'이면 중괄호를 쓸 것.",
        )
    }
}
