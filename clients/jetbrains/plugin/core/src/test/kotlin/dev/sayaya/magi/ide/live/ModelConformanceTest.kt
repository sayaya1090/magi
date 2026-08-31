package dev.sayaya.magi.ide.live

import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.Published
import dev.sayaya.magi.ide.usecase.Assist
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.LookNotes
import org.junit.jupiter.api.AfterAll
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Assumptions.assumeTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.TestInstance

/**
 * **모델을 갈아 끼웠을 때, 이 플러그인의 문들이 아직 쓸 만한가.**
 *
 * 코어의 E2E(`internal/app/e2e_test.go`)는 **에이전트 루프**를 본다 — 툴을 부르고 파일이
 * 생겼나. 그런데 이 플러그인이 쓰는 문 셋(`complete`·`suggest`·`look-over`)은 **그 루프를 안
 * 탄다.** 한 번 왕복하고 글자 하나를 돌려줄 뿐이고, 그 글자를 **우리가 파싱해서 화면에 꽂는다.**
 * 그래서 모델이 바뀌면 코어 시험은 전부 초록인데 IDE 만 이상해지는 자리가 생긴다:
 *
 *  - 자동완성이 답을 ```` ```kotlin ```` 으로 감싸 보내면, 그대로 커서 자리에 꽂힌다.
 *  - 완성이 **접두를 되풀이**해 보내면 같은 줄이 두 번 써진다.
 *  - 「설명드리겠습니다」 같은 머리말이 붙으면 그것도 코드로 들어간다.
 *  - 훑어보기가 줄번호를 계약과 다른 모양으로 붙이면([LookNotes]), 지적이 **줄에서 떨어져**
 *    전부 띠로 밀린다 — 실제로 `5broken link missing colon` 을 실측한 적이 있다.
 *
 * 그래서 재는 것은 **답의 내용이 아니라 모양**이다. 모델마다 문장은 다르고 그건 결함이 아니다.
 * 꽂을 수 있는 모양인가, 우리 파서가 짚어내는가, 그리고 **부수효과가 없는가**만 본다.
 *
 * ## 어떻게 돌리나
 *
 * 이 시험은 **모델을 실제로 부른다**(돈과 시간이 든다). 그래서 두 겹으로 잠근다 — 살아 있는
 * 데몬이 있어야 하고, 켜겠다고 말해야 한다:
 *
 *     MAGI_IDE_CONFORMANCE=1 ./gradlew :core:test --tests '*ModelConformance*' --rerun-tasks
 *
 * 데몬의 모델을 바꿔 가며 돌리면 끝에 **성적표**가 찍힌다. 실패한 항목은 그 자리에서 빨갛고,
 * 성적표는 통과한 것까지 포함해 「무엇을 봤는지」를 남긴다 — 초록이 「안 재고 지나감」이 아니게.
 */
@TestInstance(TestInstance.Lifecycle.PER_CLASS)
class ModelConformanceTest {

    private fun on(): Boolean = System.getenv("MAGI_IDE_CONFORMANCE")?.isNotBlank() == true

    private fun assist(): Assist {
        val sock = Probe.alive()
        assumeTrue(on(), "MAGI_IDE_CONFORMANCE 가 없다 — 모델을 부르는 시험은 청해야 돈다")
        assumeTrue(sock != null, "붙을 데몬이 없다 — 건너뛴다")
        return Assist({ DaemonClient.connect(sock!!) })
    }

    // ── 재는 것들 ────────────────────────────────────────────────────────────

    /**
     * 완성은 **꽂을 수 있는 글자**여야 한다. 코드 펜스도, 머리말도, 접두 되풀이도 아니다 —
     * 셋 다 그대로 커서 자리에 들어간다.
     */
    @Test
    fun `자동완성이 꽂을 수 있는 모양으로 온다`() {
        val a = assist()
        val prefix = "fun add(a: Int, b: Int): Int {\n    return "
        val suffix = "\n}\n"
        val out = report("complete") { a.completeCode("Add.kt", prefix, suffix) }
        assumeTrue(out != null, "데몬의 [autocomplete] 가 꺼져 있거나 프로필이 없다 — 재지 않는다")
        val s = out!!
        assertTrue("```" !in s, "완성에 코드 펜스가 들어 있다 — 그대로 커서에 꽂힌다: ${clip(s)}")
        assertTrue(
            !s.trimStart().startsWith("fun add"),
            "완성이 접두를 되풀이한다 — 같은 줄이 두 번 써진다: ${clip(s)}",
        )
        assertTrue(s.lineSequence().count() <= 8, "한 자리 완성이 ${s.lineSequence().count()}줄이다: ${clip(s)}")
    }

    /** 제안은 입력줄 **한 줄**에 붙는다. 여러 줄이나 펜스가 오면 그 칸이 깨진다. */
    @Test
    fun `입력줄 제안이 한 줄로 온다`() {
        val a = assist()
        val out = report("suggest") { a.suggest("이 저장소에서 ") }
        assumeTrue(out != null, "제안이 꺼져 있다 — 재지 않는다")
        val s = out!!
        assertTrue("```" !in s, "제안에 코드 펜스가 있다: ${clip(s)}")
        assertTrue(s.lineSequence().count() <= 2, "제안이 여러 줄이다 — 입력줄에 안 맞는다: ${clip(s)}")
    }

    /**
     * 훑어보기의 답이 **우리 파서에 걸리는가.** 이 시험의 요점은 모델이 뭘 짚었는지가 아니라,
     * 짚은 것이 **줄에 붙는가**다 — 안 붙으면 전부 띠로 밀려 인레이가 하나도 안 뜬다.
     */
    @Test
    fun `훑어보기 지적이 줄에 붙는다`() {
        val a = assist()
        val text = """
            fun pick(xs: List<String>, i: Int): String {
                return xs[i]
            }

            fun main() {
                val xs = listOf("a", "b")
                println(pick(xs, 5))
            }
        """.trimIndent()
        val lines = text.lineSequence().count()
        val out = report("look-over") { a.lookOver("Pick.kt", text) }
        assumeTrue(out != null, "훑어보기가 꺼져 있다 — 재지 않는다")
        val split = LookNotes.split(out!!)
        assertTrue(
            split.anchored.isNotEmpty(),
            "지적이 하나도 줄에 안 붙었다 — 전부 띠로 밀린다. 모델이 준 모양: ${clip(out)}",
        )
        val bad = split.anchored.filter { it.first !in 1..lines }
        assertTrue(bad.isEmpty(), "파일은 ${lines}줄인데 없는 줄을 짚었다: $bad")
    }

    /**
     * 커밋 초안이 **집 규칙**을 지키는가. 모델은 「다음은 커밋 메시지입니다:」 같은 머리말과
     * 펜스를 잘 붙이는데, 그것이 커밋 칸에 그대로 들어가면 사람이 지우고 써야 한다.
     */
    @Test
    fun `커밋 초안이 칸에 그대로 들어갈 모양이다`() {
        val sock = Probe.alive()
        assumeTrue(on(), "MAGI_IDE_CONFORMANCE 가 없다")
        assumeTrue(sock != null, "붙을 데몬이 없다")
        val sid = Published.of(sock!!)?.session
        assumeTrue(!sid.isNullOrBlank(), "데몬이 대화를 공표하지 않았다")
        val r = DaemonClient.connect(sock).use { Companion(it, sid!!).draftCommit() }
        // 스테이지가 비면 빈 답이 온다 — 그건 실패가 아니라 「지을 것이 없다」다.
        val msg = r.out?.trim().orEmpty()
        note("git-msg", if (r.ok) "ok" else "err:${r.error}", msg)
        assumeTrue(r.ok && msg.isNotEmpty(), "스테이지가 비어 지을 것이 없다 — 재지 않는다")
        assertTrue("```" !in msg, "커밋 초안에 코드 펜스가 있다: ${clip(msg)}")
        assertTrue(
            msg.lineSequence().first().length <= 100,
            "첫 줄이 ${msg.lineSequence().first().length}자다 — 제목으로 안 쓰인다: ${clip(msg)}",
        )
    }

    /**
     * **부수효과가 없어야 한다.** 이 문 셋은 읽기다 — 완성 한 번이 워크스페이스를 바꾸면
     * 사람은 자기가 안 한 변경을 보게 된다. 코어 E2E 는 이 문들을 안 타므로 이 사실을 못 잰다.
     */
    @Test
    fun `거들기 문은 워크스페이스를 안 바꾼다`() {
        val a = assist()
        val sock = Probe.alive()!!
        val wd = Published.of(sock)?.workdir
        assumeTrue(!wd.isNullOrBlank(), "데몬이 워크디렉토리를 공표하지 않았다")
        val before = gitStatus(wd!!)
        assumeTrue(before != null, "git 저장소가 아니다 — 이 방식으로는 못 잰다")
        a.completeCode("Add.kt", "fun add(a: Int, b: Int): Int {\n    return ", "\n}\n")
        a.suggest("이 저장소에서 ")
        a.lookOver("Pick.kt", "fun main() { println(1) }\n")
        val after = gitStatus(wd)
        note("no-side-effect", if (before == after) "ok" else "changed", "")
        assertTrue(before == after, "거들기 문을 부른 뒤 워크스페이스가 바뀌었다:\n$before\n→\n$after")
    }

    // ── 성적표 ───────────────────────────────────────────────────────────────

    private fun gitStatus(dir: String): String? = runCatching {
        val p = ProcessBuilder("git", "status", "--porcelain")
            .directory(java.io.File(dir)).redirectErrorStream(true).start()
        val out = p.inputStream.bufferedReader().readText()
        if (p.waitFor() != 0) null else out
    }.getOrNull()

    private fun clip(s: String) = s.replace("\n", "⏎").take(160)

    private fun report(door: String, work: () -> String?): String? {
        val t0 = System.nanoTime()
        val out = runCatching(work).getOrElse { e -> note(door, "throw:${e.javaClass.simpleName}", ""); throw e }
        note(door, if (out == null) "null" else "${(System.nanoTime() - t0) / 1_000_000}ms", out.orEmpty())
        return out
    }

    private fun note(door: String, how: String, out: String) {
        card += Triple(door, how, clip(out))
    }

    private val card = mutableListOf<Triple<String, String, String>>()

    @AfterAll
    fun printCard() {
        if (card.isEmpty()) return
        // 성적표는 **통과한 것까지** 적는다. 초록이 「안 재고 지나감」과 같아 보이면 안 된다.
        println(buildString {
            append("\n── 모델 적합성 성적표 ──────────────────────────────\n")
            card.forEach { (door, how, out) -> append(String.format("  %-14s %-10s %s%n", door, how, out)) }
            append("────────────────────────────────────────────────────\n")
        })
    }
}
