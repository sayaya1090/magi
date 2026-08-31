package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
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
        // **반쯤 죽는 것이 통째로 죽는 것보다 나쁘다.** 바닥이 열이면 `intellij` 가 통째로 빠져도
        // `core` 만으로 넘는다(오늘 core main 14 + core test 17) — 그런데 이 시험이 보는 것은
        // 대부분 `intellij` 쪽이다. 그때 파일을 훑어 `isEmpty()` 를 묻는 규칙들은 위반이 없어서가
        // 아니라 **잴 것이 없어서** 초록이 되고, 그 사실은 화면에서 통과와 같아 보인다.
        //
        // 개수는 안 못박는다 — 누구나 파일을 더 만들 수 있고 합칠 수도 있다. 오늘의 배치 말고
        // **두 모듈이 있다는 것**이 붙들어 두는 것만 묻는다: 둘 다 한 장이라도 보이는가.
        for (m in listOf("core", "intellij")) assertTrue(
            kt.any { "${File.separator}$m${File.separator}src${File.separator}main${File.separator}" in it.path },
            "`$m` 의 main 소스가 한 장도 안 보인다(${root.absolutePath}) — 훑어서 «없음»을 " +
                "확인하는 규칙들이 전부 빈 목록을 보고 초록이 된다")
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

        // 자리가 라벨에서 **제목표시줄**로 갔다(수준은 창이 무엇인지 말하는 자리로 — 그 판은
        // 거의 안 변하는 큰 한 줄로 전사 위를 먹고 있었다). 재는 것은 같다: 수준이 지나는 손이
        // `title` 하나이고, 그 손을 부르는 자리가 [Level] 만 받는 문 안 하나뿐인가.
        val door = "private fun say(l: Level)"
        val writes = Regex("""\btitle\(""").findAll(code).count()
        assertTrue(writes == 1 && door in code && "title(" in code.substringAfter(door),
            "제목에 글자를 넣는 자리가 $writes 이고 문은 `$door`" +
                (if (door in code) "" else " — 그 문이 없다") + ". 하나여야 하고 그 안이어야 한다. " +
                "문이 늘거나 `Level` 이 아닌 것을 받게 되면 「수준만 쓴다」를 지키는 것이 다시 " +
                "주석뿐이 된다")

        // `.state` 는 예외다 — 와이어 필드(RosterRow.state)의 이름이라 점 뒤에 정당하게 선다.
        // 이 그물이 잡는 것은 점 없는 식별자, 즉 옛 라벨 `state` 의 부활이다.
        assertTrue("say(title" !in code && Regex("""(?<![.\w])state\b""").findAll(code).count() == 0,
            "수준의 옛 자리(state 라벨)가 돌아왔거나 문이 글자를 받게 됐다. 사건은 `report()` 로 " +
                "전사에, 수준은 `say(Level)` 로 제목에 — 자리를 가른 사유가 둘의 KDoc 에 있다")
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
        // 비움의 자리가 한 번 더 옮겨 갔다: began → follow. 커서(since)가 서면서 재생이 증분이
        // 됐고, 「전량이 온다(=비워야 한다)」를 아는 것은 since==null 판정을 내리는 follow 뿐이다.
        // 계약의 불변부는 그대로다 — **프레임은 절대 비우지 않는다**(프레임이 안 오는 전사 —
        // 데몬 재시작 뒤의 보통 경로 — 를 영영 못 비운다), 그리고 비움과 전량-수신은 한 판정에서
        // 나온다(갈라지면 두 벌이 쌓이거나 증분이 지워진다).
        assertTrue(body("private fun follow()", "private fun lost(").contains("shaper.clear()"),
            "전량 재생을 여는 자리(follow, since==null)가 셰이퍼를 안 비운다 — 대화가 두 벌 쌓인다")
        assertTrue(!body("override fun frame(", "override fun note(").contains("shaper.clear()"),
            "프레임이 셰이퍼를 비운다. 그러면 프레임이 하나도 안 오는 전사에서 지난 대화가 그대로 " +
                "서 있는다. 비움은 전량-수신을 판정하는 follow 에 건다")
        assertTrue(!body("override fun began()", "override fun frame(").contains("shaper.clear()"),
            "began 이 다시 비운다 — 커서로 이어 받는 증분 재접속에서 이미 그린 대화를 지운다")
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

        // 사실 판이 설정 화면으로 접히면서(사용자 결정 2026-08-29) 이 규칙의 자리도 옮겨 갔다.
        // 3초 타이머 단언은 함께 떠나지 않았다 — 설정 화면은 모달이라 열림이 곧 reset() 이고,
        // 상시로 다시 묻는 자리는 상태 표시줄 하나만 남는다(위의 unreachable 단언이 그쪽이다).
        val facts = sources.first { it.name == "MagiConfigurable.kt" }.readText()
        val refresh = facts.substringAfter("override fun reset() {").substringBefore("}")
        assertTrue("sayOutside()" in refresh,
            "그리기 문이 밖 루트 줄을 안 다시 쓴다. 생성 때 한 번 쓰면 고쳐도 안 지워진다")
        val outside = facts.substringAfter("private fun sayOutside() {").substringBefore("\n    }")
        assertTrue("say(outside, \" \")" in outside,
            "밖 루트가 없을 때 지우는 갈래가 없다. 안 쓰는 것으로 지움을 흉내내면 처음 한 번만 맞는다")
    }

    @Test
    fun `문을 좁힌 자리는 다시 열리는 순간을 잡는다`() {
        // 위의 시험이 「문이 한 번만 열린다」를 붙든다면 이건 그 반대편이다 — **문을 일부러 좁힌**
        // 자리. 좁히는 것 자체는 옳다(접어 둔 판이 소켓을 두드리면 안 되고, 사람이 치는 동안 매
        // 글자마다 모델을 부르면 안 된다). 결함은 좁힌 문이 **다시 열려야 하는 순간**을 안 잡는 것이다.
        //
        // 자리 둘, 방향이 반대다.
        //
        // 하나 — 사실 판은 `isVisible` 로 좁혔다. 접힌 동안 낡는 것은 아무도 안 보니 결함이 아니고,
        // 결함은 **보기 시작하는 순간**에 있다: 한 시간 접어 뒀다 펴면 다음 틱까지 최대 3초 동안
        // 한 시간 전 사실이 「지금」으로 서 있다.
        //
        // 둘 — 거들기는 400ms 로 좁혔다. 여기서는 좁힌 쪽이 **묻기**인데 거두는 쪽이 같이 안 움직였다.
        // 치면 타이머만 다시 돌고 화면의 제안은 한 글자 전 앞머리로 만든 것이 그대로 섰다. 그리고 그
        // 줄은 `Tab` 이라고 적힌 **시키는 문장**이라, 시킨 대로 누르면 안 맞는 글자가 붙는다. 낡은
        // 답을 안 붙이는 문지기는 있었지만 그건 늦게 온 답을 막을 뿐 이미 선 제안은 아무도 안 거뒀다.
        //
        // 규칙 한 줄로: **다시 묻는 것과 지금 답을 거두는 것은 한 사건이다.**
        // 사실 판 절반은 설정 화면으로 접히며 규칙째 사라졌다 — 열림=reset 이라 「펴는 종」이
        // 구조에 들어 있고, 소스 글자로 잴 자리가 없다. 거들기 절반만 남는다.
        val chat = sources.first { it.name == "MagiToolWindow.kt" }.readText()
        val listener = chat.substringAfter("addDocumentListener").substringBefore("registerKeyboardAction")
        assertTrue("dropSuggestion()" in listener,
            "앞머리가 바뀌는데 선 제안을 안 거둔다. 그 줄은 `Tab` 이라고 시키는 문장이라 " +
                "시킨 대로 누르면 지금 앞머리에 안 맞는 글자가 붙는다")
    }

    @Test
    fun `열어 둔 버퍼는 타이핑마다 새로 보내고 닫힐 때 지운다`() {
        // 위의 둘과 같은 병인데 이번엔 화면이 아니라 **선을 넘어** 모델의 맥락으로 간다.
        //
        // 이 문이 파는 것은 「저장 안 한 내용」이다. 그런데 한때 `selectionChanged` 에서만 보냈다 —
        // 저장 안 한 내용이 **생기는** 사건은 타이핑인데 문은 탭 바꾸기에 달려 있었다. 그래서 보낸
        // 값은 탭을 바꾼 순간의 스냅샷이고, 한 파일에서 십 분 고치면 모델은 십 분 전 글자를 「사람이
        // 지금 편집 중인 것」으로 읽는다. 맥락에서 가장 최근 자리를 차지하는 슬롯에서.
        //
        // 낡음을 막는 코어 장치가 이 클라이언트를 안 전제한 것이 더 나쁜 쪽이다. `ambientTTL` 이
        // 15분인 근거가 주석에 "editing re-pushes every 600ms" 라고 적혀 있는데, 그건 콘솔에 대해
        // 참이고 이쪽에 대해 거짓이었다. **남의 안전장치가 내 전제를 안 지키면 그건 내 것이 아니다.**
        //
        // 나머지 반은 지우는 갈래가 아예 없던 것. 코어 주석이 그 없음을 이미 이름으로 부르고 있었다
        // — "an old console that never learned to clear". 탭을 닫아도 버퍼는 TTL 이 끊을 때까지
        // 세션의 남은 턴마다 「사람이 편집 중인 파일」로 실려 갔다.
        val buffer = buffer()
        assertTrue("documentChanged" in buffer,
            "타이핑에 안 걸려 있다. 탭 전환에서만 보내면 보내는 값이 「지금 버퍼」가 아니라 " +
                "탭을 바꾼 순간의 스냅샷이고, 코어의 TTL 이 근거로 삼는 재밀기도 안 일어난다")

        // 닫힘을 받는 것과 받아서 지우는 것은 다른 말이라 둘을 따로 묻는다. `substringAfter` 는
        // 구분자가 없으면 **원본을 통째로** 돌려주므로, 먼저 안 물으면 아래 검사가 파일 전체를
        // 뒤져 우연히 초록이 되거나 우연히 빨개진다 — 맞는 답이어도 근거가 딴것이다.
        assertTrue("fun fileClosed" in buffer,
            "탭이 닫히는 것을 아예 안 듣는다")
        val closed = buffer.substringAfter("fun fileClosed").substringBefore("private fun listen")
        assertTrue("\"\")" in closed,
            "탭이 닫혀도 데몬의 사본을 안 지운다. 빈 텍스트가 「닫혔다」인데 그걸 안 보내면 " +
                "떠난 파일이 세션의 남은 턴마다 「편집 중인 파일」로 실려 간다")
    }

    @Test
    fun `빌려 쓰는 안전장치의 전제가 움직이면 여기서 운다`() {
        // 위 시험이 「보내긴 보내는가」를 붙든다면 이건 **얼마나 자주**를 붙든다. 그리고 그 수를
        // 고른 것은 이 저장소의 이쪽이 아니다.
        //
        // 코어는 밀어 둔 버퍼를 `ambientTTL` 뒤에 조용히 버린다. 15분이 정당한 근거로 주석에 적혀
        // 있는 것이 **"editing re-pushes every 600ms"** 다. 즉 코어의 안전장치는 「살아 있는
        // 에디터라면 그 안에 다시 민다」를 전제로 서 있고, 이 플러그인의 디바운스가 그 전제를 지키는
        // 쪽이다. 콘솔을 보고 고른 수가 아니라 **콘솔과 같아야 하는 수**다.
        //
        // 위험한 것은 어긋나는 방향이 아니라 어긋나는 게 **안 보이는** 것이다. 코어가 저 수나 저
        // 문장을 바꿔도 이 모듈은 컴파일되고 시험도 다 통과하고 화면도 멀쩡하다. 드러나는 자리는
        // 사람이 십 분째 고치던 파일이 모델에게서 사라지는 순간뿐이고, 그건 아무도 결함으로 안
        // 읽는다. 남의 안전장치가 내 전제를 안 지키면 그건 내 것이 아니다 — 그러니 잰다.
        //
        // **늦게 운다는 것도 같이 적어 둔다.** `test-jetbrains.yml` 은 `clients/jetbrains/**` 에만
        // 걸리므로 코어만 고친 커밋에서는 안 돈다. 듣는 사람은 이 파일을 다음에 건드리는 사람이다 —
        // 안 듣는 것보다 낫고, 「코어가 바꾸는 순간 운다」로 읽으면 틀린다.
        val root = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .firstOrNull { File(it, "go.mod").isFile }
        assertTrue(root != null,
            "저장소 뿌리(`go.mod`)를 못 찾았다. 플러그인만 떼어 내 빌드하면 이 짝을 잴 수가 없고, " +
                "그때 이 시험이 말하는 것은 「지킨다」가 아니라 「못 봤다」이다")

        val core = File(root, "internal/app/complete.go")
        assertTrue(core.isFile, "코어의 `complete.go` 가 없다(${core.path})")
        val premise = Regex("re-pushes every (\\d+)ms").find(core.readText())
        assertTrue(premise != null,
            "`ambientTTL` 의 근거 문장이 없어졌다. 15분이 무엇을 믿고 고른 값인지 코어가 더 이상 " +
                "안 적으면, 이쪽 디바운스가 무엇에 맞춘 수인지도 같이 근거를 잃는다")

        val mine = Regex("Timer\\((\\d+)\\)").find(buffer())
        assertTrue(mine != null, "`OpenBufferListener` 에 디바운스 타이머가 없다")
        assertTrue(mine!!.groupValues[1] == premise!!.groupValues[1],
            "재밀기 주기가 코어의 전제와 다르다 — 이쪽 ${mine.groupValues[1]}ms, 코어가 믿는 값 " +
                "${premise.groupValues[1]}ms. 둘 중 하나를 고치든 `ambientTTL` 을 다시 논하든, " +
                "조용히 갈라지게 두는 것만 안 된다")
    }

    @Test
    fun `재생으로 또 오는 신호는 이벤트에서 그리지 않는다`() {
        // `Transcript.movesPrompt` 의 넷 중 `permission.decided` 는 전이가 아니라 사실이라
        // 저장되고, 창이 다시 붙을 때마다 재생으로 또 온다(실측: 저장분 69 벌 중 여섯 벌,
        // 가장 많은 것이 여덟). 그래도 화면이 안 틀리는 것은 받는 쪽이 그걸 **신호로만** 쓰기
        // 때문이다 — 그릴 값은 그때 데몬에게 새로 묻는다.
        //
        // 그 줄이 `e` 의 내용으로 그리기 시작하면 재생이 지나간 물음을 지금 것으로 그린다:
        // 붙어 있던 창과 나중에 다시 붙은 창이 같은 대화를 다르게 그리는, `echoesFact` 로 이미
        // 한 번 고친 그 결함이다. 컴파일러는 이걸 안 잡는다 — `e` 를 읽는 것은 그 자체로 완벽히
        // 올바른 코드이고, 틀린 것은 **그 값이 언제 오느냐**인데 그건 타입에 안 적혀 있다.
        //
        // 지금 이 조건은 주석으로만 서 있었다. 맞는 답이 맞는 근거로 맞고 있는 자리라, 다음
        // 사람이 근거를 모른 채 옳은 방향으로 손대면 답이 틀려진다.
        //
        // **부르는 자리를 하나로 안 좁힌다.** 오늘 부르는 곳이 하나인 것은 사실이지 규칙이
        // 아니고, 규칙을 어길 자리는 보통 **새로 생기는 자리**다. 첫 매치만 보면 그 줄 밑에 한
        // 줄을 더 붙이는 것만으로 빠져나간다. 시험 소스는 뺀다 — 거기서 이 술어를 부르는 것은
        // 술어 자체를 재는 일이라 이 규칙의 대상이 아니다.
        val calls = sources.filter { "${File.separator}main${File.separator}" in it.path }
            .flatMap { f ->
                Regex("movesPrompt\\(\\w+\\)\\)(.*)").findAll(f.readText())
                    .map { f.name to it.groupValues[1].trim() }.toList()
            }
        assertTrue(calls.isNotEmpty(),
            "`movesPrompt` 를 쓰는 자리를 못 찾았다. 없앤 것이면 이 시험도 같이 지우고, 옮긴 " +
                "것이면 옮긴 자리를 보게 고쳐라 — 못 찾은 것을 통과로 읽지 않는다")
        val stray = calls.filter { it.second != "refresh()" }
        assertTrue(stray.isEmpty(),
            "재생으로 또 오는 신호에서 `refresh()` 말고 다른 것을 한다: " +
                stray.joinToString(", ") { "${it.first} 의 `${it.second}`" } + ". 그 자리에서 " +
                "`e` 를 읽으면 지나간 물음을 지금 것으로 그린다. 정말 필요하면 재생분과 " +
                "라이브분을 먼저 갈라라 — 지금 `Sink.frame` 은 그 둘을 구분해 주지 않는다")
    }


    @Test
    fun `소켓 길이 한계는 코어와 같은 수 같은 부등호로 갈린다`() {
        // `SocketPath.tooLong` 은 포트다 — 그 KDoc 이 "daemon.go 의 `tooLong`" 이라고 적고 있다.
        // 포트에서 조용히 갈라지는 것은 수만이 아니라 **부등호**이기도 하다. 코어가 실제 한계
        // (macOS 104·리눅스 108)로 올리면 이쪽만 100 에서 막아 데몬이 잘 여는 경로를 두고
        // "더 짧은 데로 옮겨라"라고 하고, 부등호만 갈라지면 **딱 그 바이트짜리 경로 하나**에서
        // 둘의 대답이 다르다. 어느 쪽이든 컴파일되고 시험도 통과하고 화면도 멀쩡하다.
        //
        // 늦게 우는 것은 `600ms` 짝과 같다: `test-jetbrains.yml` 은 `clients/jetbrains/**` 에만
        // 걸려서 코어만 고친 커밋에서는 안 돈다. 듣는 사람은 이 모듈을 다음에 건드리는 사람이고,
        // 그건 안 듣는 것보다는 낫다 — 「코어가 바꾸는 순간 운다」로 읽으면 틀린다.
        val root = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .firstOrNull { File(it, "go.mod").isFile }
        assertTrue(root != null, "저장소 뿌리(`go.mod`)를 못 찾았다 — 이 시험이 말하는 것은 " +
            "「지킨다」가 아니라 「못 봤다」이다")

        val go = File(root, "internal/adapter/daemon/daemon.go").readText()
        val theirs = Regex("""maxSocketPath\s*=\s*(\d+)""").find(go)
        assertTrue(theirs != null, "코어의 `maxSocketPath` 가 없어졌다. 옮겨 간 것이면 이 시험도 " +
            "같이 옮겨라 — 못 찾은 것을 통과로 읽지 않는다")

        val mine = sources.first { it.name == "SocketPath.kt" }.readText()
        val ours = Regex("""MAX_SOCKET_PATH\s*=\s*(\d+)""").find(mine)
        assertTrue(ours != null, "이쪽 `MAX_SOCKET_PATH` 를 못 찾았다")
        assertTrue(theirs!!.groupValues[1] == ours!!.groupValues[1],
            "한계가 갈라졌다 — 이쪽 ${ours.groupValues[1]}, 코어 ${theirs.groupValues[1]}. " +
                "한쪽만 올리면 다른 쪽이 멀쩡한 경로를 거절하거나 못 여는 경로를 통과시킨다")

        // 부등호까지 본다. 수가 같아도 한쪽이 `<` 면 딱 그 길이에서 대답이 갈린다.
        assertTrue(Regex("""len\(path\)\s*<=\s*maxSocketPath""") in go,
            "코어가 경계를 여는 자리를 `<=` 로 안 쓴다. 부등호가 갈라지면 딱 그 바이트에서만 " +
                "틀리고, 그건 아무도 안 밟는 자리다")
        assertTrue(Regex("""n\s*<=\s*MAX_SOCKET_PATH""") in mine,
            "이쪽이 경계를 여는 자리를 `<=` 로 안 쓴다")
    }
    /**
     * 라벨에 남의 글자를 **안 거르고** 붙이는 자리를 잡는다.
     *
     * 스윙 라벨은 여는 태그로 시작하면 안을 마크업으로 읽는다. `rm x && echo <done>` 의
     * `<done>` 은 태그로 먹혀 사라지고, 사람은 짧아진 글을 보고 「허용」을 누르거나 Tab 을
     * 누른다 — **보이는 것과 정해지는 것이 다른 창**이다.
     *
     * **이 시험이 있는 이유가 그것 자체다.** `Markup.text` 를 만들고 권한 물음을 고친 다음
     * 「거쳐야만 들어가는 문을 뒀다」고 적었는데, 같은 저장소에 **안 거치는 라벨이 그 순간에도
     * 둘 더 서 있었다**(모델이 지은 제안, 컨텐트 루트 경로). 손으로 부르는 함수는 문이 아니라
     * 습관이고, 습관은 다음 사람에게 안 전해진다.
     *
     * **이건 트립와이어지 증명이 아니다.** 진짜 문은 타입이다 — 라벨 대입이 `String` 대신
     * 「이미 거른 것」만 받으면 안 거른 것은 **적을 수가 없다.** 그건 아직 안 만들었다. 여기서
     * 재는 것은 소스 글자뿐이고, 못 재는 것은 아래 시험에 **돌아가는 갈래로** 적어 뒀다 —
     * 주석으로 적으면 그물이 넓어져도 아무도 안 지운다.
     */
    @Test
    fun `라벨에 붙는 남의 글자는 거쳐야 한다`() {
        // 「훑어서 없음」은 잴 것이 없어도 초록이다. 그러니 먼저 **이 그물에 걸릴 것이 실제로
        // 있는지**를 묻는다: 거르는 함수를 지운 셈 치면 어딘가는 울어야 한다.
        assertTrue(sources.any { labelLeaks(it.name, it.readText().replace("Markup", "")).isNotEmpty() },
            "라벨을 한 장도 못 찾았다 — 이 시험이 아무것도 안 보고 있다(옮겼으면 같이 옮겨라)")
        val leaks = sources.flatMap { labelLeaks(it.name, it.readText()) }
        assertTrue(leaks.isEmpty(), "라벨에 안 거르고 붙는 자리: $leaks")
    }

    /**
     * 그물이 실제로 우는지, 그리고 **어디서 안 우는지**를 못 박는다.
     *
     * 위 시험은 저장소가 깨끗하면 초록인데, 규칙 하나를 통째로 눌러도 똑같이 초록이었다(실측).
     * 규칙마다 가짜 소스를 한 장씩 먹여야 그 규칙이 서 있다는 말이 검사가 된다.
     *
     * 마지막 갈래가 이 그물의 **경계**다. 거르기가 라벨 밖에서 일어나면 여기선 안 보인다 —
     * 못 고치는 것이 아니라 **소스 글자로는 못 재는 것**이고, 그래서 [safeInLabels] 가
     * 구멍을 메우는 대신 근거를 말로 남긴다.
     */
    @Test
    fun `라벨 그물이 우는 자리와 안 우는 자리`() {
        // 여는 태그를 통째로 안 적고 이어 붙인다 — 통째로 적으면 이 파일이 자기 규칙에 걸리고,
        // 이 파일만 빼 주면 규칙에서 빠져나갈 구멍이 하나 생긴다(위 달러 규칙과 같은 이유).
        val open = "\"" + "<" + "html>"
        // **가짜 소스마다 우는 규칙이 하나여야 한다.** 처음엔 이 주석 줄이 없었는데, 그러면
        // 「이 파일이 거르는 함수를 모른다」가 모든 갈래에서 같이 울어 **다른 규칙의 울음을
        // 가렸다** — 규칙 셋을 통째로 눌러도 시험은 초록이었다. 규칙이 서 있다는 말이 검사가
        // 되려면 그 규칙 말고는 울 것이 없어야 한다.
        fun leaks(inner: String) = labelLeaks("Fake.kt", "// Markup\nval x = $open$inner</html>\"")

        assertTrue(leaks("\${cmd}").isNotEmpty(), "안 거른 중괄호 보간을 놓쳤다")
        assertTrue(leaks("\$cmd").isNotEmpty(), "안 거른 이름 보간을 놓쳤다")
        assertTrue(leaks("\" + xs.joinToString(\"<br/>\") + \"").isNotEmpty(),
            "원소를 안 거르고 이어 붙이는 `joinToString` 을 놓쳤다")
        assertTrue(leaks("\" + xs.joinToString(\"<br/>\") { Markup.text(it) } + \"").isEmpty(),
            "원소를 거쳐 붙였는데 울었다")
        assertTrue(leaks("\" + cmd + \"").isNotEmpty(), "이름을 그대로 이어 붙이는 꼴을 놓쳤다")
        assertTrue(leaks("ok").isEmpty(), "붙이는 것이 없는 라벨까지 울면 사람이 이 시험을 끈다")
        assertTrue(leaks("\${Markup.text(cmd)}").isEmpty(), "거친 것을 붙였는데 울었다")

        // 거른 것으로 **시작**하기만 하면 뒤에 뭘 붙여도 통과하던 자리. 순서만 바꾼 쪽은
        // 잡히는데 이쪽은 안 잡히는 비대칭이라, 다음 사람이 어느 쪽을 쓸지가 운이었다.
        assertTrue(leaks("\${Markup.text(a) + raw}").isNotEmpty(),
            "거른 것 뒤에 이어 붙인 날것을 놓쳤다")
        // 중괄호가 겹치면 정규식이 안쪽에서 잘리고, 잘린 **뒤쪽이 통째로** 그물 밖이 된다.
        assertTrue(leaks("\${a.let { b }}\$raw").isNotEmpty(), "겹친 중괄호 뒤의 보간을 놓쳤다")

        // 규칙 하나씩 서 있는지 보려면 **그 규칙만 걸리는** 소스가 하나씩 있어야 한다. 아래는
        // 붙는 것이 근거 있는 이름뿐이라, 우는 이유가 「이 파일이 거르는 함수를 아예 모른다」
        // 하나로 좁혀진다.
        val fake = mapOf("Fake.kt:ok" to Safe("시험용", emptyList()))
        assertTrue(labelLeaks("Fake.kt", "val x = $open\$ok</html>\"", fake).isNotEmpty(),
            "라벨에 붙이면서 `Markup` 를 모르는 파일을 놓쳤다")
        assertTrue(labelLeaks("Fake.kt", "// Markup\nval x = $open\$ok</html>\"", fake).isEmpty(),
            "근거 있는 이름만 붙었는데 울었다")
        // 근거는 **그 파일에 대한** 주장이다. 열쇠가 이름뿐이면 옆 파일의 같은 이름이 남의
        // 근거로 축복받는다.
        assertTrue(labelLeaks("Other.kt", "// Markup\nval x = $open\$ok</html>\"", fake).isNotEmpty(),
            "옆 파일의 같은 이름이 남의 근거로 통과했다")

        // 라벨은 한 줄로 안 끝난다. 여는 태그 줄만 보면 **둘째 줄에 붙는 것**이 통째로 안 보이고,
        // 안 보이는 것은 위반이 없는 것과 화면에서 같아 보인다.
        assertTrue(labelLeaks("Fake.kt", "// Markup\nval x = $open\" +\n    cmd + \"</html>\"")
            .isNotEmpty(), "여는 태그 다음 줄에 붙는 것을 놓쳤다")

        // **여기가 경계다.** 거르기를 윗줄에서 해 두고 라벨엔 이름만 놓으면, 거른 것인지가 라벨에
        // 안 적혀 있어 이 그물은 못 가른다. 그래서 그런 이름은 통과시키지 않고 근거를 요구한다.
        assertTrue(labelLeaks("Fake.kt", "val safeBit = Markup.text(cmd)\nval x = ${open}\$safeBit</html>\"")
            .isNotEmpty(), "이름만 놓인 것을 근거 없이 통과시켰다")
    }

    /**
     * 뽑는 것과 판정하는 것을 **따로 잰다.**
     *
     * 그물 전체(`labelLeaks`)로는 이 둘이 안 갈린다 — 뽑기가 잘려도, 판정이 헐거워도, 옆 규칙이
     * 대신 울어서 목록은 여전히 비어 있지 않다. 「울었으니 됐다」로 읽으면 **다른 줄이 울고 있는
     * 것**이고, 그 사이 이 두 줄은 죽어도 아무 말이 없다(실측: 둘 다 눌러도 초록이었다).
     */
    @Test
    fun `보간을 뽑는 것과 거른 것을 가리는 것`() {
        // 겹친 중괄호에서 잘리면 앞의 것이 짧아지고 **뒤의 것이 통째로** 없어진다. 목록을 통째로
        // 못 박아야 「몇 개 나왔나」가 아니라 「무엇이 나왔나」가 검사가 된다.
        assertEquals(listOf("a.let { b }", "raw"), interpolations("\${a.let { b }}\$raw"))
        // 안 닫힌 채 끝나면 남은 것을 통째로 내놓는다. 조용히 버리면 「없다」와 같아진다.
        assertEquals(listOf("a.let { b"), interpolations("\${a.let { b"))

        assertTrue(escaped("Markup.text(it)"), "거친 것을 아니라고 했다")
        assertTrue(escaped("Markup.text(f(a))"), "안에 괄호가 있다고 아니라고 했다")
        // 앞머리만 보면 이 줄이 통과한다. 거른 것으로 **시작**하기만 하면 뒤는 자유였다.
        assertFalse(escaped("Markup.text(a) + raw"), "거른 것 뒤에 이어 붙인 날것을 축복했다")
        assertFalse(escaped("Markup.text(a).plus(raw)"), "거른 것에 이어 붙인 날것을 축복했다")
        assertFalse(escaped("raw + Markup.text(a)"), "날것으로 시작하는데 통과시켰다")
    }

    /**
     * 라벨에서 안 거치고 붙는 자리들. 파일 하나의 글자만 보고 판정한다.
     *
     * [safe] 를 받는 이유는 시험 때문이다 — 규칙 하나만 걸리는 가짜 소스를 만들려면 그 소스에
     * 맞는 근거 목록을 같이 먹여야 한다. 프로덕션 호출자는 기본값을 쓴다.
     */
    private fun labelLeaks(name: String, text: String, safe: Map<String, Safe> = safeInLabels): List<String> {
        val lines = text.lines()
        val head = "\"" + "<" + "html>"
        val spans = mutableListOf<Pair<Int, String>>()
        var i = 0
        while (i < lines.size) {
            if (head in lines[i]) {
                var j = i
                while (j < lines.size - 1 && "</html>" !in lines[j]) j++
                spans += (i + 1) to lines.subList(i, j + 1).joinToString("\n")
                i = j + 1
            } else i++
        }
        // 붙이는 것이 없는 라벨은 이 규칙의 대상이 아니다. 글자를 다 이 파일이 지었으면 뭉갤
        // 남의 글자가 없다.
        val open = spans.filter { (_, s) -> "\$" in s || "joinToString(" in s || bareConcat(s).any() }
        if (open.isEmpty()) return emptyList()

        val out = mutableListOf<String>()
        // 첫째: 라벨에 남을 붙이는 파일은 거르는 함수를 알아야 한다. 새 파일이 생기는 것이
        // 제일 흔한 모양이고, 그때 이 줄이 운다.
        if ("Markup" !in text) out += "$name: 라벨에 붙이면서 `Markup` 를 한 번도 안 쓴다"
        for ((at, span) in open) {
            // 둘째: 끼워 넣는 것은 거른 것이어야 한다. 중괄호 꼴과 이름 꼴을 **둘 다** 본다 —
            // 처음엔 앞의 것만 봤는데, 그러면 이 저장소가 이미 쓰고 있는 뒤의 꼴이 통째로 그물
            // 밖이라 「검사한다」는 말이 절반만 참이 된다.
            interpolations(span).map { it.trim() }
                .filterNot { escaped(it) || "$name:$it" in safe }
                .forEach { out += "$name:$at: \$$it" }
            // 셋째: 여럿을 이어 붙이는 자리. 원소 하나가 남의 글자면 하나짜리와 같은 결함이다.
            if ("joinToString(" in span &&
                !Regex("""joinToString\([^)]*\)\s*\{[^}]*Markup\.text\(""").containsMatchIn(span)
            ) out += "$name:$at: `joinToString` 이 원소를 안 거르고 붙인다"
            // 넷째: 보간 말고 **이어 붙이는** 꼴. `"<b>" + cmd + "</b>"` 는 보간과 하는 일이
            // 같은데 글자 모양만 다르다. 이 갈래를 안 보면 다음 사람이 쓰는 꼴에 따라 그물이
            // 있다 없다 한다.
            bareConcat(span).filterNot { "$name:$it" in safe }
                .forEach { out += "$name:$at: + $it" }
        }
        return out
    }

    /**
     * 라벨 안에 끼워 넣는 것들을 뽑는다. **중괄호를 세면서 간다.**
     *
     * 정규식 `\$\{([^}]*)\}` 로 쓰면 안쪽 중괄호에서 잘린다 — `\${x.let { ... }}` 같은 줄이
     * 생기는 순간 그 뒤가 통째로 그물 밖이 되고, 그물이 없어진 것은 위반이 없는 것과 화면에서
     * 같아 보인다. 오늘 라벨에 람다가 없다는 것은 안전의 근거가 아니라 **아직 안 썼다**이다.
     */
    private fun interpolations(span: String): List<String> {
        val out = mutableListOf<String>()
        var i = 0
        while (i < span.length) {
            if (span[i] != '\$') { i++; continue }
            if (i + 1 < span.length && span[i + 1] == '{') {
                var depth = 0
                var j = i + 1
                while (j < span.length) {
                    if (span[j] == '{') depth++
                    if (span[j] == '}') { depth--; if (depth == 0) break }
                    j++
                }
                // 안 닫힌 채 줄이 끝나면 남은 것을 통째로 내놓는다. 조용히 버리면 「없다」가 된다.
                if (j >= span.length) { out += span.substring(i + 2); break }
                out += span.substring(i + 2, j)
                i = j + 1
            } else {
                val m = Regex("""^[A-Za-z_][A-Za-z0-9_.]*""").find(span.substring(i + 1))
                if (m == null) i++ else { out += m.value; i += 1 + m.value.length }
            }
        }
        return out
    }

    /**
     * 이 표현이 **통째로** 거른 것인가.
     *
     * 앞머리만 보면(`startsWith`) `\${Markup.text(a) + raw}` 가 축복받는다 — 거른 것으로
     * 시작하기만 하면 뒤에 뭘 붙여도 통과라, `\${raw + Markup.text(a)}` 는 잡히는데 순서만
     * 바꾸면 안 잡히는 비대칭이 된다. 다음 사람이 어느 쪽을 쓸지는 운이다. 그래서 여는 괄호의
     * **짝이 마지막 글자**여야 한다고 본다.
     */
    private fun escaped(expr: String): Boolean {
        val call = "Markup.text("
        if (!expr.startsWith(call)) return false
        var depth = 0
        for ((k, ch) in expr.withIndex()) {
            if (ch == '(') depth++
            if (ch == ')') {
                depth--
                if (depth == 0) return k == expr.length - 1
            }
        }
        return false
    }

    /** 라벨에 함수 호출이 아니라 **이름 그대로** 이어 붙는 것들. */
    private fun bareConcat(span: String): List<String> =
        Regex("""\+\s*([A-Za-z_][A-Za-z0-9_.]*)\s*(?![\w(.])""").findAll(span)
            .map { it.groupValues[1] }.toList()

    /**
     * 근거 하나. [why] 는 사람이 읽는 말이고, [anchors] 는 **그 말이 참인 동안 그 파일에 글자
     * 그대로 남아 있어야 하는 조각**이다.
     *
     * 산문만 두면 근거가 반증 불가능해진다. 「바로 위에서 거쳐 붙인다」고 적어 둔 다음 누가 그
     * 위를 정리해 버리면, 그 이름은 목록에 없는 것보다 **나쁘다** — 시험이 적극적으로 보증해
     * 주니까. 안 적힌 구멍은 언젠가 걸리고 축복받은 구멍은 안 걸린다.
     */
    private data class Safe(val why: String, val anchors: List<String>)

    /**
     * 라벨에 이름만 놓여도 되는 것들. 열쇠는 **파일:이름**이다.
     *
     * 이름만으로 열쇠를 삼으면 근거가 저장소 전체에 걸린다 — `MagiToolWindow` 를 보고 적은
     * 근거로 내일 다른 파일의 같은 이름이 축복받는다. 근거는 그 파일에 대한 주장이므로 열쇠도
     * 그 파일까지다.
     *
     * 이 목록의 항목은 검사가 아니라 **주장**이고, [anchors] 가 그 주장을 반증 가능하게 만든다.
     */
    private val safeInLabels = mapOf(
        "MagiConfigurable.kt:out.size" to Safe(
            "수다 — 글자가 아니라 마크업을 실을 수가 없다",
            listOf("val out = workspace.rootsOutsideWorkspace()"),
        ),
        "MagiToolWindow.kt:at" to Safe(
            "이 파일이 지은 `(2/3)` 꼴, 안이 다 수다",
            listOf("val at = if (w.total > 1)"),
        ),
        "MagiToolWindow.kt:subject" to Safe(
            "바로 위에서 `Markup.text` 로 지어 붙인 조각이다",
            listOf(
                "sub.args?.let { \"<tt>\${Markup.text(it)}</tt>\" }",
                "sub.reason?.let { Markup.text(it) }",
            ),
        ),
        "MagiToolWindow.kt:why" to Safe(
            "바로 위에서 `Markup.text` 로 지어 붙인 조각이다",
            listOf("?.why?.let { \"<br/><i>\${Markup.text(it)}</i>\" }"),
        ),
    )

    /**
     * 근거가 아직 참인지 잰다.
     *
     * 이 시험이 없으면 [safeInLabels] 의 산문은 **다른 소스에 대한 검사 안 되는 주장**이다.
     * 하필 거기 앉은 이름들(`subject`·`why`)이 데몬에서 온 남의 글자를 싣는 권한 창의 값이라,
     * 강제가 제일 약한 칸이 값이 제일 비싼 줄에 앉아 있었다.
     */
    /**
     * **dispose 는 클래스를 처음 로드하는 자리가 되면 안 된다.**
     *
     * 이 자리는 IDE 가 나갈 때도 돌고, 그때 우리 플러그인 클래스로더는 이미 닫혀 있을 수
     * 있다 — 이 세션에서 한 번도 안 건드린 클래스는 그 순간 못 불려 온다. 라이브에서 그대로
     * 났다(`NoClassDefFoundError: …/RichAnswer` at `MagiToolWindow$View.dispose`): 리치 답을
     * 한 번도 안 그린 창을 닫았고, **터진 dispose 가 그 아래 정리를 통째로 걸렀다** — 스트림도
     * 손도 안 거둬졌다. 못 거두는 것보다 나쁜 것은 못 거두면서 나머지까지 데려가는 것이다.
     *
     * 그 자리 바로 옆줄은 이미 `runCatching` 이었다. **같은 부재를 옆에서 다르게 적어 두면
     * 안 감싼 쪽이 터진다** — 그래서 「기억해서 감싼다」가 아니라 여기서 잰다.
     *
     * 재는 것은 `Xxx.yyy(` 한 모양이다 — dispose 본문에서 대문자로 시작하는 이름의 멤버를
     * 부르면 감싸져 있어야 한다. 필드·지역변수 호출은 소문자라 안 걸리고, `runCatching { … }`
     * 안이면 통과다(줄 텍스트가 아니라 **범위**로 판정하므로 여러 줄로 감싸도 된다).
     *
     * **이 초록이 「dispose 는 안전하다」는 뜻은 아니다.** 클래스를 로드시키는 다른 모양은 이
     * 그물을 지난다: 생성자 호출 `Xxx()`, `Xxx.Companion.y()`(점 뒤가 대문자), 상수·프로퍼티
     * 읽기 `Xxx.LOG`, 다른 파일의 최상위 함수(`FooKt` 로드). 넓히기 쉬운 그물이 아니라
     * **실제로 났던 모양**을 막는 그물이라 이렇게 두고, 못 잡는 것을 여기 적어 둔다 — 다음
     * 사람이 이 시험의 초록을 그 이상으로 읽지 않게.
     */
    /**
     * **설정 디렉토리를 정하는 자리는 하나다.**
     *
     * `SocketPath.configDir()` 를 인자 없이 부르면 IDE 프로세스의 environ 을 본다. 그런데
     * Dock·Toolbox 로 띄운 IDE 에는 사람의 셸 설정이 하나도 없어서, 셸에서 `MAGI_CONFIG_DIR`
     * 을 쓰는 기계에서는 그 답이 틀린다 — 창은 A 소켓을 보고 열린 버퍼는 B 소켓으로 가고,
     * 띄운 데몬은 빈 설정 디렉토리에서 뜬다. 실제로 두 자리가 그 모양이었다.
     *
     * 그래서 `intellij` 에서는 [Shell] 만 그 함수를 부른다. 두 벌이 되면 안 재지는 쪽이 갈린다.
     */
    @Test
    fun `설정 디렉토리를 정하는 자리는 하나다`() {
        val mine = sources.filter { "${File.separator}intellij${File.separator}" in it.path }
        fun calls(f: File, withEnv: Boolean) = f.readText().lineSequence().any {
            val bare = it.substringBefore("//")
            "SocketPath.configDir(" in bare && ("env" in bare) == withEnv
        }
        // 재는 것이 실제로 있는지부터. 이 줄이 없으면 아래 「없다」는 규칙이 지켜져서가 아니라
        // **볼 것이 없어서** 초록이 될 수 있다.
        assertEquals(
            listOf("Shell.kt"), mine.filter { calls(it, withEnv = true) }.map { it.name }.sorted(),
            "환경을 넘겨 설정 디렉토리를 정하는 자리가 Shell 이 아니다 — 이 시험이 볼 것을 잃었다",
        )
        assertEquals(
            emptyList<String>(), mine.filter { calls(it, withEnv = false) }.map { it.name }.sorted(),
            "IDE 환경으로 설정 디렉토리를 정하는 자리가 생겼다 — 사람의 셸이 아는 값은 Shell 이 안다",
        )
    }

    @Test
    fun `창을 거두는 자리는 클래스를 처음 로드하지 않는다`() {
        // **식 본문도 센다.** 처음엔 `override fun dispose() {` 만 찾았는데, 이 모듈의 dispose
        // 셋 중 둘이 `= Unit` / `= timer.stop()` 이라 가드 밖이었다 — 라이브 사고 재발을 막으라고
        // 세운 가드가 3분의 1만 보고 있었다(리뷰 R2). 같은 결함을 같은 커밋 안에서 되풀이했다.
        val head = Regex("""override fun dispose\(\)\s*(\{|=)""")
        val call = Regex("""(?<![.\w])[A-Z][A-Za-z0-9_]*\.[a-z][A-Za-z0-9_]*\s*\(""")
        val mine = sources.filter { "${File.separator}intellij${File.separator}" in it.path }
        // 몇 개를 봐야 하는지는 **소스가 말하게** 한다. `> 0` 으로 두면 하나만 봐도 초록이라,
        // 방금 그 함정을 이 시험 자신이 못 잡는다.
        val declared = mine.sumOf { f -> Regex("""override fun dispose\(\)""").findAll(f.readText()).count() }
        var looked = 0
        val bad = mutableListOf<String>()
        for (f in mine) {
            val text = f.readText()
            var m = head.find(text)
            while (m != null) {
                looked++
                val block = text[m.range.last] == '{'
                // 블록이면 중괄호를 세어 끝을 찾고, 식이면 그 줄 하나가 본문이다.
                var i = m.range.last + 1
                if (block) {
                    var depth = 1
                    while (i < text.length && depth > 0) {
                        when (text[i]) { '{' -> depth++; '}' -> depth-- }
                        i++
                    }
                } else {
                    i = text.indexOf('\n', i).let { if (it < 0) text.length else it }
                }
                val bodyText = text.substring(m.range.last + 1, i)
                // **감쌌는지는 줄이 아니라 자리로 판정한다.** 줄 텍스트에 `runCatching` 이 있는지만
                // 보면 여러 줄로 감싼 코드를 위반으로 신고한다 — 맞는 코드를 빨갛게 만드는 가드다
                // (리뷰 R3). 그래서 `runCatching { … }` 의 범위를 먼저 구해 둔다.
                val safe = mutableListOf<IntRange>()
                for (g in Regex("""runCatching\s*\{""").findAll(bodyText)) {
                    var d = 1
                    var j = g.range.last + 1
                    while (j < bodyText.length && d > 0) {
                        when (bodyText[j]) { '{' -> d++; '}' -> d-- }
                        j++
                    }
                    safe += g.range.first until j
                }
                for (c in call.findAll(bodyText)) {
                    if (safe.any { c.range.first in it }) continue
                    val line = bodyText.substring(0, c.range.first).substringAfterLast('\n') +
                        bodyText.substring(c.range.first).substringBefore('\n')
                    if (line.trimStart().startsWith("//")) continue
                    bad += "${f.name}: ${line.trim()}"
                }
                m = head.find(text, i)
            }
        }
        assertTrue(looked > 0, "dispose 를 하나도 못 찾았다 — 이 시험이 아무것도 안 보고 있다")
        assertEquals(
            declared, looked,
            "소스에 dispose 가 $declared 개인데 $looked 개만 봤다 — 훑는 모양이 규칙이 사는 " +
                "모양보다 좁다(식 본문 `= expr` 을 놓치던 그 함정)",
        )
        assertTrue(
            bad.isEmpty(),
            "창을 거두는 자리에서 감싸지 않은 클래스 호출:\n  " + bad.joinToString("\n  ") +
                "\nIDE 가 나가는 중이면 그 클래스는 못 불려 오고, 터진 dispose 는 그 아래 정리를 " +
                "통째로 거른다. `runCatching { … }` 으로 감쌀 것.",
        )
    }

    @Test
    fun `라벨 예외의 근거는 그 파일에 남아 있어야 한다`() {
        val bad = staleAnchors(safeInLabels) { f -> sources.firstOrNull { it.name == f }?.readText() }
        assertTrue(bad.isEmpty(), "라벨 예외의 근거가 낡았다: $bad")

        // **닻 검사가 실제로 우는지도 여기서 잰다.** 위 줄은 저장소가 성하면 초록이고, 검사를
        // 통째로 눌러도 똑같이 초록이다 — 지키는 것이 성할 때 가드가 조용한 것은 정상이라
        // 「돌연변이가 죽나」로는 이 줄이 서 있는지 알 수 없다. 막으려던 상태를 만들어 본다.
        val gone = mapOf("Fake.kt:x" to Safe("사라진 근거", listOf("val x = Markup.text(raw)")))
        assertTrue(staleAnchors(gone) { "// 정리했다" }.isNotEmpty(), "없어진 닻을 못 봤다")
        assertTrue(staleAnchors(gone) { "val x = Markup.text(raw)" }.isEmpty(), "성한 닻에 울었다")
        assertTrue(staleAnchors(gone) { null }.isNotEmpty(), "근거가 가리키는 파일이 없는데 조용했다")
    }

    /** 근거의 닻이 아직 그 파일에 남아 있나. [read] 는 파일 이름을 글자로 바꾼다. */
    private fun staleAnchors(safe: Map<String, Safe>, read: (String) -> String?): List<String> =
        safe.flatMap { (key, s) ->
            val file = key.substringBefore(':')
            val text = read(file)
                ?: return@flatMap listOf("$key: 그런 파일이 없다 — 근거가 가리키는 것이 사라졌다")
            s.anchors.filterNot { it in text }.map { "$key: 근거의 닻이 없어졌다(${s.why}): $it" }
        }

    private fun buffer(): String = sources.first { it.name == "OpenBufferListener.kt" }.readText()

}
