package dev.sayaya.magi.ide.usecase

/**
 * 남이 준 글자를 HTML 라벨에 **그대로** 보이게 한다.
 *
 * 스윙 라벨은 `<html>` 로 시작하면 안을 마크업으로 읽는다. 그래서 여기 실려 오는 것 —
 * 정해지는 명령, 정책이 댄 사유, 도구 이름 — 은 전부 **남이 지은 글자**인데 그대로 붙이면
 * 화면에 나오는 것이 승인되는 것과 다른 글자가 된다. `rm x && echo <done>` 의 `<done>` 은
 * 태그로 먹혀 사라지고, 사람은 없어진 조각을 못 보고 「허용」을 누른다.
 *
 * **숨기는 것과 뭉개는 것은 같은 결함이다.** 무엇을 허가하는지 안 보이는 창이나, 보이긴 하는데
 * 실제와 다른 창이나, 사람이 모르고 누르는 것은 같다.
 *
 * `intellij` 가 아니라 여기 있는 이유: 저 모듈에는 테스트 소스셋이 없어서 이 규칙을 **잴 수가
 * 없다**. 플랫폼을 안 만지는 것은 아래에 둔다.
 */
object Markup {

    /**
     * 엔티티를 막고 줄바꿈을 `<br/>` 로 바꾼다.
     *
     * `&` 를 **먼저** 바꾼다. 나중에 바꾸면 앞서 넣은 `&lt;` 의 `&` 를 다시 잡아 `&amp;lt;` 가
     * 되고, 화면에는 `&lt;` 라는 글자가 뜬다.
     */
    fun text(s: String): String = s
        .replace("&", "&amp;")
        .replace("<", "&lt;")
        .replace(">", "&gt;")
        .replace("\n", "<br/>")

    /**
     * 모델 답의 마크다운 **부분집합**을 HTML 로 편다 — 원문이 마크다운으로 오는데 원문 글자를
     * 그대로 보이면 그것대로 못 읽는다(사용자 실측: "md 포맷으로 오는데 렌더링이 제대로 안
     * 되는 것 같네"). 여기는 전사 안의 **가독 렌더**만 맡고, 제대로 된 것(머메이드까지)은
     * 행의 「md ↗」 가 IDE 의 마크다운 에디터/미리보기로 연다 — IDE 가 더 잘 그리는 것은
     * IDE 에(§0-5 역-불변식).
     *
     * 이스케이프가 **먼저**다([text] 와 같은 사유) — 마크다운을 편 뒤에 이스케이프하면 우리가
     * 만든 태그가 같이 죽고, 안 하면 남의 글자가 태그로 먹힌다. 지원: ``` 펜스(고정폭 블록),
     * `인라인 코드`, **굵게**, *기울임*, # 머리글, -·* 목록. 그 밖(링크·표·이미지)은 원문
     * 글자 그대로 둔다 — 어설픈 반쪽 렌더보다 정직한 원문이 낫다.
     */
    fun markdown(s: String): String {
        val esc = s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
        val out = StringBuilder()
        var inFence = false
        val fenceBuf = StringBuilder()
        fun inline(t: String): String {
            // 코드 조각을 **먼저 빼 둔다.** 안 빼면 강조 정규식이 코드 안에서 다시 먹어
            // `a**b**c` 의 별 두 쌍이 화면에서 사라진다(리뷰 실측) — 옮겨 적을 것이 옮겨
            // 적히지 않는 것은 이 파일이 존재하는 사유 그 자체다.
            val code = mutableListOf<String>()
            var r = Regex("`([^`]+)`").replace(t) { m ->
                code += m.groupValues[1]
                "\u0000" + (code.size - 1) + "\u0000"
            }
            r = Regex("\\*\\*([^*]+)\\*\\*").replace(r) { m -> "<b>" + m.groupValues[1] + "</b>" }
            r = Regex("(?<![*\\w])\\*([^*\\s][^*]*)\\*").replace(r) { m -> "<i>" + m.groupValues[1] + "</i>" }
            return Regex("\u0000(\\d+)\u0000").replace(r) { m ->
                "<code>" + code[m.groupValues[1].toInt()] + "</code>"
            }
        }
        for (line in esc.lines()) {
            if (line.trimStart().startsWith("```")) {
                if (inFence) {
                    out.append("<pre>").append(fenceBuf).append("</pre>")
                    fenceBuf.setLength(0)
                } // 여는 펜스 줄의 언어 표기는 버린다 — 글자가 아니라 지시다.
                inFence = !inFence
                continue
            }
            if (inFence) { fenceBuf.append(line).append("\n"); continue }
            val h = Regex("^(#{1,6})\\s+(.*)").find(line)
            when {
                h != null -> out.append("<b>").append(inline(h.groupValues[2])).append("</b><br/>")
                line.startsWith("- ") || line.startsWith("* ") ->
                    out.append("&nbsp;&bull; ").append(inline(line.substring(2))).append("<br/>")
                else -> out.append(inline(line)).append("<br/>")
            }
        }
        // 닫는 펜스가 안 온 원문도 실린 데까지 고정폭으로 — 잘린 답도 답이다.
        if (inFence && fenceBuf.isNotEmpty()) out.append("<pre>").append(fenceBuf).append("</pre>")
        // 마지막 줄바꿈은 뗀다 — 답마다 빈 줄 하나가 남던 자리(여백 불평과 같은 축).
        return out.toString().removeSuffix("<br/>")
    }
}
