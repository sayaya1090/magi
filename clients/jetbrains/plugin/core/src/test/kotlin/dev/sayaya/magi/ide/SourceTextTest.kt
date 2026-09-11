package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
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

    /**
     * **에디터 폰트로 직접 그리면 한글이 두부가 된다.**
     *
     * `Graphics.drawString` 은 **주어진 폰트 하나로만** 그린다 — 스윙 라벨이나 플랫폼 인레이가
     * 알아서 하는 폰트 폴백이 거기엔 없다. 그리고 IDE 의 기본 에디터 폰트인 JetBrains Mono 에는
     * 한글 글리프가 **하나도** 없다.
     *
     * 실측(2026-09-11, IDE 배포판이 번들한 `JetBrainsMono-Italic.ttf` 를 직접 열어서):
     *
     *     Font.canDisplayUpTo("한글이 깨진다") = 0      ← 첫 글자부터 못 그린다
     *
     * 사용자 보고와 맞는다 — 「에이전트 의견」이 한국어면 통째로 네모였다. 맥의 **시스템**
     * 폰트는 자바가 합성해 주므로 이 기계의 화면으로는 재현되지 않는다.
     *
     * 폭과 그림이 **같은 폰트**여야 하는 것도 같이 붙든다: 하나만 바꾸면 인레이가 제 글자보다
     * 좁거나 넓게 자리를 잡는다.
     */
    @Test
    fun `인레이가 글리프 없는 글자를 대체 폰트로 넘긴다`() {
        val f = sources.first { it.name == "LookInlays.kt" }
        val src = code(f)
        assertTrue("drawString" in src, "인레이가 글자를 안 그린다 — 이 규칙이 없는 것을 잰다")
        assertTrue("getFontWithFallback" in src,
            "에디터 폰트를 그대로 drawString 에 준다 — JetBrains Mono 에 한글 글리프가 없어 두부가 된다")
        assertEquals(
            1, Regex("""private fun font\(""").findAll(src).count(),
            "폰트를 고르는 자리가 여럿이다 — 폭과 그림이 다른 폰트를 쓰게 된다",
        )
        val uses = Regex("""font\(ed\)""").findAll(src).count()
        assertTrue(uses >= 2, "폭이나 그림 중 하나가 그 폰트를 안 쓴다 (font(ed) $uses 곳)")
    }

    /**
     * **다시 붙기까지 기다리는 시간도 계약의 것이다.**
     *
     * 여기 1초·배증·30초가 손으로 적혀 있었고 VS Code 에도 같은 두 수가 따로 적혀 있었다.
     * 게다가 둘 다 **지터가 없어서**, 창을 여럿 연 기계에서는 전부 같은 순간에 다시 붙으러
     * 갔다 — 계약의 ±20% 가 그것을 흩으려고 있는 것이다.
     *
     * 좁게 잰다: 재접속 고리가 `Backoff` 를 지나는가, 그리고 제 숫자를 다시 들지 않는가.
     */
    @Test
    fun `다시 붙는 간격을 계약에서 가져온다`() {
        val f = sources.first { it.name == "MagiToolWindow.kt" }
        val src = code(f)
        val at = src.indexOf("private fun reattach()")
        assertTrue(at > 0, "`reattach` 를 못 찾았다 — 이 규칙이 빈 글을 본다")
        val body = src.substring(at, minOf(at + 1200, src.length))
        assertTrue("Backoff.delayMs(" in body,
            "재접속이 제 백오프를 쓴다 — 지터가 없으면 창 여럿이 같은 순간에 몰린다")
        assertTrue(
            Regex("""30_000|1_000""").find(body) == null,
            "재접속이 제 숫자를 다시 들고 있다 — 계약의 값과 갈린다",
        )
    }

    /**
     * **기동 예산은 정책이 정한다 — 창이 제 규칙을 따로 쓰지 않는다.**
     *
     * 예산·유예·「언제 용서하나」가 `clients/contract/lifecycle-policy.json` 으로 옮겨 갔는데,
     * 창이 옛 길을 그대로 쓰고 있으면 계약은 초록이고 화면은 옛 규칙대로 돈다 — 시험이 아무것도
     * 재지 않는 그 상태다. 그래서 **부르는 자리**를 붙든다.
     *
     * 재는 것 셋:
     *
     * 1. 붙었을 때 예산을 통째로 되돌리지 않는다. 옛 `Restarts.ok()` 가 그랬고, 그러면 떴다가
     *    2초 만에 죽기를 되풀이하는 데몬이 매 폴마다 용서받아 영원히 재시도된다.
     * 2. 띄우기 전에 정책에 묻는다.
     * 3. 유예를 제 상수로 따로 적지 않는다. 두 벌이던 5초가 이 규칙이 생긴 이유다.
     */
    /**
     * **준비 판정이 「누군가 듣는다」로 끝나지 않는다.**
     *
     * 옛 판정은 `기록의 pid == 내 자식 && reach is Listening` 이었다. 앞의 것은 **기록이** 우리
     * 자식을 가리킨다는 말이고 뒤의 것은 아무 프로세스나 거기 있다는 말이라, 둘 다 참이면서
     * 답하는 것이 남의 데몬일 수 있다. 규칙은 `Generation` 이 들고 시험도 거기 있지만, **규칙이
     * 옳은 것과 창이 그것을 부르는 것은 다른 사실**이라 부르는 자리를 따로 붙든다.
     *
     * 낱말이 아니라 **문장**을 못박는다 — 물어보고 답을 버리는 변이는 낱말을 다 남긴다.
     */
    /**
     * **§3 의 상태가 문서에만 있지 않다.**
     *
     * 전이표가 옳은 것과 창이 그것을 쓰는 것은 다른 사실이다 — 아무도 안 부르는 상태 기계는
     * 문서를 한 벌 더 쓴 것과 같다. 그래서 부르는 자리를 붙든다: 떠나면서 **세대를 들고 가고**,
     * 돌아와서 그것이 아직 제 것인지 묻고, 준비·실패·닫힘을 그 타입에 옮긴다.
     *
     * ⚠ 세대가 필요한 이유는 `project.isDisposed` 로 안 되기 때문이다. 이 창의 맵들은 창 사이에
     * **공유되고** 워크스페이스 경로로 키를 잡는다 — 닫히고 다시 열린 창은 「살아 있다」로 답하고,
     * 떠났던 비동기가 돌아와 새 창의 상태에 옛 결과를 쓴다.
     */
    @Test
    fun `창이 §3 의 상태를 실제로 옮긴다`() {
        val f = sources.first { it.name == "StartDaemon.kt" }
        val src = code(f)
        assertTrue(Regex("""val mine = phase\.generation""").containsMatchIn(src),
            "떠나면서 세대를 안 들고 간다 — 돌아와서 제 것인지 물을 근거가 없다")
        assertTrue(Regex("""if \(!phase\.still\(mine\)\) return""").containsMatchIn(src),
            "돌아와서 세대를 안 묻는다 — 닫히고 다시 열린 창에 옛 결과를 쓴다")
        assertTrue(Regex("""phase\.on\(Move\.Answered\)""").containsMatchIn(src),
            "준비를 상태로 안 옮긴다")
        assertTrue(Regex("""phase\.on\(Move\.LaunchFailed\)""").containsMatchIn(src),
            "기동 실패를 상태로 안 옮긴다")
        assertTrue(Regex("""Disposer\.register\(project\) \{ phase\.on\(Move\.Close\) \}""")
            .containsMatchIn(src),
            "창이 닫힐 때 상태를 안 닫는다 — 세대가 안 올라 떠났던 일이 제 것이라고 믿는다")
    }

    @Test
    fun `준비 판정이 답하는 프로세스에게 직접 묻는다`() {
        val f = sources.first { it.name == "StartDaemon.kt" }
        val src = code(f)
        assertTrue(
            Regex("""Generation\.same\(published, hello, child\.pid\(\)\)""").containsMatchIn(src),
            "준비 판정이 세대를 안 본다 — 「누군가 듣는다」로 준비를 선언하면 남의 데몬을 제 것으로 들인다",
        )
        assertTrue(
            Regex("""method = "about"""").containsMatchIn(src),
            "답하는 쪽에 직접 묻지 않는다 — 기록만 읽으면 파일과 프로세스가 갈렸을 때 못 본다",
        )
        assertTrue(
            Regex("""lineage\[base\] = it""").containsMatchIn(src),
            "확인한 계보를 안 붙든다 — 핸들이 죽은 후계를 남의 데몬으로 읽게 된다",
        )
        assertTrue(
            Regex("""Generation\.foreign\(""").containsMatchIn(src),
            "교체 판정이 여전히 「소켓이 돌아왔나」뿐이다 — 그 자리에 선 남의 데몬도 교체로 센다",
        )
    }

    @Test
    fun `창이 기동을 정책에 묻고, 제 규칙을 따로 쓰지 않는다`() {
        val f = sources.first { it.name == "StartDaemon.kt" }
        val src = code(f)
        assertTrue("Launches" in src, "창이 정책을 아예 안 쓴다 — 계약이 화면에 안 닿는다")
        assertTrue(".ok()" !in src,
            "붙자마자 예산을 되돌리는 옛 길이 남아 있다 — 순간 성공으로 초기화하지 않는다는 계약과 어긋난다")
        assertTrue(".connected(" in src,
            "붙어 있음을 정책에 안 알린다 — 안정 구간을 아무도 세지 않으니 예산이 영영 안 돌아온다")
        assertTrue(".may(" in src && ".spawned(" in src,
            "띄우기 전에 정책에 묻지 않거나, 띄운 것을 안 알린다 — 이동 구간이 셀 것이 없다")
        assertTrue(".lost(" in src, "손실을 정책에 안 알린다 — 유예도 연속 실패도 안 선다")
        // ⚠ **연속 실패에 아무도 1을 안 더하고 있었다**(설계 검토 R4). 준비 시한을 넘기거나
        // 뜨자마자 죽는 데몬은 **붙은 적이 없어** `lost` 로도 안 세인다 — 그러면 1분마다 예산이
        // 돌아와 같은 실패를 무한히 되풀이하고 `Blocked` 에 영영 못 닿는다.
        assertTrue(".failed(" in src,
            "기동 실패를 정책에 안 알린다 — 연속 실패가 안 쌓여 크래시 루프가 영원히 재시도된다")
        assertTrue(".ready(" in src,
            "준비됐다는 것을 정책에 안 알린다 — 안정 구간의 시계가 안 선다")

        // ⚠ **소유 모드는 파이프를 쥐는 것이 전부다**(설계 검토 R3). 이 창은 코어에 소유 모드가
        // 생긴 뒤에도 그냥 `--daemon` 으로 띄우고 자식 stdin 을 **곧바로 닫고** 있었다. 소유
        // 모드에서 그 닫힘은 「소유자가 떠났다」라, 데몬이 뜨자마자 스스로 끝낸다.
        assertTrue("owned-daemon-v1" in src,
            "소유 모드를 쓸 수 있는지 묻지 않는다 — 코어에 있는 수명 계약이 창에 안 닿는다")
        assertTrue("--client-owned" in src, "소유 모드로 띄우지 않는다")
        assertTrue(".hold(" in src,
            "소유자의 파이프를 아무도 안 쥔다 — 열어 두는 것이 이 모드의 전부다")
        assertTrue(
            Regex("""if \(owned\)[\s\S]{0,400}?outputStream\.close\(\)""").containsMatchIn(src),
            "구형 코어에서 stdin 을 안 닫는다 — 옛 수명이 그대로여야 한다",
        )
        // ⚠ 유예는 정책에서 온다. 창이 제 숫자를 들면 둘 중 하나만 고치는 날이 온다.
        assertTrue(
            Regex("""RESTART_GRACE\s*=\s*\d""").find(src) == null,
            "창이 유예를 제 숫자로 적는다 — 정책의 graceMs 와 갈린다",
        )
    }

    /**
     * **실려 온 생각은 그려져야 한다.**
     *
     * 셰이퍼가 `thought` 를 읽는 것은 `RowsTest` 가 붙든다. 그런데 이 창은 카운슬 행의 칸을
     * **이름으로 하나씩** 꺼내 그리므로(근거·유지 항목·사유), 행에 있다는 것과 화면에 선다는
     * 것은 다른 사실이다 — 이 트리가 되풀이해 겪은 「실려 오지만 안 그려짐」이 그것이다.
     *
     * `intellij` 에는 시험 소스셋이 없어서 화면을 세워 볼 수가 없다. 그래서 글자로 재되,
     * **무엇을 재는지는 좁힌다**: 카운슬 몸통을 짓는 그 자리에 `r.thought` 가 있는가.
     */
    @Test
    fun `카운슬 행이 멤버의 생각을 그린다`() {
        val f = sources.first { it.name == "MagiToolWindow.kt" }
        val src = code(f)
        // ⚠ 닻은 **유일해야 한다.** 처음엔 몸통을 여는 `add(Look.prose(r.text))` 로 잘랐는데 그
        // 줄은 이 파일에 두 번 있고(라운드가 열린 행과 평결 행), 첫 번째를 집어 카운슬이 아닌
        // 블록을 재고 있었다. `Who.Council` 분기 자체에서 시작한다.
        val body = src.substringAfter("Who.Council -> if (r.opened)", "")
        assertTrue(body.isNotEmpty(), "카운슬 분기를 못 찾았다 — 이 규칙이 빈 글을 보고 초록이 된다")
        val block = body.substringBefore("Who.Info ->")
        assertTrue("r.cite" in block, "닻으로 삼은 근거 줄이 이 블록에 없다 — 자른 자리가 틀렸다")
        assertTrue("r.thought" in block,
            "멤버의 생각이 카운슬 행에 안 그려진다. 답이 없던 표는 「답이 없었다」 한 줄로만 " +
                "남고, 왜 없었는지는 화면 밖이다")
        assertTrue("chat.verdict.thought" in block,
            "생각을 번들 없이 그린다 — 화면 글자는 번들에서 온다(영어 IDE 가 한국어를 본다)")
    }

    /**
     * **빈 전사는 무엇이든 말해야 한다.**
     *
     * 창을 처음 열면 판이 비고, 그동안 화면이 말하던 것은 제목표시줄의 수준과 12픽셀짜리
     * 글리프의 **툴팁**뿐이었다. 붙는 중인 창과 데몬이 없어 못 붙은 창이 똑같이 생겼다 —
     * 사용자가 그 자리를 직접 짚었다("뜨는중인지 무슨 문제가 있는지 불안해").
     *
     * 재는 것 셋:
     *
     * 1. 행이 없으면 인사를 세운다. 이걸 지워도 컴파일은 되고 화면도 «잘» 그려진다 — 빈 판이
     *    빈 판인 것은 오류가 아니라서, 이 규칙 말고는 아무도 안 본다.
     * 2. 인사의 상태 줄은 [MagiToolWindow] 의 `mood` 에서 온다. 새 문자열을 그 자리에서
     *    지어내면 글리프와 두 군데가 되고, 그러면 「초록 점에 연결 끊김 문구」가 돌아온다
     *    (그 파일의 R6 가 이미 한 번 겪었다).
     * 3. 상태가 바뀌면 빈 판을 다시 그린다. 안 그리면 「연결 중…」이 붙은 뒤에도 남는다 —
     *    안심시키려고 세운 줄이 거짓말이 되는, 이 변경이 만들 수 있는 최악의 결과다.
     */
    @Test
    fun `빈 전사에 인사가 서고, 그 인사는 연결 상태를 따라간다`() {
        val f = sources.first { it.name == "MagiToolWindow.kt" }
        val src = code(f)

        val draw = src.substringAfter("private fun redrawLog() {", "")
        assertTrue(draw.isNotEmpty(), "`redrawLog` 를 못 찾았다 — 아래 규칙들이 빈 글을 보고 초록이 된다")
        val greet = draw.substringBefore("private fun ")
        assertTrue("rows.isEmpty()" in greet && "Look.welcome(" in greet,
            "전사가 비었을 때 세우는 인사가 `redrawLog` 에 없다. 빈 판은 붙는 중인지 못 붙은 " +
                "것인지 말하지 않는다 — 그것이 사용자가 짚은 결함이다")
        assertTrue("mood.why" in greet && "mood.colour" in greet,
            "인사의 상태 줄을 `mood` 말고 다른 데서 만들고 있다. 글리프와 두 자리가 되면 " +
                "둘이 어긋난다(R6 — 초록 점에 연결 끊김 문구)")

        val paint = src.substringAfter("private fun paintLink() =", "")
        assertTrue(paint.isNotEmpty(), "`paintLink` 를 못 찾았다 — 아래 규칙이 빈 글을 본다")
        // ⚠ **다음 `private fun` 까지 자르면 안 된다.** `paintLink` 다음에 오는 것은 `private val`
        // 이라, 그렇게 자른 창은 250줄 아래 꼬리 타이머(`private val tail = Timer(130) { redrawLog() }`)
        // 까지 삼킨다 — 여기서 이 호출을 통째로 지운 변이가 그 남의 것을 읽고 **살아남았다**(실측).
        // 종류를 안 가리고 다음 선언에서 끊는다.
        val body = paint.split(Regex("""private (val|fun|var)|@Volatile""")).first()
        assertTrue("redrawLog()" in body,
            "상태가 바뀌어도 빈 판을 다시 안 그린다. 「연결 중…」이 붙은 뒤에도 남아, " +
                "안심시키려고 세운 줄이 거짓말이 된다")
    }

    /**
     * **카드의 되돌릴 수 없는 동사는 물어보고 한다.**
     *
     * 정보 카드는 모델 콤보 옆에 「다시 시작」과 「업데이트」를 세운다. 앞의 것은 되돌릴 수 있고
     * 뒤의 둘은 **도는 턴을 끝낸다** — 한 판에 섞여 있으면 눌러 보다가 일을 날린다. 그래서
     * 세 동사가 전부 `MessageDialogBuilder.yesNo(...).ask(project)` 를 지나는지 본다.
     *
     * 컴파일러도 다른 시험도 못 잡는다: 확인을 빼도 타입이 맞고 화면도 잘 그려진다. 다른
     * 것이 되는 것뿐이다.
     */
    @Test
    fun `카드의 되돌릴 수 없는 동사는 묻고 나서 문을 두드린다`() {
        val f = sources.first { it.name == "MagiToolWindow.kt" }
        val src = f.readText()
        val at = src.indexOf("private fun showInfo(")
        assertTrue(at > 0, "정보 카드를 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        // 카드 하나만 본다. 뒤 함수까지 흘러들면 남의 확인 대화를 우리 것으로 세게 된다.
        val end = src.indexOf("\n        /**", at)
        assertTrue(end > at, "카드의 끝을 못 찾았다 — 범위가 파일 끝까지 번졌다")
        val card = src.substring(at, end)

        // 카드의 **단추**를 소스에서 세어 목록을 지어내지 않는다. 단추 하나는 `act(라벨, 확인) { 문 }`
        // 한 줄이고, 가운데 인자가 그 단추의 확인 문구다 — 거기가 `null` 이면 말없이 도는 단추다.
        val buttons = Regex("""act\(([^\n]*?)\)\s*\{ it\.(\w+)\(\) \}""")
            .findAll(card).map { it.groupValues[1] to it.groupValues[2] }.toList()
        val doors = buttons.map { it.second }
        assertTrue(doors.size >= 2, "카드의 단추를 ${doors.size}개만 찾았다 — 훑기가 죽었다")
        assertTrue("restart" in doors,
            "세우는 문이 카드에 없다(찾은 것: $doors) — 이 규칙이 딴 자리를 보고 있다")
        for ((args, door) in buttons) assertTrue(
            Regex("""MagiBundle\.msg\("[^"]+"\)\s*,\s*MagiBundle\.msg\("[^"]+"\)""").containsMatchIn(args),
            "`$door` 단추가 확인 문구 없이 선다 — 누르는 순간 돈다(인자: $args)")
        assertTrue(card.contains(".yesNo(label, confirm).ask(project)"),
            "확인 문구를 들고만 있고 묻지는 않는다 — 사람은 대화가 뜰 줄 알고 누른다")

        // 업데이트는 예/아니오가 아니라 **갈래**라 위 접기를 안 쓴다(기다릴지 지금 할지). 규칙은
        // 같다 — 묻고 나서 두드린다 — 이므로 그 블록만 따로, **순서까지** 본다: 물음이 먼저 서고,
        // 고르지 않으면 돌아가고, 그 다음에야 문을 두드린다. 셋의 자리를 재는 이유는 낱말만
        // 세는 검사가 「물어보고 답을 버리는」 변이를 통과시키기 때문이다.
        val up = card.substringAfter("JButton(MagiBundle.msg(\"chat.info.update\"))", "")
            .substringBefore("c.gridx = 0")
        assertTrue(up.isNotEmpty(), "업데이트 단추를 못 찾았다 — 이 규칙이 빈 글을 본다")
        val asked = up.indexOf("Messages.showDialog(")
        val bailed = up.indexOf("return@addActionListener")
        val knocked = up.indexOf("comp.update(")
        assertTrue(asked >= 0, "업데이트 단추가 아무것도 안 묻는다 — 누르는 순간 돈다")
        assertTrue(bailed in (asked + 1) until knocked && knocked > 0,
            "묻기($asked)·돌아가기($bailed)·두드리기($knocked)의 순서가 어긋났다 — " +
                "답을 받고도 그냥 진행하면 물어본 것이 아니다")
        assertTrue("\"idle\"" in up,
            "갱신이 한가할 때를 고를 수 없다 — 두 선택지 중 기본이던 쪽이 사라졌다")
    }

    /**
     * **데몬이 말한 판 번호를 어느 화면인가는 그린다.**
     *
     * `about` 은 `version` 을 답하고 와이어에도 칸이 있는데, 2026-09-09까지 이 플러그인의 어느
     * 화면도 그것을 안 그렸다. 에러가 나지 않는 부류다 — 사람이 「어느 빌드에 붙어 있나」를
     * 물을 자리가 그냥 없었을 뿐이다. 다시 그렇게 되지 않게 못박는다.
     */
    @Test
    fun `데몬이 말한 판 번호가 화면까지 온다`() {
        val screens = sources.filter { "${File.separator}ui${File.separator}" in it.path }
        assertTrue(screens.size > 3, "화면 소스를 ${screens.size}장만 찾았다 — 훑기가 죽었다")
        assertTrue(
            screens.any { Regex("""about\(\)[\s\S]{0,40}\.version""").containsMatchIn(it.readText()) },
            "`about` 의 판 번호를 아무 화면도 안 읽는다 — 와이어에 있는 칸이 화면까지 안 온다")
    }

    /**
     * **모델 목록이 비면 데몬이 말한 사유가 화면에 온다.**
     *
     * `models` 는 백엔드가 5초 안에 답을 못 하면 **`ok = true` 에 빈 목록 + `why`** 로 온다
     * (`internal/adapter/daemon/doors.go` 의 `answerModels`: "no menu is a better answer than a
     * stuck one"). 그래서 `ok` 만 보는 화면은 **아무 실패도 못 본다** — 고를 것 없는 콤보가
     * 서고, 사람은 왜인지 알 길이 없다. 거절이 아니라 **성공에 담겨 오는 실패**다.
     *
     * 이 규칙이 계기가 된 것은 정보 카드였다(2026-09-09): 계획 판과 설정 화면은 처음부터
     * `why` 를 읽고 있었는데 새로 만든 카드만 안 읽었다. 세 자리가 갈리지 않게 못박는다 —
     * `models()` 를 부르는 자리는 전부 그 답의 `why` 를 읽는다.
     */
    /**
     * 주석을 걷고 **공백까지 접은** 소스. 「부르는 자리 옆」을 글자 수로 재는 규칙들이 쓴다.
     *
     * ⚠ 접는 것이 걷는 것만큼 중요하다. 주석만 지우면 그 줄의 **들여쓰기가 그대로 남아**, 여섯
     * 줄짜리 설명 하나가 220자를 먹는다 — 창이 코드가 아니라 공백으로 차고, 규칙은 바로 아래
     * 있는 코드를 못 본다. 실제로 그렇게 한 번 거짓으로 걸렸다.
     */
    private fun code(f: File): String = f.readText()
        .replace(Regex("""/\*.*?\*/""", RegexOption.DOT_MATCHES_ALL), "")
        .replace(Regex("""//[^\n]*"""), "")
        .replace(Regex("""\s+"""), " ")

    /**
     * **잡을 세우는 데는 끝이 둘이고 `ok` 는 둘 다 참이다.**
     *
     * `answerJobKill` 은 `{OK: true, Removed: KillBackgroundJob(name)}` 을 답하고, 바로 위
     * 주석이 그 이유를 적는다 — "pressed twice must read 'already gone', not 'failure'". 끝난
     * 잡을 세우라고 해도 **거절이 아니라** ok 로 답하고, 어느 쪽이었는지는 `removed` 가 말한다.
     *
     * ⚠ `Removed` 는 `omitempty` 가 붙은 Go bool 이라 **거짓은 전선에 안 나간다** — 「이미 없었다」의
     * 답은 글자 그대로 `{"ok":true}` 다(도는 데몬 실측, 2026-09-09).
     *
     * 그리고 그 구별이 가장 필요한 자리가 이 단추다: 행은 `jobs` 폴로 그려지므로 **잡이 끝난 뒤에도
     * 폴 한 번만큼 더 서 있다.** 낡은 행을 눌러도 행은 다음 폴에서 똑같이 사라지니, 아무 말이
     * 없으면 이 단추가 세운 줄로 읽힌다.
     */
    @Test
    fun `잡을 세운 것과 이미 없던 것을 가른다`() {
        val core = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val wire = File(core, "internal/adapter/daemon/protocol.go")
        assertTrue(wire.isFile, "코어의 와이어를 못 찾았다(${wire.absolutePath}) — 근거를 못 대고 있다")
        assertTrue(Regex("""Removed\s+bool\s+`json:"removed,omitempty"`""").containsMatchIn(wire.readText()),
            "와이어가 `removed` 를 이 규칙이 읽는 모양으로 안 싣는다")

        val src = code(sources.first { it.name == "PlanToolWindow.kt" })
        val at = src.indexOf("killJob(")
        assertTrue(at > 0, "잡을 세우는 자리를 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        // 그 자리만 본다. 넓히면 남의 갈래가 이 규칙을 통과시킨다.
        val where = src.substring(at, minOf(src.length, at + 300))
        assertTrue("removed" in where,
            "잡을 세우면서 답의 `removed` 를 안 읽는다 — 「세웠다」와 「이미 없었다」가 똑같이 조용하다")
        // 읽기만 하고 갈래가 없으면 사람에게 가는 것은 여전히 하나다.
        assertTrue(Regex("""!\w+\.removed\s*->""").containsMatchIn(where),
            "`removed` 를 읽지만 그것으로 갈라지는 갈래가 없다 — 읽은 사실이 화면까지 안 온다")
    }

    @Test
    fun `모델 목록을 묻는 화면은 못 받은 사유도 읽는다`() {
        // **부르는 자리만.** 문을 «선언하는» `Companion.kt` 는 `fun models()` 라 점이 없고,
        // 부르는 쪽은 언제나 `comp.models()` 다 — 그 차이로 가른다(첫 판은 선언을 부름으로 세어
        // 스스로 걸렸다).
        val callers = sources.filter { f ->
            "${File.separator}main${File.separator}" in f.path && ".models()" in f.readText()
        }
        assertTrue(callers.size >= 3,
            "`models()` 를 부르는 자리를 ${callers.size}곳만 찾았다 — 훑기가 죽었다")
        // ⚠ **파일 전체에서 `why` 를 찾으면 안 된다.** 첫 판이 그랬고, 변이가 살아남았다 —
        // `MagiToolWindow.kt` 에는 신호등의 `Mood.why` 가 따로 있어서, 카드가 사유를 통째로
        // 버려도 파일에는 언제나 `why` 가 있었다. 살아남은 것은 변이가 약해서가 아니라 이
        // 규칙이 **엉뚱한 자리를 보고 있어서**였다. 그래서 **부르는 자리 옆**만 본다.
        //
        // 창은 300자 — **접은 뒤**의 코드 300자다([code] 참조). 이 창을 넓히면 남의 `why` 가
        // 들어오고, 좁히면 그 화면이 거짓으로 걸린다.
        //
        // ⚠ **주석은 걷고 잰다.** 두 번째 판이 그러지 않아 계획 판의 변이가 살아남았다 — 그
        // 자리의 주석이 마침 "why 는 백엔드가 잠깐 죽었다는 말이라…"라고 설명하고 있어서,
        // 코드가 사유를 통째로 버려도 낱말은 늘 거기 있었다. 이 파일의 다른 규칙이 같은 함정을
        // 이미 이름으로 부르고 있다("설명하는 문장이 위반으로 잡힌다" — 거울상이다).
        val window = 300
        for (f in callers) {
            val src = code(f)
            for (m in Regex("""\.models\(\)""").findAll(src)) {
                val near = src.substring(m.range.last + 1, minOf(src.length, m.range.last + 1 + window))
                assertTrue("why" in near,
                    "${f.name} 가 `models()` 를 부르고 접은 코드 ${window}자 안에서 답의 `why` 를 안 읽는다 — " +
                        "백엔드가 죽으면 빈 목록이 이유 없이 선다(`ok` 는 참이라 거절 경로도 안 탄다)")
            }
        }
    }

    /**
     * **컴패니언 상태는 낱말 열거형이고, 화면이 그 낱말을 찍고 있었다.**
     *
     * `fleet.State`(`internal/adapter/fleet/fleet.go`)는 여섯인데 플릿 행은 셋만 옮기고 나머지를
     * `else` 로 흘렸다 — 사람은 「— abandoned」와 「— stopped」를 나란히 읽고 어느 쪽이 나쁜지
     * 스스로 알아야 했다. 코어가 `Abandoned` 위에 적어 둔 것이 정확히 그 해악이다: *"Every other
     * view renders this identically to a finished session, which is why it is here."*
     *
     * 여섯을 **코어에서 읽는다** — 일곱째가 코어에 서면 화면에 낱말로 새기 전에 여기서 운다.
     * 그리고 **둘이 같은 글자로 읽히면** 실패한다: 코어가 애써 가른 것을 화면이 도로 붙이는 것이
     * 이 규칙이 막으려는 바로 그 일이다.
     */
    @Test
    fun `플릿 행은 컴패니언 상태를 낱말이 아니라 구절로 말한다`() {
        val core = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val fleet = File(core, "internal/adapter/fleet/fleet.go")
        assertTrue(fleet.isFile, "코어의 플릿을 못 찾았다(${fleet.absolutePath}) — 근거를 못 대고 있다")
        val states = Regex("""(?m)^\t(\w+)\s+State\s*=\s*"([a-z]+)"""")
            .findAll(fleet.readText()).map { it.groupValues[2] }.toList()
        assertTrue(states.size >= 6,
            "코어에서 상태를 ${states.size}개만 읽었다 — 훑기가 죽으면 이 가드는 영원히 「전부 옮겼다」고 답한다")
        for (must in listOf("abandoned", "stopped"))
            assertTrue(must in states, "`$must` 을(를) 못 봤다 — 이 가드가 있는 이유가 그 둘이다")

        val src = code(sources.first { it.name == "PlanToolWindow.kt" })
        val at = src.indexOf("val state = when (r.state)")
        assertTrue(at > 0, "플릿 행의 상태 갈래를 못 찾았다")
        val where = src.substring(at, minOf(src.length, at + 700))
        val en = File(sources.first { it.name == "PlanToolWindow.kt" }
            .parentFile.parentFile.parentFile.parentFile.parentFile.parentFile.parentFile,
            "resources/messages/MagiBundle.properties").readText()

        val said = mutableMapOf<String, String>()
        for (st in states) {
            if (st == "idle") continue // 「아무 말 안 함」이 곧 「평상시」다 — 빈 칸이 맞다
            val key = Regex(""""$st" -> MagiBundle\.msg\("([a-z.]+)"\)""").find(where)?.groupValues?.get(1)
            assertTrue(key != null, "`$st` 가 갈래에 없다 — `else` 로 흘러 프로토콜 낱말이 그대로 찍힌다")
            val line = Regex("(?m)^${Regex.escape(key!!)}=(.*)$").find(en)?.groupValues?.get(1)
            assertTrue(line != null, "`$key` 가 번들에 없다 — 사람이 `!$key!` 를 본다")
            said[st] = line!!
        }
        assertNotEquals(said["abandoned"], said["stopped"],
            "일을 쥔 채 죽은 것과 끝나고 떠난 것이 같은 글자로 읽힌다 — 코어가 이름 댄 바로 그 해악이다")
    }

    /**
     * **창이 얼마나 찼나 옆에 무엇으로 찼나가 온다 — 그리고 화면이 그것을 실제로 그린다.**
     *
     * 코어의 `ContextParts` 는 다섯이고, 이 클라이언트는 그 칸을 **선언조차 안 하고 있었다** —
     * 전선에서 여기까지 올 길이 없었다. 코어가 그 결과를 이름 대어 적어 두었다: *"A screen that
     * shows only a total invites the wrong move… **the conversation is routinely the small half**."*
     *
     * ⚠ **다섯 다** 못박는다. 몇 개만 받으면 몫이 서로 안 더해지고 빠진 조각은 보이지도 않는다.
     *
     * ⚠ **그리는 자리까지** 본다. 처음 판은 총량 라벨에 `\n` 으로 붙였는데 `JBLabel` 은 개행을
     * 안 그린다 — 컴파일도 시험도 초록인 채로 그 줄이 화면에 없었다. 그래서 이 규칙은 「받나」가
     * 아니라 **「제 컴포넌트로 그리나」**를 묻는다.
     */
    @Test
    fun `창을 무엇이 채우는지가 전선에서 화면까지 온다`() {
        val core = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val st = File(core, "internal/app/context_state.go")
        assertTrue(st.isFile, "코어의 컨텍스트 상태를 못 찾았다(${st.absolutePath})")
        val struct = st.readText().substringAfter("type ContextParts struct").substringBefore("\n}")
        val names = Regex("""`json:"([a-z]+)""").findAll(struct).map { it.groupValues[1] }.toList()
        assertTrue(names.size >= 5, "코어에서 조각을 ${names.size}개만 읽었다 — 훑기가 죽었다")
        assertTrue("results" in names, "`results` 를 못 봤다 — 아무도 정하지 않았는데 자라는 그 조각이다")

        val wire = code(sources.first { it.name == "Wire.kt" })
        for (n in names) assertTrue(Regex("""val $n: Int""").containsMatchIn(wire),
            "와이어가 `$n` 을 안 받는다 — 몫이 서로 안 더해지고 빠진 조각은 보이지도 않는다")
        assertTrue("val parts: ContextParts?" in wire, "`ContextState` 가 조각을 안 든다")

        val panel = code(sources.first { it.name == "PlanToolWindow.kt" })
        assertTrue("fun makeup(" in panel, "조각을 글로 옮기는 자리가 없다")
        for (n in names) assertTrue("p.$n" in panel, "화면이 `$n` 을 안 그린다")
        // ⚠ **제 컴포넌트로 그린다.** 총량 라벨(JBLabel)에 붙이면 개행이 안 그려져 조용히 사라진다.
        // ⚠ **정확한 식이 아니라 성질을 묻는다.** 첫 판은 그 식을 글자 그대로 찾았는데,
        // 그 줄에 「접은 뒤 남은 주제」가 더해지자 앵커가 깨졌다 — 규칙의 뜻은 그대로인데.
        val assign = panel.indexOf("ctxParts.text =")
        assertTrue(assign > 0, "조각을 제 컴포넌트에 안 그린다 — 총량 라벨에 붙이면 화면에서 사라진다")
        assertTrue("makeup(" in panel.substring(assign, minOf(panel.length, assign + 300)),
            "제 컴포넌트에 조각이 아닌 것을 그린다 — 조각은 어디로 갔나")
        assertFalse(Regex("ctx\\.text[^\\n]{0,80}makeup\\(").containsMatchIn(panel),
            "조각을 총량 라벨에 붙였다 — `JBLabel` 은 개행을 안 그려 그 줄이 화면에서 사라진다")
        assertTrue(Regex("""val ctxParts = Look\.flow\(\)""").containsMatchIn(panel),
            "조각 줄이 접히는 칸이 아니다 — 한 줄 라벨이면 좁은 판에서 잘린다")
    }

    /**
     * **손에 하나를 쥔 컴패니언이 「비었다」로 읽히면 안 된다.**
     *
     * 코어는 `waiting` 과 `handling` 을 **함께 서명하고** 그 이유를 적어 뒀다 — *"they decide where
     * team-addressed work goes: fleet.Resolve routes a team address to the lightest companion, and
     * **load is Waiting + (1 if Handling)**."* 이 판은 큐만 그려서, 큐가 빈 채로 한 조각을 돌리고
     * 있는 컴패니언이 「비었다」로 보였다 — 사람이 다음 일을 건네는 행이 바로 그 행이다.
     *
     * ⚠ 합이 아니라 **둘 다**인지 본다. 수 하나로 접으면 「이미 하나가 돌고 있다」가 사라지고,
     * 손으로 고르는 사람이 알고 싶은 것이 그것이다.
     */
    @Test
    fun `플릿 행이 지고 있는 일을 말한다`() {
        val core = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val cluster = File(core, "internal/core/cluster/cluster.go")
        assertTrue(cluster.isFile, "코어의 클러스터를 못 찾았다(${cluster.absolutePath})")
        assertTrue("load is Waiting + (1 if Handling)" in cluster.readText(),
            "코어가 이 셈을 더는 적지 않는다 — 믿기 전에 다시 읽어라")

        val panel = code(sources.first { it.name == "PlanToolWindow.kt" })
        val at = panel.indexOf("val load = ")
        assertTrue(at > 0, "플릿 행의 부하 자리를 못 찾았다")
        val where = panel.substring(at, minOf(panel.length, at + 300))
        assertTrue("r.handling" in where,
            "큐만 그린다 — 손에 하나를 쥔 컴패니언이 「비었다」로 읽힌다")
        assertTrue("r.waiting" in where, "큐 깊이가 사라졌다")
        // 둘을 한 수로 접지 않았는지 — 접으면 「이미 하나가 돌고 있다」가 안 보인다.
        assertFalse(Regex("""r\.waiting\s*\+""").containsMatchIn(where),
            "둘을 한 수로 접었다 — 라우팅의 셈이지 사람이 읽을 말이 아니다")
    }

    /**
     * **접었으면 무엇이 아직 남아 있는지도 말한다.**
     *
     * 접기는 대화를 요약으로 바꾸는 일이라 수만 적으면 손실만 알린 셈이다. 코어는 자세한 내용을
     * 로그에 두고 `recall_context` 로 되불러 오며, **이름을 대는 것이 그 차이**라고 적어 뒀다 —
     * 토픽은 *"what «the detail is not lost» means concretely, and naming them is the difference
     * between that claim and a promise."*
     *
     * ⚠ 이 칸이 여기 없던 것은 **한쪽으로만 도는 가드** 때문이다: VS Code 의 `manual.test.ts` 는
     * 젯브레인 매뉴얼이 이름 대는 기능을 저쪽 이식 표가 판정하게 강제하지만, **그 반대 방향은
     * 아무도 안 잰다.** 그래서 VS Code 에 먼저 선 기능은 이쪽에서 조용히 빠진다.
     */
    @Test
    fun `접은 뒤 남은 주제가 전선에서 화면까지 온다`() {
        val core = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val st = File(core, "internal/app/context_state.go")
        assertTrue(st.isFile, "코어의 컨텍스트 상태를 못 찾았다(${st.absolutePath})")
        assertTrue(Regex("""Topics \[\]string\s+`json:"topics,omitempty"`""").containsMatchIn(st.readText()),
            "코어가 `topics` 를 이 규칙이 읽는 모양으로 안 싣는다")

        val wire = code(sources.first { it.name == "Wire.kt" })
        assertTrue("val topics: List<String>?" in wire, "와이어가 주제를 안 받는다 — 화면까지 올 길이 없다")
        val panel = code(sources.first { it.name == "PlanToolWindow.kt" })
        assertTrue("it.topics" in panel, "문의 답에서 주제를 안 꺼낸다")
        assertTrue("seen?.topics" in panel, "화면이 주제를 안 그린다 — 나르는 것과 그리는 것은 다르다")
        assertTrue("plan.usage.kept" in panel, "주제를 적을 글자가 없다")
    }

    /**
     * **실패한 서브에이전트가 성공한 것과 다르게 그려진다.**
     *
     * 등록부는 「도는 것 **또는 방금 끝난 것**」을 든다(`internal/app/subagent_jobs.go` 의 그 주석)
     * 그리고 `finish()` 는 행을 지우지 않고 `Err` 를 채운다. 그러니 `jobs.children` 에는 **실패
     * 사유를 담은 끝난 행**이 있다.
     *
     * 이 판은 그 목록을 `running` 으로만 걸러 쓰고 끝난 것은 `children` 문(=`SessionRow`, 에러 칸이
     * **없다**)으로 그려서, 실패한 자식과 성공한 자식이 똑같은 ⛒ 한 줄이었다. 붙들고 있던 것은
     * 「끝나면 등록부에서 사라진다」는 **틀린 주석**이었다.
     *
     * 근거가 움직이면 이 규칙도 다시 읽어야 하므로, 코어의 그 두 사실을 여기서 못박는다.
     */
    @Test
    fun `실패한 자식은 성공한 자식과 다르게 적힌다`() {
        val core = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val reg = File(core, "internal/app/subagent_jobs.go")
        assertTrue(reg.isFile, "코어의 서브에이전트 등록부를 못 찾았다(${reg.absolutePath})")
        val src = reg.readText()
        assertTrue("running or have just finished" in src,
            "등록부가 더는 「방금 끝난 것」을 든다고 말하지 않는다 — 이 규칙의 근거가 움직였다")
        assertTrue(Regex("""j\.Running, j\.Ended, j\.Steps, j\.Err = false""").containsMatchIn(src),
            "`finish()` 가 더는 `Err` 를 채우며 행을 남기지 않는다 — 다시 읽어라")

        val panel = code(sources.first { it.name == "PlanToolWindow.kt" })
        assertTrue("!it.running" in panel,
            "끝난 자식의 행을 등록부에서 안 꺼낸다 — 실패 사유가 거기에만 있다")
        assertTrue("plan.kid.failed" in panel, "실패를 적을 글자가 없다")
        // 사유를 꺼내 놓고 안 그리면 같은 결함이다.
        //
        // ⚠ **행 자체를 본다.** 첫 판은 `⛒` 앞뒤 창을 봤는데, 그 창에 사유를 «만드는» 줄
        // (`plan.kid.failed` 를 부르는 자리)이 들어 있어서 **행에서 빼도 통과했다** — 변이가
        // 그대로 살아남아 드러났다. 만드는 것과 붙이는 것은 다르다.
        val row = Regex("""kidRow\(project, "⛒ [^"]*"""").find(panel)?.value
        assertTrue(row != null, "끝난 자식의 행을 못 찾았다")
        assertTrue("\$failed" in row!!,
            "사유를 읽어 놓고 행에 안 붙인다 — 나르는 것과 그리는 것은 다르다: $row")
    }

    /**
     * **플릿 행이 그 컴패니언이 무엇을 하는 곳인지 말한다.**
     *
     * 라이브 실측(2026-09-10): word·excel·powerpoint 세 행이 「idle · sonnet」으로 **구별이 안 됐고**,
     * 같은 행이 `does` 로 각각이 무엇을 하는지 싣고 있었다. 코어가 이 칸이 전선을 타는 이유를
     * 적어 뒀다 — *"a name is enough to **pick a companion out of a roster**"*. 이 행이 그 로스터다.
     *
     * ⚠ `can` 은 `does.size` 가 아니다 — 목록이 `MaxDoes` 를 넘으면 **표본**이라 수를 따로 싣는다.
     * 셋만 보이고 마는 행은 그것이 전부인 것처럼 읽힌다.
     */
    @Test
    fun `플릿 행이 무엇을 하는 곳인지 말한다`() {
        val core = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val cluster = File(core, "internal/core/cluster/cluster.go")
        assertTrue(cluster.isFile, "코어의 클러스터를 못 찾았다(${cluster.absolutePath})")
        val src = cluster.readText()
        assertTrue("A SAMPLE when there are more than MaxDoes" in src,
            "코어가 더는 목록이 표본이라고 말하지 않는다 — `+N` 의 근거가 움직였다")

        val panel = code(sources.first { it.name == "PlanToolWindow.kt" })
        assertTrue("r.does" in panel, "행이 `does` 를 안 읽는다 — 컴패니언들이 서로 구별이 안 된다")
        assertTrue("r.can" in panel, "`can` 을 안 읽는다 — 표본을 전부인 양 그린다")
        assertTrue(Regex("""maxOf\(r\.can, does\.size\)""").containsMatchIn(panel),
            "표본 수를 `does.size` 로 센다 — 코어가 수를 따로 싣는 이유가 그것이 아니다")
        // 만드는 것과 붙이는 것은 다르다 — 라벨 문자열 자체를 본다.
        val row = Regex("""JBLabel\(name \+ [^)]*\)""").find(panel)?.value
        assertTrue(row != null, "플릿 행의 라벨을 못 찾았다")
        assertTrue("offers" in row!!, "무엇을 하는지 만들어 놓고 행에 안 붙인다: $row")
    }

    /**
     * **멤버의 렌즈가 화면까지 온다.**
     *
     * 실려 오는데 안 그리고 있었다 — 그리는 자리가 「라운드가 열린 행」 갈래에만 있었고, 그 칸이
     * 규칙과 한 자리를 쓰는 바람에 규칙만 그려졌다. 나르는 것과 그리는 것은 다르다.
     */
    @Test
    fun `카운슬 판정 행이 어느 렌즈인지 말한다`() {
        val panel = code(sources.first { it.name == "MagiToolWindow.kt" })
        assertTrue("r.rule" in panel, "라운드의 규칙을 제 칸에서 안 읽는다")
        assertTrue(Regex("""val lens = r\.lens""").containsMatchIn(panel),
            "멤버의 렌즈를 안 읽는다 — 한 라운드의 판정 셋이 서로 바꿔 놔도 같은 글이다")
        // 만드는 것과 붙이는 것은 다르다 — 머리글 문자열 자체를 본다.
        val d = "$"   // 원시 문자열 안의 `$`는 보간이라 글자로 쓰려면 한 번 돌린다
        val head = Regex("""rowHead\("⚖ \${d}name[^"]*"""").find(panel)?.value
        assertTrue(head != null, "판정 행의 머리글을 못 찾았다")
        assertTrue("${d}lens" in head!!, "렌즈를 만들어 놓고 머리글에 안 붙인다: ${d}head")
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

        // **자리가 또 옮겨졌고, 이번엔 없어졌다.** 수준은 라벨 → 제목표시줄로 갔다가, 이제
        // 어느 쪽에도 안 쓴다. 도구창 제목은 창이 무엇인지 말하는 자리이지 상태를 흘리는 자리가
        // 아니었고, 거기 쓰는 동안 사람이 본 것은 이랬다: 영어 IDE 의 탭에 「컴패니언이 붙어
        // 있다」가 뜨고, 그 탭을 드래그하니 **둘째 탭 제목까지** 같은 글자로 바뀌었다
        // (실측 2026-09-01). 같은 사실은 상태 표시줄이 늘 말하고 있었다 — 셋째 자리였다.
        //
        // 그래서 재는 것이 뒤집혔다: 「수준을 쓰는 문이 하나인가」가 아니라 **「수준이 글자가
        // 되는 자리가 없는가」**다. `Level` 은 이제 `text` 를 안 들고(그 글자는 `core` 에 살아
        // 영영 번역이 안 됐다), 남은 것은 사람이 할 일이 생기는 못-닿음 하나뿐이다.
        val door = "private fun say(l: Level)"
        assertTrue(door in code, "수준을 받는 문 `$door` 이 없다 — 규칙이 붙들 자리가 사라졌다")
        assertEquals(
            0, Regex("""\btitle\(""").findAll(code).count(),
            "수준이 다시 도구창 제목으로 간다. 그 자리는 창의 이름이지 상태가 아니고, 상태는 " +
                "상태 표시줄이 이미 말한다 — 셋째 자리를 만들면 탭 제목이 그 글자로 덮인다",
        )

        // `.state` 는 예외다 — 와이어 필드(RosterRow.state)의 이름이라 점 뒤에 정당하게 선다.
        // 이 그물이 잡는 것은 점 없는 식별자, 즉 옛 라벨 `state` 의 부활이다.
        assertTrue("say(title" !in code && Regex("""(?<![.\w])state\b""").findAll(code).count() == 0,
            "수준의 옛 자리(state 라벨)가 돌아왔거나 문이 글자를 받게 됐다. 사건은 `report()` 로 " +
                "전사에, 수준은 `say(Level)` 로 — 자리를 가른 사유가 둘의 KDoc 에 있다")
    }

    /**
     * **`core` 는 화면 글자를 안 든다.**
     *
     * `Level` 이 갈래마다 `text` 를 들고 있었다. 그 파일은 `core` 에 사는데 `core` 는 번들에
     * 못 닿으므로 그 글자는 **영영 한국어**였고, 영어 IDE 에 「컴패니언이 붙어 있다」가 떴다
     * (실측 2026-09-01). i18n 훑기가 `intellij` 만 봤기 때문에 못 잡은 자리다 — 훑는 곳이
     * 규칙이 사는 곳보다 좁으면 가드는 반쯤 죽는다.
     *
     * 재는 것은 **화면에 그대로 서는 글자를 내주는 멤버**다: `val text` / `fun text()`.
     * `core` 의 다른 한국어(사유 문장 등)는 부르는 쪽이 번들을 거쳐 쓰거나 로그로 가므로 여기서
     * 안 잡는다 — 넓히면 잡는 것보다 참는 것이 늘어난다.
     */
    @Test
    fun `수준 갈래는 화면 글자를 안 든다`() {
        val f = sources.first { it.name == "Level.kt" }
        val code = f.readText()
            .replace(Regex("""/\*.*?\*/""", RegexOption.DOT_MATCHES_ALL), "")
            .replace(Regex("""//[^\n]*"""), "")
        assertFalse(
            Regex("""\b(val|fun)\s+text\b""").containsMatchIn(code),
            "Level 이 화면 글자를 다시 들었다. 이 파일은 core 라 번들에 못 닿는다 — 그 글자는 " +
                "어느 IDE 에서든 한국어로 뜬다. 그리는 것은 번들에 닿는 쪽이 한다",
        )
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
        // `SocketPath.tooLong` 은 포트다 — 그 KDoc 이 "publish.go 의 `tooLong`" 이라고 적고 있다.
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

        // **파일이 아니라 패키지를 읽는다.** 한때 데몬의 큰 파일 하나를 이름으로 읽었는데, 그 파일이
        // 여섯으로 갈리자(2026-09-03) 이 시험이 `FileNotFoundException` 으로 죽었다 — 릴리스
        // 파이프라인에서. 상수는 패키지 안에서 옮겨 다니고, 이 시험이 지키는 것은 그 상수이지
        // 그것이 지금 어느 파일에 있는지가 아니다.
        val pkg = File(root, "internal/adapter/daemon")
        assertTrue(pkg.isDirectory, "코어의 daemon 패키지를 못 찾았다: $pkg")
        val go = pkg.listFiles { f: File -> f.name.endsWith(".go") && !f.name.endsWith("_test.go") }
            .orEmpty().joinToString("\n") { it.readText() }
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
        "MagiToolWindow.kt:grounds" to Safe(
            "바로 위에서 조각마다 Markup.text 로 지어 붙인 것이다 — 물음의 근거(키·본문 둘 다)",
            listOf("Markup.text(it.key)", "Markup.text(it.text)"),
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

    @Test
    fun `높이를 묻는 것은 시계 말고 로드가 끝난 사실에도 걸린다`() {
        // 답판의 높이는 브라우저만 안다. 스윙에서 물어보려면 JS 를 실어 보내야 하는데, 그 물음이
        // **문서가 실리기 전에** 나가면 CEF 는 프레임이 없다고 그냥 버린다 — 예외도 없고 콜백도
        // 없다. 물음이 시계에만 걸려 있으면(350ms·1400ms) 시작 경합에서 두 번 다 허공에 나가고,
        // 그때 감사 한 줄이 찍히고 **끝난다**: 다시 묻는 데가 없어서 그 판은 짐작 높이로 영영 선다.
        //
        // 실측(2026-09-03, 샌드박스 로그): 시작 직후 한 초에 세 판이 그 길로 갔다. 다음 런은
        // 0건이었다 — 안 날 때가 더 많은 경합이라, 눈으로 보는 것으로는 안 지켜진다.
        //
        // 고침은 타이머를 하나 더 두는 것이 아니다(같은 경주를 한 번 더 하는 것뿐이다). 로드가
        // 끝났다는 **사실**에 거는 것이고, 이 시험은 그 배선이 남아 있는지만 본다.
        val f = sources.firstOrNull { it.name == "RichAnswer.kt" }
        assertTrue(f != null, "RichAnswer.kt 를 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        val text = f!!.readText()
        assertTrue("onLoadEnd" in text,
            "RichAnswer 가 로드 끝에 높이를 다시 안 묻는다 — 시작 경합에서 놓친 판은 회복되지 않는다")
        assertTrue("addLoadHandler" in text,
            "로드 핸들러를 안 건다 — onLoadEnd 를 적어 놔도 아무도 부르지 않는다")
    }
    /**
     * ★ **코어가 물음에 실어 보낸 것은 그 물음을 그리는 자리에 닿는다.**
     *
     * `Waiting` 의 칸을 **와이어 선언에서 읽어** 판정한다. 손으로 적은 목록이 아니므로 코어가
     * 칸을 더하고 이 판이 그것을 안 그리면 그때 운다 — 이 카드에서 사라졌던 것이 이미 셋이다
     * (승인의 주어, 물음의 근거, 그리고 선 시각).
     *
     * 왜 소스를 글자로 읽나: 이것을 그리는 `MagiToolWindow` 는 `intellij` 모듈이고 그 모듈에는
     * **시험 소스셋이 없다.** 그리는 자리를 재는 길이 이것뿐이다.
     *
     * 안 그리기로 한 것은 여기에 사유와 함께 적는다. 목록이 아니라 **결정**이다.
     */
    @Test
    fun `물음에 실려 온 칸은 물음을 그리는 자리에 닿는다`() {
        val wire = sources.first { it.name == "Wire.kt" }.readText()
        val at = wire.indexOf("data class Waiting(")
        assertTrue(at > 0, "Waiting 선언을 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        val decl = wire.substring(at, wire.indexOf("\n) {", at))
        val fields = Regex("""^\s{4}val (\w+):""", RegexOption.MULTILINE)
            .findAll(decl).map { it.groupValues[1] }.toList()
        assertTrue(fields.size >= 8, "Waiting 칸을 ${fields.size} 개만 읽었다 — 훑기가 죽었다")

        // 그리지 않기로 한 것과 그 사유.
        val notDrawn = mapOf(
            "id" to "답을 되보낼 때 쓰는 손잡이지 사람에게 보일 사실이 아니다 — 단추가 들고 간다",
            "kind" to "무엇을 그릴지 정하는 값이다. `ask` 가 이 값에서 갈리므로 글자로 또 적으면 같은 말을 두 번 한다",
        )
        // **파생 속성을 따라간다.** `Waiting` 은 몇 칸을 스스로 접어 내놓는다 — `subject` 가
        // args·reason 을, `ask` 가 kind·options 를 읽는다. 창은 그 속성을 그리므로, 칸이 거기
        // 닿는 것도 「그린다」이다. 여기서 목록을 안 적고 **선언에서 읽어** 잇는다: 접는 자리가
        // 칸 하나를 떨어뜨리면 그때도 운다.
        val derived = Regex("""val (\w+): \w+ get\(\) \{""").findAll(wire.substring(at))
            .associate { m ->
                val body = wire.substring(at + m.range.first)
                m.groupValues[1] to body.substring(0, body.indexOf("\n    }"))
            }
        assertTrue(derived.isNotEmpty(), "Waiting 의 파생 속성을 하나도 못 읽었다 — 훑기가 죽었다")

        val win = sources.first { it.name == "MagiToolWindow.kt" }.readText()
        val from = win.indexOf("private fun drawPrompt(")
        assertTrue(from > 0, "물음을 그리는 자리를 못 찾았다")
        val end = win.indexOf("\n        /**", from)
        assertTrue(end > from, "그리는 자리의 끝을 못 찾았다 — 범위가 파일 끝까지 번졌다")
        val draw = win.substring(from, end)

        for (f in fields) {
            if (f in notDrawn) continue
            val direct = Regex("""\bw\.$f\b""").containsMatchIn(draw)
            val viaDerived = derived.any { (name, body) ->
                Regex("""\bw\.$name\b""").containsMatchIn(draw) && Regex("""\b$f\b""").containsMatchIn(body)
            }
            assertTrue(direct || viaDerived,
                "코어가 물음에 `$f` 를 실어 보내는데 그리는 자리가 안 읽는다 — 전선을 건너와 여기서 죽는다")
        }
    }

    /**
     * ★ **초는 화면까지 가지 않는다.**
     *
     * 변이가 잡았다. `RowText.ago` 를 시험이 잰다고 해서 **창이 그것을 부른다는 뜻은 아니다** —
     * 창에서 그 호출을 떼고 `r.ageSeconds` 를 그대로 문구에 넣어도 core 시험은 전부 초록이었다.
     * 부르는 자리가 `intellij` 모듈이고 그 모듈에는 시험 소스셋이 없다.
     *
     * 그래서 **나이가 글자가 되는 모든 자리**를 소스에서 본다: `ageSeconds` 를 읽는 줄은 반드시
     * `RowText.ago(` 를 지난다. 한 자리도 못 찾으면 그것부터 실패한다.
     */
    @Test
    fun `가십의 나이는 말로 바뀐 뒤에야 화면에 간다`() {
        val uses = sources
            .filter { "${File.separator}main${File.separator}" in it.path }
            .flatMap { f -> f.readText().lines().mapIndexed { n, l -> Triple(f.name, n + 1, l) } }
            .filter { (name, _, l) -> "ageSeconds" in l && name != "Wire.kt" }
        assertTrue(uses.isNotEmpty(), "나이를 쓰는 자리를 한 곳도 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        for ((name, line, text) in uses) assertTrue("RowText.ago(" in text,
            "$name:$line 이 나이를 말로 바꾸지 않고 쓴다 — 초가 화면에 그대로 간다: ${text.trim()}")
    }

    /**
     * ★ **접기의 결과를 그리는 자리는 접혔다는 사실도 그린다.**
     *
     * 「아직 남아 있음: …」은 접기에 대한 위로다. 그것만 그리면 화면에 위로는 있고 무엇에 대한
     * 위로인지는 없다 — 실제로 오래 그랬다.
     *
     * 그리는 자리가 `intellij` 모듈이라 시험 소스셋이 없다. 선언만 잰 시험(`WireConformanceTest`)
     * 은 칸이 **선언됐는지**만 알고 **그려지는지**는 모른다 — 오늘 이 세션에서 그 틈에 두 번
     * 걸렸다. 그래서 그리는 자리를 글자로 본다.
     */
    @Test
    fun `접기의 결과를 그리는 자리는 접힌 횟수도 그린다`() {
        val win = sources.first { it.name == "PlanToolWindow.kt" }.readText()
        val at = win.indexOf("ctxParts.text = listOf(")
        assertTrue(at > 0, "창 요약을 그리는 자리를 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        val block = win.substring(at, win.indexOf(").filter", at))
        assertTrue("topics" in block, "이 자리가 접기의 결과를 안 그린다 — 범위가 엉뚱한 곳을 잡았다")
        assertTrue("compactions" in block,
            "접기의 결과(topics)는 그리면서 접혔다는 사실(compactions)은 안 그린다 — " +
                "화면에 위로만 서고 무엇에 대한 위로인지가 없다")
    }

    /**
     * ★ **계기의 칸은 문이 준 값에서 온다 — 자리만 채운 상수가 아니라.**
     *
     * 변이가 잡았다. 앞 시험은 그리는 자리가 `compactions` 를 **이름 대는지**만 보는데, 문에서
     * 판으로 값을 옮기는 줄에서 `it.compactions` 를 `0` 으로 바꿔도 컴파일되고 아무도 안 울었다 —
     * 화면은 그 칸을 그리는 코드를 그대로 들고 영원히 「접힌 적 없음」을 그린다.
     *
     * 그래서 [Rows.Ctx] 의 칸을 **선언에서 읽어** 하나씩 본다: 만드는 자리가 같은 이름의 값을
     * 문에서 가져오는가. 칸이 늘면 따라오고, 하나도 못 읽으면 그것부터 운다.
     */
    @Test
    fun `계기의 칸은 문이 준 값에서 온다`() {
        val rows = sources.first { it.name == "Rows.kt" }.readText()
        val at = rows.indexOf("data class Ctx(")
        assertTrue(at > 0, "Ctx 선언을 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        // 괄호 짝으로 자른다 — 끝을 「\n    )」로 찾으면 들여쓰기 한 칸에 매달리고, KDoc 안의
        // 닫는 괄호에도 잘린다. 실측으로 밟았다: 같은 선언을 훑던 도구가 여섯 칸 중 **세 칸만**
        // 읽고 멀쩡히 답했다.
        val open = rows.indexOf('(', at)
        var depth = 0; var end = -1
        for (k in open until rows.length) {
            if (rows[k] == '(') depth++
            else if (rows[k] == ')') { depth--; if (depth == 0) { end = k; break } }
        }
        assertTrue(end > open, "Ctx 선언의 괄호 짝을 못 찾았다")
        // 주석을 걷는다. 남는 것만 코드다.
        val decl = rows.substring(open, end).lines()
            .filterNot { it.trimStart().startsWith("//") || it.trimStart().startsWith("*") ||
                it.trimStart().startsWith("/*") }
            .joinToString("\n")
        // 들여쓰기를 못박지 않는다 — 클래스가 한 겹 안팎으로 움직이면 훑기가 조용히 줄어든다.
        val fields = Regex("""^\s+val (\w+)\s*:""", RegexOption.MULTILINE)
            .findAll(decl).map { it.groupValues[1] }.toList()
        // 바닥은 기억한 숫자가 아니라 **두 번째 세기**다: 같은 몸통에서 `val` 을 따로 세어
        // 견준다. 하나라도 어긋나면 훑기가 칸을 흘린 것이고, 그때 초록은 「깨끗함」이 아니다.
        val counted = Regex("""\bval\s+\w+\s*:""").findAll(decl).count()
        assertEquals(counted, fields.size,
            "Ctx 칸을 ${fields.size}개 읽었는데 몸통에는 ${counted}개다 — 훑기가 칸을 흘린다")
        assertTrue(fields.size >= 4, "Ctx 칸을 ${fields.size}개만 읽었다 — 훑기가 죽었다")

        val win = sources.first { it.name == "PlanToolWindow.kt" }.readText()
        val from = win.indexOf("Rows.Ctx(")
        assertTrue(from > 0, "계기를 만드는 자리를 못 찾았다")
        val make = win.substring(from, win.indexOf(")", win.indexOf("it.window", from)) + 1)

        // `percent` 는 문에 그 이름의 칸이 없다 — used/window 로 여기서 셈한다.
        for (f in fields - setOf("percent", "tokens")) {
            assertTrue("it.$f" in make,
                "계기의 `$f` 가 문이 준 값에서 안 온다 — 자리는 채워지고 값은 영영 기본값이다: $make")
        }
        // tokens 는 이름이 다르다(문의 `used`). 이름이 다르다는 것 자체를 여기서 못박는다.
        assertTrue("it.used" in make, "계기의 tokens 가 문의 used 에서 안 온다")
    }

    /**
     * ★ **대화를 고르는 목록은 언제 마지막으로 움직였는지 말한다.**
     *
     * 실측(2026-09-10, 도는 데몬): 이 워크스페이스의 대화가 **241개**였고 그중 쉰한 개는 제목도
     * 없다. 목록은 제목과 id 여섯 자만 세웠다 — 이백 줄을 훑는 사람이 「아까 그 대화」를 찾는
     * 실마리가 그 시각뿐인데 그리지 않았다. 코어는 `lastActivity` 를 늘 보낸다.
     *
     * 고르는 자리가 `intellij` 모듈이라 시험 소스셋이 없다. 값이 **말로 바뀐 뒤** 가는 것까지
     * 본다 — 전선의 RFC3339 를 그대로 찍으면 UTC 라 이 기계의 시계와 어긋난다(짝인 VS Code 가
     * 정확히 그러고 있었고 같은 웨이브에서 함께 고쳤다).
     */
    @Test
    fun `대화를 고르는 목록은 마지막으로 움직인 때를 말한다`() {
        val win = sources.first { it.name == "MagiToolWindow.kt" }.readText()
        val at = win.indexOf("class Pick(val row: SessionRow)")
        assertTrue(at > 0, "대화를 고르는 줄을 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        // ⚠ **주석을 먼저 걷어낸다.** 안 걷으면 이 규칙이 코드가 아니라 **산문**에 걸린다 —
        // 변이가 그것을 보여 줬다: 그리는 식을 통째로 빈 글자로 바꿨는데도 바로 위 주석의
        // 「코어는 늘 보낸다(lastActivity)」가 그 낱말을 대신 물고 통과시켰다.
        val pick = win.substring(at, win.indexOf("\n                            }", at))
            .lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        assertTrue("row.title" in pick, "고르는 줄의 범위가 엉뚱한 곳을 잡았다")
        // 한 식으로 못박는다. 둘을 따로 물으면 「값은 있는데 딴 데 쓴다」와 「말로 바꾸긴
        // 하는데 딴 값을 바꾼다」가 둘 다 통과한다.
        assertTrue("RowText.asked(row.lastActivity)" in pick,
            "목록이 마지막으로 움직인 때를 **말로 바꿔** 그리지 않는다 — 안 그리면 이백 줄에서 " +
                "「아까 그 대화」를 찾을 실마리가 없고, 전선 값을 그대로 찍으면 RFC3339 는 UTC 라 " +
                "보는 사람의 시계와 어긋난다")
    }


    /**
     * ★ **못 뜬 사유의 코드는 코어가 정한다 — 그리고 모르는 코드는 배관이 아니라 낱말로 보인다.**
     *
     * 설정 화면은 코드로 번들 열쇠를 짓는다(`set.complete.why.<코드>`). 코어가 다섯째 코드를
     * 보내면 그 열쇠가 없고, 그때 화면에 서는 것은 사유가 아니라 `!set.complete.why.throttled!`
     * 같은 **제 구현**이다. 사유를 알리려고 만든 자리가 그 자리에서 배관을 보인다.
     *
     * 두 가지를 함께 못박는다. 아는 코드 목록이 **코어의 열거형과 같은가**(그래야 다섯째가
     * 생기는 날 여기가 운다), 그리고 그 코드마다 **두 언어 번들에 문장이 있는가**.
     */
    @Test
    fun `못 뜬 사유의 코드는 코어와 같고 두 언어에 문장이 있다`() {
        val core = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val go = File(core, "internal/app/complete.go")
        assertTrue(go.isFile, "코어의 complete.go 를 못 찾았다: $go")
        val fromCore = Regex("""Complete\w+\s+CompleteReason\s*=\s*"([a-z-]+)"""")
            .findAll(go.readText()).map { it.groupValues[1] }.filter { it.isNotEmpty() }.toSet()
        assertTrue(fromCore.size >= 4, "코어에서 사유 코드를 ${fromCore.size}개만 읽었다 — 훑기가 죽었다")
        assertEquals(fromCore, dev.sayaya.magi.ide.usecase.Assist.emptyReasons,
            "코어가 내는 사유 코드와 이 판이 아는 목록이 다르다 — 모르는 코드는 화면에 열쇠로 뜬다")

        val res = File(File(System.getProperty("user.dir")).parentFile,
            "intellij/src/main/resources/messages")
        for (name in listOf("MagiBundle.properties", "MagiBundle_ko.properties")) {
            val f = File(res, name)
            assertTrue(f.isFile, "번들을 못 찾았다: $f")
            val text = f.readText()
            for (code in fromCore) assertTrue("set.complete.why.$code=" in text,
                "$name 에 `$code` 의 문장이 없다 — 그 사유가 오면 화면에 열쇠가 뜬다")
        }

        // 모르는 코드는 열쇠를 안 만든다. 만들면 없는 열쇠라 배관이 뜬다.
        assertEquals(null, dev.sayaya.magi.ide.usecase.Assist.emptyKey("throttled"))
        assertEquals(null, dev.sayaya.magi.ide.usecase.Assist.emptyKey(null))
        assertEquals("set.complete.why.off", dev.sayaya.magi.ide.usecase.Assist.emptyKey("off"))

        // 그리는 자리가 그 갈래를 실제로 쓰는가 — 이 모듈에는 시험 소스셋이 없다.
        val ui = sources.first { it.name == "MagiConfigurable.kt" }.readText()
            .lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        assertTrue("Assist.emptyKey(code)" in ui,
            "설정 화면이 코드로 열쇠를 바로 짓는다 — 모르는 코드에서 배관이 뜬다")
    }


    /**
     * ★ **깨끗하지 않게 끝난 배경 명령은 판에 남는다.**
     *
     * 이 판은 배경 잡을 `it.running` 으로 걸러 도는 것만 그렸다. 컴패니언이 돌린 명령이 죽어도
     * ⚙ 한 줄이 조용히 사라질 뿐이고, 그 화면은 「잘 끝났다」와 똑같이 생긴다. 코어는 그러라고
     * `killed` 와 `exit` 를 싣는데 이 판은 둘 다 **선언만 하고 안 읽었다**.
     *
     * 그리는 자리가 `intellij` 모듈이라 시험 소스셋이 없다. 그래서 글자로 본다 — 걸러 내는 식이
     * 그 둘을 읽는지, 그리고 **「할 일 없음」 판정이 그 목록을 함께 보는지**(안 보면 실패한
     * 명령이 서 있는데 판이 「없음」이라 적는다).
     */
    @Test
    fun `깨끗하지 않게 끝난 배경 명령은 판에 남는다`() {
        val win = sources.first { it.name == "PlanToolWindow.kt" }.readText()
            .lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        val at = win.indexOf("val bgBad")
        assertTrue(at > 0, "끝난 배경 잡을 고르는 자리가 없다 — 죽은 명령이 판에서 사라진다")
        val pick = win.substring(at, at + 240)
        assertTrue("it.killed" in pick, "세운 잡을 안 가른다 — 코어가 `killed` 를 싣는 이유가 그것이다")
        assertTrue("it.exit" in pick, "종료 코드를 안 본다 — 실패가 성공과 같아 보인다")
        assertTrue("!it.running" in pick, "도는 잡까지 끝난 것으로 그린다")
        // 「할 일 없음」이 그 목록을 함께 본다.
        // ⚠ 조건의 끝은 여는 중괄호까지다. `substringBefore(")")` 로 자르면 **첫 괄호**인
        // `bgRunning.isEmpty()` 에서 잘려 나머지 조건을 못 본다 — 이 가드가 처음에 그렇게 죽었다.
        val none = win.substring(win.indexOf("bgRunning.isEmpty()")).substringBefore("{")
        assertTrue("kids.isEmpty()" in none, "「할 일 없음」 조건을 끝까지 못 읽었다 — 훑기가 잘렸다")
        assertTrue("bgBad.isEmpty()" in none,
            "실패한 명령이 서 있는데 판이 「할 일 없음」이라 적는다")
    }


    /**
     * ★ **읽는 자리도 커서 근처만 읽는다.**
     *
     * 보내는 자리는 `AssistTest` 가 잰다. 읽는 자리는 `intellij` 모듈이라 시험 소스셋이 없는데,
     * **진짜 비용이 거기 있다** — 통째 `getText` 는 4만 줄 파일에서 타건이 멈출 때마다 버퍼 전체를
     * ReadAction 안에서 복사한다. 보내는 쪽만 조이면 소켓은 가벼워지고 그 복사는 그대로 남는다.
     *
     * 그래서 문서 끝(`textLength`)이나 0 을 그대로 쓰는 범위가 없는지 본다 — 그것이 「통째로」의
     * 모양이다. 상한은 `Assist.SIDE_CAP` 에서 온다(두 자리가 다른 수를 들면 한쪽이 헛일이다).
     */
    @Test
    fun `완성 문맥을 읽는 자리도 커서 근처만 읽는다`() {
        val src = sources.first { it.name == "InlineCompletion.kt" }.readText()
            .lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        val at = src.indexOf("readAction {")
        assertTrue(at > 0, "완성 문맥을 읽는 자리를 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        val block = src.substring(at, src.indexOf("\n        }", at))
        assertTrue("TextRange" in block, "범위를 읽는 자리가 아니다 — 훑기가 엉뚱한 곳을 잡았다")
        assertTrue("Assist.SIDE_CAP" in src,
            "읽는 자리가 상한을 안 쓴다 — 보내는 자리만 조이면 통째 복사는 그대로 남는다")
        assertFalse(Regex("""TextRange\(\s*0\s*,""").containsMatchIn(block),
            "문서 처음부터 읽는다 — 커서에서 먼 쪽까지 통째로 복사한다")
        assertFalse(Regex("""TextRange\(\s*offset\s*,\s*doc\.textLength\s*\)""").containsMatchIn(block),
            "문서 끝까지 읽는다 — 커서에서 먼 쪽까지 통째로 복사한다")
    }


    /**
     * ★ **커밋 메시지 상자는 사람 것이다 — 초안이 쓰던 글을 덮지 않는다.**
     *
     * 이 액션은 기다리는 사이의 변경만 막고 있었다(`doc.text != before`). 그래서 **부를 때 이미
     * 글자가 있던 경우**는 그대로 덮었다 — 세 줄 써 두고 눌러 본 사람은 그 세 줄을 잃는다. 누른
     * 것이 「지금 쓴 것을 버려라」는 뜻은 아니다.
     *
     * 짝인 VS Code 가 같은 자리에 규칙을 적어 뒀다: *"Never overwrite. If they started typing, the
     * draft goes to a notification instead — the box is theirs."*
     *
     * 그리는 자리가 `intellij` 모듈이라 시험 소스셋이 없다 — 글자로 본다. **초안이 사라지지
     * 않는지**(안 앉으면 풍선으로 가는지)까지 본다: 안 덮는 것과 잃는 것은 다른 일이다.
     */
    @Test
    fun `커밋 초안은 쓰던 글을 덮지 않는다`() {
        val src = sources.first { it.name == "DraftCommitAction.kt" }.readText()
            .lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        val at = src.indexOf("val landed")
        assertTrue(at > 0, "초안을 앉히는 자리를 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        val block = src.substring(at, src.indexOf("}.getOrDefault", at))
        assertTrue("doc.text != before" in block, "기다리는 사이의 변경을 안 막는다")
        assertTrue("before.isNullOrBlank()" in block,
            "부를 때 이미 있던 글자를 안 본다 — 쓰던 커밋 메시지가 초안에 덮인다")
        // 안 앉았으면 초안이 사라지지 않는다.
        val after = src.substring(src.indexOf("}.getOrDefault", at))
        assertTrue("if (!landed)" in after, "안 앉은 초안을 아무 데도 안 보낸다 — 그러면 초안을 잃는다")
    }

    /**
     * **손의 가두기는 판정을 한 곳에서 빌려 온다.**
     *
     * `IdeHand.find` 는 에이전트가 대는 경로를 가상 파일로 바꾸는 자리다. 그 뒤에 `show` 와
     * `replace` 와 `problems` 가 있고, 가운데 것은 **문서를 고친다**. 여기서 가두기가 새면 옆
     * 프로젝트의 파일이 고쳐진다.
     *
     * 그 자리가 `path.startsWith(base)` 였다(2026-09-10 실측). 접두사 비교는 `/x/proj` 를 기준으로
     * `/x/proj-notes/…` 를 통과시키고, 그 함수의 KDoc 은 그때도 「작업 영역 외부 파일 접근 차단」이라
     * 적고 있었다 — 산문이 맞고 코드가 틀린 부류다.
     *
     * [InsideTest] 가 판정 자체를 재고, 여기서는 **그 판정을 실제로 부르는지**를 본다. 함수를 재는
     * 것과 부르는 자리를 재는 것은 다른 사실이다: 짝인 VS Code 에서 인용 함수를 시험해 두고 호출부의
     * 인용을 지웠더니 205개가 전부 초록이었다.
     */
    @Test
    fun `손은 작업 영역을 마디로 견준다`() {
        val src = sources.first { it.name == "IdeHand.kt" }.readText()
            .lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        val at = src.indexOf("private fun find(")
        assertTrue(at > 0, "경로를 가상 파일로 바꾸는 자리를 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        val block = src.substring(at, src.indexOf("\n    }", at))
        assertTrue("inside(base," in block,
            "손이 판정을 빌려 오지 않는다 — 가두기가 이 파일 안에서 다시 쓰이면 규칙이 둘로 갈린다")
        assertFalse("startsWith(base)" in block,
            "글자 접두사로 가둔다 — `/x/proj` 기준으로 `/x/proj-notes/…` 가 통과한다")
    }

    /**
     * **갈아타기는 적고 나서 움직인다.**
     *
     * 툴윈도는 `session.moved` 를 받으면 옛 스트림을 끊고 새 대화에 다시 붙는다. 그 분기가 셰이퍼에
     * 먹이기 **전에** 돌아가고 있었고, 그래서 못 따라가는 자리(고정 탭·나중에 다시 연 대화)에서는
     * 전사가 아무 말 없이 끝났다.
     *
     * [dev.sayaya.magi.ide.usecase.RowsTest] 가 행을 만드는 쪽을 재고, 여기서는 **그 행을 만들 기회를
     * 주는지**를 본다. 셰이퍼가 그릴 줄 아는데 아무도 안 먹이면 안 그리는 것과 같다.
     */
    @Test
    fun `갈아타기는 셰이퍼에 먹인 뒤에 움직인다`() {
        val src = sources.first { it.name == "MagiToolWindow.kt" }.readText()
            .lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        val at = src.indexOf("""if (e.type == "session.moved")""")
        assertTrue(at > 0, "갈아타기 분기를 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        val fed = src.indexOf("shaper.feed(e)", at)
        val left = src.indexOf("return", at)
        assertTrue(fed in (at + 1) until left,
            "셰이퍼에 먹이기 전에 돌아간다 — 고정 탭과 다시 연 대화에서 전사가 말없이 끝난다")
    }

    /**
     * **확신은 화면에도 선다.**
     *
     * [dev.sayaya.magi.ide.usecase.RowsTest] 가 행이 그 수를 나르는지와 옮겨 적는 글에 실리는지를 재고,
     * 여기서는 **평결 카드가 그것을 그리는지**를 본다. 행이 나르는데 아무도 안 그리면 안 나르는 것과 같다 —
     * 이 저장소가 되풀이해 값을 치른 그 모양이다.
     */
    @Test
    fun `평결 카드는 확신을 그린다`() {
        val src = sources.first { it.name == "MagiToolWindow.kt" }.readText()
            .lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        // ⚠ `val marks = buildList {` 는 **둘**이다(사용자 행과 평결 카드). 이름으로 집으면 첫째가
        // 걸리고, 이 규칙은 평결 카드가 아니라 남의 블록을 보고 실패한다 — 첫 판이 정확히 그랬다.
        // 평결 카드에만 있는 것으로 앵커를 잡는다.
        val at = src.indexOf("RowText.verdict(r.decision)?.let")
        assertTrue(at > 0, "평결 카드의 표식 목록을 못 찾았다 — 이 규칙이 아무것도 안 보고 있다")
        val block = src.substring(at, src.indexOf("Look.rowHead", at))
        assertTrue("chat.mark.noanswer" in block, "평결 카드가 아닌 블록을 보고 있다")
        assertTrue("r.confidence" in block, "확신이 카드에 안 선다 — 표는 보이고 규칙이 그것으로 무엇을 했는지는 가려진다")
    }

}
