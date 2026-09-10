package dev.sayaya.magi.ide.usecase

/**
 * 행 하나를 그리기 전에 **글자로 정해지는 것들**.
 *
 * 창 클래스 안에 사는 사설 함수였다. 거기는 시험 소스셋이 없는 모듈이라(`intellij/`), 이
 * 규칙들은 오직 화면을 눈으로 봐야만 확인됐다 — 그런데 정작 하는 일은 문자열 계산이고
 * 플랫폼을 하나도 안 만진다. 재는 자리로 옮긴다. 창은 붓만 들면 된다.
 */
object RowText {

    /**
     * 이벤트 시각을 사람이 읽는 시계로. **못 읽으면 빈 글자다** — 지어내지 않는다.
     *
     * 나노초를 뗀다: 전사는 행이 수백 개고, 그 자리에서 소수점 아래 아홉 자리는 읽는 사람의
     * 눈만 먹는다. 표준시간대는 이 기계의 것이다(로그가 아니라 사람이 보는 줄이다).
     */
    /**
     * 판정 하나를 **다른 표면들이 이미 쓰는 말**로.
     *
     * `council.Decision` 은 셋인데 하나가 제 뜻의 반대로 읽힌다 — `continue` 는 "not done, more
     * work is needed" 이고 **턴을 끝내는 게이트**라 작업이 그것을 못 지나간다. 낱말을 그대로
     * 찍으면 그 표가 작업을 통과시킨 것처럼 읽힌다.
     *
     * 코어는 이 값을 이미 두 번 치렀다. 터미널은 첫 판정부터 "reject" 라고 적었고
     * (`internal/adapter/tui/render.go` 의 `councilVerdictLabel`), 웹 서버에는 이름이
     * `TestAContinueVoteReadsAsTheRejectionItIs` 인 시험이 있다 — *"The page printed the raw word
     * in a neutral colour, which reads as progress — the opposite of what the vote means."*
     *
     * [silent] 은 **넷째 결정이 아니라 넷째 결과**다: 아무도 안 준 평결이 집계에 안 세이도록
     * `abstain` 옆에 실려 온다. 그것을 「기권」으로 그리면 백엔드 고장을 「멤버가 재보고
     * 물러섰다」로 보고하는 셈이다.
     *
     * [word] 는 옮겨 적는 글의 말(이 파일의 이웃들처럼 영어 리터럴)이고, [key] 는 화면이 쓸
     * 번들 열쇠다 — **표는 하나**이므로 둘이 갈릴 수 없다. 모르는 결정은 터미널이 쓰는 중립
     * 표식과 함께 날것으로 지나간다.
     */
    data class Verdict(val icon: String, val word: String, val key: String)

    fun verdict(decision: String?, silent: Boolean = false): Verdict? {
        if (silent) return Verdict("⋯", "no answer", "chat.verdict.noanswer")
        return when (decision?.trim()) {
            null, "" -> null
            "done" -> Verdict("✓", "done", "chat.verdict.done")
            "continue" -> Verdict("✗", "reject", "chat.verdict.reject")
            "abstain" -> Verdict("∅", "abstain", "chat.verdict.abstain")
            else -> Verdict("·", decision.trim(), "")
        }
    }

    /**
     * **서 있는 물음이 언제 선 것인가.** 없거나 못 읽으면 빈 글자.
     *
     * 코어가 이 사실을 [dev.sayaya.magi.ide.model.Waiting.since] 로 보낸다 — 그 구조체에서
     * `omitempty` 가 붙지 않은 **유일한 칸**이라, 물음이 서 있으면 언제나 실려 온다. 안 읽고
     * 있었다: 아무도 안 볼 때 마흔 분 전에 선 물음과 방금 내가 만든 물음이 똑같이 그려졌고,
     * 둘은 다른 상황이다 — 앞의 것은 자리를 비운 사이 턴이 통째로 멈춰 서 있었다는 뜻이다.
     *
     * **경과("40분 전")가 아니라 시계로 적는다.** 물음이 서 있는 동안 이 판은 다시 안 그려지므로
     * 경과는 처음 그려진 값에서 얼어붙어 조용히 거짓말을 한다. 시계는 언제 읽어도 맞는다.
     *
     * 오늘이 아니면 날짜를 붙인다 — 어제 것의 「14:32」는 한 시간 전으로 읽힌다. [now] 는 시험이
     * 오늘을 정할 수 있게 인자로 받는다(기본은 진짜 지금).
     */
    fun asked(at: String?, now: java.time.Instant = java.time.Instant.now()): String = at?.let {
        runCatching {
            val zone = java.time.ZoneId.systemDefault()
            val t = java.time.Instant.parse(it).atZone(zone)
            val hm = "%02d:%02d".format(t.hour, t.minute)
            if (t.toLocalDate() == now.atZone(zone).toLocalDate()) hm
            else "${t.toLocalDate()} $hm"
        }.getOrNull()
    }.orEmpty()

    /**
     * **가십으로 본 것이 얼마나 오래된 사실인가**, 사람이 한눈에 읽는 말로.
     *
     * 전선이 싣는 것은 초이고, 코어가 그 칸에 규칙을 적어 뒀다 — *"A screen that shows a state
     * without its age is claiming to know something it cannot"*. 가십은 한 시간에 걸쳐 삭으므로
     * 이 수의 정상 범위가 0~3600 이고, 그래서 초를 날것으로 찍으면 대부분의 시간을
     * 「3540초 전 확인」 같은 꼴로 보낸다. 실제로 그렇게 찍고 있었다.
     *
     * **없는 신선함을 지어내지 않는다**: 음수나 말 안 한 값은 빈 글자다. 「방금」은 삭은 줄을
     * 잰 줄처럼 보이게 만드는 딱 하나의 주장이다.
     */
    fun ago(seconds: Long?): String {
        val s = seconds ?: return ""
        if (s < 0) return ""
        // 「방금」이 아니라 「1분 미만」이다. 문구가 "seen {0} ago" 라 「방금」을 넣으면
        // "seen just now ago" 가 되고, 무엇보다 이 판과 VS Code 가 같은 사실을 다른 말로 적게 된다.
        if (s < 60) return "<1m"
        val m = Math.round(s / 60.0)
        if (m < 60) return "${m}m"
        val h = m / 60
        val rest = m % 60
        return if (rest == 0L) "${h}h" else "${h}h ${rest}m"
    }

    fun clock(at: String?): String = at?.let {
        runCatching {
            java.time.Instant.parse(it).atZone(java.time.ZoneId.systemDefault())
                .toLocalTime().withNano(0).toString()
        }.getOrNull()
    }.orEmpty()

    /**
     * **행 하나를 옮겨 적을 글자로.** 클립보드로 나가는 것은 화면이 아니라 이것이다.
     *
     * 화면은 색·아이콘·접힘으로 사실을 말한다. 글자로 나갈 때 그 사실들이 통째로 사라지면
     * 붙여넣은 쪽은 **무슨 일이 있었는지 모르는 전사**를 받는다 — 실패한 툴 호출이 성공한
     * 것과 똑같이 생기고, 접혀 있던 생각은 아예 없던 일이 된다. 그래서 색이 말하던 것을
     * 글자가 말하게 한다: 누가 말했는지, 툴이 됐는지 안 됐는지, 이것이 생각인지.
     *
     * **접힘은 안 본다.** 화면에서 접혀 있어도 옮겨 적을 때는 편다 — 접힘은 보는 사람의
     * 편의지 사실이 아니고, 붙여넣기는 대개 「남에게 보여 주려고」 하는 일이다.
     *
     * 아직 흐르는 중인 행([Row.draft])은 커서 글리프를 안 붙인다. 화면에서는 반쪽 답이
     * 그렇게 보여야 하지만, 글자로 나간 뒤엔 그 `▌` 가 답의 일부처럼 읽힌다.
     */
    fun plain(r: Row): String {
        val head = when (r.who) {
            Who.User -> "You"
            Who.Agent -> "magi"
            Who.Thinking -> "thinking"
            Who.Tool -> buildString {
                append("tool ").append(r.tool.orEmpty())
                append(
                    when {
                        r.ok == null -> " (running)"
                        r.note -> " (ok, read this)"
                        r.ok == true -> " (ok)"
                        else -> " (failed)"
                    },
                )
            }
            Who.Council -> buildString {
                append("council")
                r.member?.takeIf { it.isNotBlank() }?.let { append(" ").append(it) }
                if (r.round > 0) append(" r").append(r.round)
                verdict(r.decision, r.silent)?.let { append(" — ").append(it.word) }
                // 얼마나 확신했나. 집계가 이 수로 가중하므로, 낱말만 옮겨 적으면 규칙이
                // 그것으로 무엇을 했는지가 붙여 넣은 글에서 사라진다.
                r.confidence?.let { append(" ").append(Math.round(it * 100)).append("%") }
            }
            Who.Info -> "info"
        }
        val body = buildList {
            if (r.text.isNotBlank()) add(r.text)
            // 물은 것과 답한 것은 다른 사실이라 둘 다 적는다 — 화면의 규칙과 같다.
            r.args?.takeIf { it.isNotBlank() }?.let { add(it) }
            r.out?.takeIf { it.isNotBlank() }?.let { add(it) }
            r.why?.takeIf { it.isNotBlank() }?.let { add(it) }
            // 옮겨 적는 글에도 같이 간다 — 화면에만 있고 붙여 넣은 글에 없으면, 그 표가
            // 무엇 위에 서 있었는지가 대화 밖으로 못 나간다.
            r.cite?.takeIf { it.isNotBlank() }?.let { add("on: $it") }
            r.keep?.takeIf { it.isNotBlank() }?.let { add("keep: $it") }
            // 생각도 간다. 화면이 접어 두는 것이지 없는 사실이 아니고, [plain] 은 접힘을 안 본다
            // — 이 파일 맨 위의 규칙 그대로다. 「표가 아니다」를 붙여 넣은 글에서도 말해야 한다.
            r.thought?.takeIf { it.isNotBlank() }?.let { add("thought (not a vote): $it") }
        }
        return (listOf(head) + body).joinToString("\n")
    }

    /**
     * 여러 행을 하나로. 빈 줄로 가른다 — 행 사이가 안 갈리면 붙여넣은 쪽에서 어디까지가 한
     * 사람의 말인지 못 읽는다.
     */
    fun plain(rows: List<Row>): String = rows.joinToString("\n\n") { plain(it) }

    /** 한 줄로 줄인다. 인자가 길면 전사가 그 인자만으로 화면을 다 먹는다. */
    fun oneLine(s: String, max: Int): String {
        val one = s.lineSequence().joinToString(" ")
        return if (one.length <= max) one else one.take(max) + "…"
    }

    /**
     * 접힘을 기억하는 열쇠. **글자까지 넣는다** — 같은 자리의 행이 내용만 바뀌면 그건 다른
     * 것이고, 펼침을 물려받으면 사람이 안 편 것이 펴진 채로 선다.
     */
    fun foldKey(r: Row): String = "${r.msgId}:${r.who}:${r.callId}:${r.text.hashCode()}"

    /**
     * 리치 렌더 패널을 붙들어 두는 열쇠. `msgId` 가 없으면 시각으로 — 둘 다 없으면 빈 글자라
     * 그 행은 캐시를 안 탄다(같은 열쇠에 다른 답이 묶이는 것보다 낫다).
     */
    fun richKey(r: Row): String = r.msgId.takeIf { it.isNotBlank() } ?: r.at.orEmpty()

    /**
     * 이 행이 편집이면 그 양쪽. **성공한 것만** — 실패한 편집은 디스크를 안 바꿨으니 보일
     * 변화가 없고, 그걸 그리면 안 일어난 일을 그리는 것이다.
     */
    fun diffSides(r: Row): Triple<String, String, String>? =
        if (r.ok != true) null else Rows.EditSides.of(r.tool, r.args)
}
