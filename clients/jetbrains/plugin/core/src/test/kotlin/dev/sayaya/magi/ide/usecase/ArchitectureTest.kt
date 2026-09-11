package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
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
    fun `컴포저는 턴이 열렸는지를 사실로 넘긴다 — 탐침에 맡기지 않는다`() {
        // 유닛은 `say(turnOpen=…)` 이 옳게 고르는 것까지만 잰다. **부르는 쪽이 그 값을 정말
        // 넘기는지**는 창 코드에 있고, `intellij` 에는 시험 소스 세트가 없어 여기서 원본을 읽는다.
        //
        // 이 배선이 결함의 전부였다: 기전은 있었고 입력이 안 왔다. 창이 값을 안 넘기면 데몬 탐침이
        // 쓰이는데, 그 탐침은 평범하게 도는 턴을 못 본다(`Companion.turnIsOpen` 주석의 실측).
        val win = sources().firstOrNull { it.name == "MagiToolWindow.kt" }
        assertTrue(win != null, "MagiToolWindow.kt 를 못 찾았다 — 이 가드는 아무것도 안 읽고 있다")
        val calls = Regex("""comp\.say\([^)]*\)""").findAll(win!!.readText()).map { it.value }.toList()
        assertTrue(calls.isNotEmpty(), "컴포저의 say 호출을 못 찾았다 — 이름이 바뀌었으면 이 가드부터 고칠 것")
        assertTrue(
            calls.any { "shaper.open" in it },
            "컴포저가 턴이 열렸는지를 안 넘긴다: $calls — 창은 전사를 흘려보며 그 사실을 이미 " +
                "들고 있다(Rows.open). 안 넘기면 도는 턴에 submit 이 나가고 코어가 그 턴의 " +
                "계획을 비운다(resetForNewTopLevel).",
        )
    }

    /**
     * ★ **코어가 쓰는 사건을 전부 읽나, 아니면 안 읽는다고 사유와 함께 적었나.**
     *
     * 「분기하는 이름이 코어에 있나」의 **거울상**이고 더 비싼 쪽이다. 앞의 것은 못 도는 갈래를
     * 잡는데, 이건 **기능이 통째로 없는 것**을 잡는다 — 코어가 사실을 쓰는데 아무도 안 읽으면
     * 컴파일도 통과하고 아무것도 안 터지고 화면에만 없다.
     *
     * 실측(2026-09-09): 이 클라이언트는 27 중 25 를 읽고 있었고 진짜 구멍은 **없었다**. VS Code
     * 쪽에 같은 자를 대었더니 셋이 나왔다(`council.decided` 로 카운슬이 무엇을 결정했는지가
     * 전사에 없었고, `interjection.deferred`/`answered` 로 파킹된 프롬프트가 도는 중처럼
     * 보였다). 지금 이쪽이 깨끗한 것은 **규칙이 있어서가 아니라 우연**이므로 규칙을 둔다.
     *
     * 「읽는다」는 이 셰이퍼가 아니라 **클라이언트 전체**다 — 여럿은 계획 판이나 표시줄의 몫이고,
     * 전부 전사 행이어야 한다고 하면 사실을 엉뚱한 화면으로 민다.
     */
    @Test
    fun `3초마다 도는 폴은 폴의 인내를 쓴다`() {
        // 인내가 하나면 **틀리는 방향이 하나뿐인 것처럼 보인다.** 모델이 지나는 문은 넉넉해야
        // 하고(느린 로컬 모델의 정답이 시한 초과로 둔갑한다), 기억에서 답하는 문은 짧아야 한다
        // — 이 워치독이 있는 사유가 「3초마다 두드리면 스레드가 쌓인다」인데, 2분이면 창 둘이
        // 3초마다 두드려 첫 하나가 포기하기 전에 마흔 개가 물린다(2026-09-09 실측).
        //
        // 두 숫자가 실제로 다른지, 그리고 **3초 폴이 짧은 쪽을 쓰는지**를 본다. 배선은
        // `intellij` 에 있고 거기엔 시험 소스 세트가 없어 원본을 읽는다.
        assertTrue(
            dev.sayaya.magi.ide.transport.DaemonClient.PATIENCE_POLL <
                dev.sayaya.magi.ide.transport.DaemonClient.PATIENCE_ASK,
            "폴의 인내가 모델 문의 인내보다 짧지 않다 — 하나로 두면 웨지된 데몬 앞에서 스레드가 쌓인다",
        )

        for (name in listOf("StatusBar.kt", "PlanToolWindow.kt")) {
            val f = sources().firstOrNull { it.name == name }
            assertTrue(f != null, "$name 을 못 찾았다 — 이 가드는 아무것도 안 읽고 있다")
            val src = f!!.readText()
            // 3초 시계가 있는 파일만 이 규칙의 대상이다. 없으면 이 시험이 낡은 것이다.
            assertTrue(Regex("""Timer\(\s*3_000""") in src, "$name 에 3초 시계가 없다 — 이 시험부터 고칠 것")
            assertTrue(
                "onDaemonPolling(" in src,
                "$name 의 3초 폴이 기본 인내(모델 문의 2분)로 붙는다 — 답 안 하는 데몬 앞에서 스레드가 쌓인다",
            )
        }
    }

    @Test
    fun `창 사용량은 문을 먼저 묻고 스트림은 낙하다`() {
        // 스트림의 `context.usage` 는 transient 라 재생이 없다 — 이미 돌고 있는 대화에 붙은 창은
        // 턴이 한 번 돌기 전까지 아무것도 못 본다. 문은 지금 답하므로 **문이 먼저**다. 문 없는
        // 데몬에서는 스트림이 유일한 원천이라 낙하로 남긴다.
        //
        // 유닛은 문이 옳게 나가는 것까지만 잰다(`CompanionTest`). **판이 그것을 쓰는지**는
        // `intellij` 에 있고 거기엔 시험 소스 세트가 없어 여기서 원본을 읽는다.
        val plan = sources().firstOrNull { it.name == "PlanToolWindow.kt" }
        assertTrue(plan != null, "PlanToolWindow.kt 를 못 찾았다 — 이 가드는 아무것도 안 읽고 있다")
        val src = plan!!.readText()
        assertTrue("comp.context()" in src, "판이 `context` 문을 안 두드린다 — 스트림만 보면 턴 전에는 빈칸이다")
        assertTrue("""caps.contains("context")""" in src,
            "광고를 안 보고 문을 두드린다 — 없는 문을 부르면 거절이 오고 그 거절은 판이 할 말이 아니다")
        assertTrue(Regex("""ctxFromDoor \?: [^\n]*contextNow\(\)""") in src,
            "문과 스트림의 차례가 뒤집혔거나 낙하가 사라졌다 — 문이 먼저이고 스트림은 낙하다")
    }

    @Test
    fun `코어가 쓰는 사건을 전부 읽거나, 안 읽는다고 적었다`() {
        val repo = generateSequence(File(".").absoluteFile) { it.parentFile }
            .firstOrNull { File(it, ".git").exists() }
        assertTrue(repo != null, "저장소 뿌리를 못 찾았다 — 이 가드는 아무것도 안 읽고 있다")
        val eventGo = File(repo, "internal/core/event/event.go")
        assertTrue(eventGo.exists(), "$eventGo 가 없다 — 코어가 옮겨졌으면 이 시험부터 고칠 것")

        val types = Regex("""Type[A-Za-z]+\s+Type\s*=\s*"([a-z][a-z.]*)"""")
            .findAll(eventGo.readText()).map { it.groupValues[1] }.toSet()
        assertTrue(types.size >= 20, "코어에서 사건 이름을 ${types.size} 개밖에 못 읽었다 — 파서가 낡았다")

        val read = mutableSetOf<String>()
        for (f in sources()) {
            val body = f.readText()
                .replace(Regex("""/\*.*?\*/""", RegexOption.DOT_MATCHES_ALL), "")
                .lines().joinToString("\n") { it.replace(Regex("//.*$"), "") }
            // **점 없는 이름도 사건이다**(`compaction`·`error`). 점을 요구하는 정규식은 그 둘을
            // 「안 읽는 사건」으로 보고한다 — VS Code 쪽에서 실제로 그렇게 틀렸다.
            for (m in Regex("\"([a-z][a-z.]*)\"").findAll(body)) {
                if (m.groupValues[1] in types) read += m.groupValues[1]
            }
        }
        assertTrue(read.size >= 15, "클라이언트에서 사건 이름을 ${read.size} 개밖에 못 봤다 — 스캔이 깨졌다")
        for (dotless in listOf("compaction", "error")) {
            assertTrue(dotless in read, "스캔이 \"$dotless\" 를 못 본다 — 점 없는 이름도 사건이다")
        }

        // 일부러 안 읽는 것. 사유는 읽는 사람이 확인할 수 있어야 한다.
        val skipped = mapOf(
            "labels.changed" to "이 클라이언트는 아직 일에 이름을 안 붙인다 — 붙이는 화면이 생기면 그때 읽는다",
            "result.elided" to "덜어낸 툴 결과는 **컨텍스트 창의 사실**이지 대화의 사실이 아니다. 그 행은 이미 무엇을 불렀는지 적고 있고, 「모델에게 안 보인다」는 계획 판의 컨텍스트 띠가 말한다",
        )
        val missed = (types - read - skipped.keys).sorted()
        assertEquals(
            emptyList<String>(), missed,
            "코어가 이것들을 쓰는데 아무도 안 읽는다 — 기능이 없는데 아무도 안 운다: " +
                missed.joinToString(", ") + ". 읽거나, 사유와 함께 skipped 에 적을 것.",
        )
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
     * **디스크에 있는 소스가 전부 git 에 실려 있다.**
     *
     * 2026-09-01 에 실제로 난 결함이고, 값이 컸다: 새 파일 셋이 한 번도 커밋 안 된 채로 `main`
     * 이 **컴파일 안 되는 상태**로 며칠 서 있었다. `Copying.kt` 는 바로 앞 커밋이 부르게 해 놓고
     * 정작 클래스를 안 실은 것이었다.
     *
     * 원인은 커밋하는 손이다. 이 저장소는 공유 체크아웃이라 `git add .` 를 금하고 `git commit
     * -F - -- <경로>` 로 경로를 대는데, 그 방식은 **추적 안 되는 새 파일을 경로 안에 있어도
     * 안 싣는다.**
     *
     * 그리고 이 결함은 **로컬에서 영영 안 보인다.** 파일이 내 디스크에 있으니 내 빌드는 돈다.
     * CI 는 추적되는 것만 체크아웃한다 — 재는 기계와 사람이 다른 나무를 보고 있었다. 그래서
     * 이 가드는 「빌드가 되나」가 아니라 **「내가 보는 것과 git 이 보는 것이 같나」**를 묻는다.
     *
     * git 이 없으면 **실패한다.** 건너뛰면 이 가드가 있으나 마나인 자리로 조용히 돌아간다 —
     * 이 저장소가 세 번 겪은 「초록인데 안 재고 있다」의 그 모양이다.
     */
    @Test
    fun `소스가 전부 git 에 실려 있다`() {
        val root = generateSequence(File(".").absoluteFile) { it.parentFile }
            .firstOrNull { File(it, ".git").exists() }
        assertNotNull(root, "저장소 뿌리를 못 찾았다 — 이 가드가 아무것도 안 잰다")
        val repo = root!!
        val p = ProcessBuilder("git", "ls-files", "-z", "--", "clients/jetbrains/plugin")
            .directory(repo).redirectErrorStream(true).start()
        val out = p.inputStream.readBytes().toString(Charsets.UTF_8)
        assertEquals(0, p.waitFor(), "git ls-files 가 안 돌았다: $out")
        val tracked = out.split('\u0000').filter { it.isNotBlank() }.toSet()
        assertTrue(tracked.size > 50, "추적 목록이 ${tracked.size}개다 — 얕은 체크아웃이면 이 가드가 위양성이다")
        val plugin = File(repo, "clients/jetbrains/plugin")
        val onDisk = plugin.walkTopDown()
            .filter { it.isFile && it.extension == "kt" }
            .filterNot { f -> generateSequence(f) { it.parentFile }.any { it.name == "build" || it.name == ".intellijPlatform" } }
            .map { it.relativeTo(repo).path }
            .toList()
        assertTrue(onDisk.size > 50, "디스크에서 찾은 소스가 ${onDisk.size}개다 — 이 가드가 아무것도 안 잰다")
        assertEquals(
            emptyList<String>(),
            onDisk.filterNot { it in tracked }.sorted(),
            "이 파일들이 git 에 안 실려 있다. 내 빌드는 돌지만 CI 는 이것 없이 빌드한다 — " +
                "`git add <파일>` 을 먼저 하고 커밋할 것(경로만 대는 커밋은 새 파일을 안 싣는다).",
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
            setOf("Activity.kt", "Assist.kt", "Authorship.kt", "Companion.kt", "CoreRelease.kt", "Hand.kt", "DaemonLifecycle.kt", "Launches.kt", "Level.kt", "LookNotes.kt", "Markup.kt", "McpName.kt", "OnceAcross.kt", "Palette.kt", "Ports.kt", "Problems.kt", "Restarts.kt", "RowText.kt", "Rows.kt", "Schedules.kt", "Transcript.kt"),
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
