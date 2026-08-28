package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 소스 글자 자체를 보는 시험. **컴파일러가 안 잡는 것만 여기 둔다.**
 *
 * 여기 있는 규칙은 전부 "돌긴 도는데 딴것을 한다" 부류다. 타입이 맞고 빌드가 초록이고
 * 시험이 다 통과하는데 사람이 보는 글자가 틀린 것 — 그래서 컴파일러도 다른 시험도 못 잡고,
 * 잡을 자리가 여기밖에 없다.
 *
 * **왜 `core` 에 있나.** 보는 대상은 `intellij` 쪽이 더 많은데 그 모듈에는 시험 소스셋이
 * 아예 없다(`build.gradle.kts` 에 `src/test` 가 없다). 이 시험은 클래스가 아니라 **글자**를
 * 읽으므로 모듈 경계에 안 걸린다 — IDE 플랫폼을 안 당겨온다.
 *
 * **못 찾으면 통과가 아니라 실패다.** 소스 트리를 못 찾았는데 조용히 넘어가면 이 시험은
 * 있으나 마나가 되고, 그 사실을 아무도 모른다. 없음을 통과로 읽지 않는다.
 */
class SourceTextTest {

    private val sources: List<File> by lazy {
        // gradle 의 test 작업은 프로젝트 디렉토리(`plugin/core`)에서 돈다. 한 칸 위가 `plugin` 이고
        // 거기에 두 모듈이 다 있다.
        val root = File(System.getProperty("user.dir")).parentFile
        val kt = root.walkTopDown()
            .filter { it.isFile && it.extension == "kt" && "${File.separator}build${File.separator}" !in it.path }
            .toList()
        assertTrue(kt.size > 10, "소스를 못 찾았다(${root.absolutePath}). 이 시험이 아무것도 안 보고 있다")
        kt
    }

    @Test
    fun `달러를 글자로 박아 두면 화면에 템플릿 원문이 찍힌다`() {
        // 코틀린에서 달러를 `'$'` 리터럴로 감싼 템플릿 표현은 **달러 한 글자**로 평가된다. 그래서
        // 그렇게 쓴 "#(달러){e.seq}" 는 seq 를 넣지 않고 "#(달러){e.seq}" 라는 글자를 그대로
        // 찍는다. 타입이 맞으니 컴파일이 통과하고, 값이 아니라 문자열이라 어떤 시험도 안 건드린다 —
        // 창을 눈으로 봐야만 드러난다.
        //
        // 이렇게 되는 경로가 하나 있다. 셸 히어독으로 코틀린을 써 넣을 때 달러가 셸에 먹히지
        // 않게 막는 관용구가 저것인데, 막은 채로 파일에 남으면 그대로 굳는다. 실제로 이 저장소의
        // 창 코드 여섯 자리와 시험 실패 메시지 두 자리가 그 상태로 들어와 있었다 — 전사 한 줄을
        // 만드는 `render` 도 거기 있었으므로 **모든 줄이 같은 원문으로 찍히고 있었다.**
        //
        // 진짜 달러 한 글자가 필요하면 `\$` 로 쓴다. 짧고, 셸을 안 거쳐도 읽히고, 무엇보다
        // 보간과 헷갈리지 않는다.
        //
        // 찾을 글자를 **여기서 이어 붙인다.** 통째로 적어 두면 이 파일이 자기 규칙에 걸리고,
        // 그렇다고 이 파일만 빼 주면 규칙에서 빠져나갈 구멍을 하나 만들어 두는 셈이 된다.
        val marker = "\${" + "'\$'" + "}"
        val bad = sources.filter { marker in it.readText() }
        assertTrue(bad.isEmpty(), "달러가 글자로 박혔다 — 보간이 아니라 원문이 찍힌다: " +
            bad.joinToString(", ") { it.name })
    }

    @Test
    fun `라벨에 글자를 넣는 문은 하나이고 그 문은 수준만 받는다`() {
        // 창 윗줄 라벨은 **수준**을 쓰는 자리다 — 「컴패니언이 붙어 있다」, 「데몬이 없다」. 거기에
        // **사건**을 쓰면(「안 갔다: 사유」) 다음 수준 갱신이 그것을 지운다. 둘은 같은 것에 대한 새
        // 소식이 아니라서 뒤엣것이 이겨도 앞엣것이 거짓이 되지 않는다 — 그냥 사유가 사라진다.
        // 갱신을 부르는 것이 전사 프레임 하나이므로(`movesPrompt` → `refresh`) **순서를 잡는
        // 것으로는 안 막힌다.**
        //
        // 예전엔 이것을 **자리 수**로 붙들었다(라벨에 쓰는 자리가 둘, 둘 다 수준). 셈은 도달이다:
        // 자리가 둘인 채 그중 하나가 조용히 사건으로 바뀌면 셈은 그대로 둘이라 아무도 안 운다.
        // 「둘 다 수준이다」를 시험이 아니라 **내 판단**이 붙들고 있었다.
        //
        // 지금은 그 문장을 **쓸 수가 없다** — 라벨에 넣는 값이 `Level` 갈래이고 사건에는 거기
        // 들어올 이름이 없다(`Level.Unreachable` 하나만 글자를 받는데, 그걸로 사건을 실으려면
        // 「닿지 못했다」는 이름으로 거짓말을 해야 한다). 그래서 셈이 필요 없어졌고, 대신 **그
        // 타입이 실제로 길목에 서 있는지**만 잰다. 문이 여럿이면 타입은 그중 하나만 지킨다.
        //
        // 주석을 걷어내고 센다. 이 규칙은 코드에 대한 것인데 안 걷으면 **설명하는 문장이 위반으로
        // 잡힌다** — 바로 아래 `say` 의 KDoc 이 그렇다.
        val f = sources.first { it.name == "MagiToolWindow.kt" }
        val code = f.readText()
            .replace(Regex("""/\*.*?\*/""", RegexOption.DOT_MATCHES_ALL), "")
            .replace(Regex("""//[^\n]*"""), "")

        val door = "private fun say(l: Level)"
        val writes = Regex("""\bstate\.text\b""").findAll(code).count()
        assertTrue(writes == 1 && door in code && "state.text" in code.substringAfter(door),
            "라벨에 글자를 넣는 자리가 $writes 이고 문은 `$door`" +
                (if (door in code) "" else " — 그 문이 없다") + ". 하나여야 하고 그 안이어야 한다. " +
                "문이 늘거나 `Level` 이 아닌 것을 받게 되면 「수준만 쓴다」를 지키는 것이 다시 " +
                "주석뿐이 된다")

        assertTrue("say(state" !in code,
            "라벨을 인자로 받는 `say` 가 돌아왔다. 그러면 아무 글자나 설 수 있어서 보고가 다시 " +
                "라벨로 간다 — 사건은 `report()` 로 전사에 낸다")
    }

    @Test
    fun `판을 비우는 것은 첫 프레임이 아니라 붙었다는 말에 걸린다`() {
        // 이 창은 커서를 안 보내므로 다시 붙을 때마다 재생이 통째로 다시 온다 — 그래서 붙을 때마다
        // 판을 비워야 한다. 그것을 **첫 프레임**에 걸면, 프레임이 안 오는 전사에서는 비울 기회가
        // 영영 없다. 그런데 그게 예외가 아니라 **기본 경로**다: 데몬이 재시작하면 새 세션의 전사는
        // 비어 있다. 즉 「끊겨서 다시 붙었다」가 곧 「프레임이 안 온다」이고, 그 자리에서 화면은 지난
        // 세션의 대화를 지금 것인 양 세워 둔다.
        //
        // 붙자마자 비우면 **못 붙은 시도**가 사람이 읽던 전사를 지운다는 것이 첫 프레임에 걸어 둔
        // 사유였는데, `began` 은 애초에 못 붙으면 안 온다(`TranscriptTest`). 그래서 이쪽이 그 사유를
        // 잃지 않으면서 위의 구멍을 안 만드는 자리다. 순서도 스트림이 보장한다 — `follow` 는 워커를
        // 띄우기 전에 `began` 을 부른다.
        //
        // 잰다고 이 시험이 화면을 띄우지는 않는다. 보는 것은 **비움이 어느 문 안에 적혀 있는가**뿐
        // 이고, 그건 글자로 확인된다. 되돌아오는 모양(비움을 `frame` 으로 옮기기)에 정확히 걸린다.
        val f = sources.first { it.name == "MagiToolWindow.kt" }
        val src = f.readText()
        val body = { from: String, to: String ->
            val a = src.indexOf(from).also { assertTrue(it >= 0, "`$from` 이 없다") }
            val b = src.indexOf(to, a).also { assertTrue(it >= 0, "`$from` 뒤에 `$to` 가 없다") }
            src.substring(a, b)
        }
        assertTrue(body("override fun began()", "override fun frame(").contains("log.text = \"\""),
            "붙었다는 말에 판을 비우는 것이 없다. 프레임에 걸면 프레임이 안 오는 전사를 못 비운다")
        assertTrue(!body("override fun frame(", "override fun note(").contains("log.text = \"\""),
            "프레임이 판을 비운다. 그러면 프레임이 하나도 안 오는 전사 — 데몬 재시작 뒤의 보통 경로 " +
                "— 에서 지난 대화가 그대로 서 있는다. 비움은 `began` 에 건다")
    }

    @Test
    fun `열 때 한 번 센 값을 지금인 양 그리지 않는다`() {
        // 이 저장소가 이미 아는 결함의 시간 축 판본이다. 「경고를 게으른 자리에만 두면 경고가
        // 필요한 사람이 제일 못 본다」로 우측 판에서 상태 표시줄로 옮긴 경고가, 옮긴 자리에서
        // **열릴 때 한 번만** 세어졌다. 컨텐트 루트는 세션 중에 바뀌므로 이건 같은 잘못이다 —
        // 경고가 필요해지는 순간에 루트를 더한 사람이 제일 못 본다. 반대쪽도 같다: 사람이 루트를
        // 워크스페이스 안으로 옮겨 **시킨 대로 해도** 경고가 안 사라졌다.
        //
        // 미끄러진 자리를 적어 둔다. 두 주석 다 「데몬을 안 기다린다」를 근거로 들고 있었는데,
        // 그건 **데몬을 안 부른다**의 근거이지 **한 번만 센다**의 근거가 아니다. 근거 하나로 둘을
        // 사면 나중에 읽는 사람이 안 나눈다.
        //
        // 되돌아오는 모양 둘에 정확히 걸린다: 세어 필드에 두기, 그리고 그리기 문을 생성 시점
        // 하나로 되돌리기. 숫자가 아니라 **자리**를 보므로 호출자가 늘어도 안 약해진다.
        val bar = sources.first { it.name == "StatusBar.kt" }.readText()
        assertTrue("private fun unreachable()" in bar,
            "못 만지는 루트 수를 부를 때마다 안 센다. 필드에 두면 세션 중에 루트가 바뀌어도 안 변한다")
        assertTrue("private val unreachable" !in bar,
            "못 만지는 루트 수를 `val` 로 세어 뒀다. 매 틱 그리는 줄이 한 번 잰 값을 실어 나른다")

        val facts = sources.first { it.name == "FactsToolWindow.kt" }.readText()
        val refresh = facts.substringAfter("fun refresh() {").substringBefore("}")
        assertTrue("sayOutside()" in refresh,
            "그리기 문이 밖 루트 줄을 안 다시 쓴다. 생성 때 한 번 쓰면 고쳐도 안 지워진다")
        val outside = facts.substringAfter("private fun sayOutside() {").substringBefore("\n        }")
        assertTrue("say(outside, \" \")" in outside,
            "밖 루트가 없을 때 지우는 갈래가 없다. 안 쓰는 것으로 지움을 흉내내면 처음 한 번만 맞는다")
        assertTrue("isRepeats = true" in facts,
            "사실 판이 다시 안 묻는다. 「지금 무엇을 하나」를 판 연 순간의 뜻으로 세워 두게 된다")
    }

}
