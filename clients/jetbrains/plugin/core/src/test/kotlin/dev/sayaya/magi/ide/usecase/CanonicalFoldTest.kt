package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * **정본 접기와 이 사본이 같은 사건을 같은 종류의 행으로 만드는가.**
 *
 * 코어의 `internal/adapter/idebridge` 가 행을 짓는 규칙의 정본이고(여덟 과제 중 셋째), 그 픽스처와
 * 골든은 TypeScript 원본과 **행 단위로 대조해** 만들어졌다. 이 사본은 그 대조 밖에 있었다 — 어휘를
 * 여덟로 맞추기 전까지 낱말이 갈려 있었던 것도, 아무도 두 접기에 **같은 사건을 통과시켜 보지
 * 않아서** 그랬다.
 *
 * ⚠ **글자까지 같기를 요구하지 않는다. 그럴 수가 없다.** 두 가지가 일부러 다르다:
 *  1. 정본은 **한 줄 요약**을 낸다(`clip`: 첫 줄·공백 제거·100 UTF-16 단위). 「얕게 그린다」가 그
 *     문의 설계이고, 이 창은 답 전문을 마크다운으로 그린다 — 여기서 자르면 답이 사라진다.
 *  2. 이 창의 문구 일부가 한국어 리터럴이다(회복된 오류·답 없는 평결·갈아타기·접기). 정본은 그
 *     문장을 영어로 들고 있고, 어느 쪽 낱말을 쓸지는 **이 시험이 정할 일이 아니다**.
 *
 * 그래서 재는 것은 **종류의 수열**과 **구조 칸**이다. 글자가 아니라 사실 — 사건 하나가 몇 개의 행이
 * 되고 각각이 무엇인가. 거기서 갈리면 한쪽 화면이 다른 화면과 다른 이야기를 하는 것이고, 그것이
 * 이 트리가 되풀이해 값을 치른 자리다.
 */
class CanonicalFoldTest {

    private val root = File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
    private val json = Json { ignoreUnknownKeys = true; isLenient = true }

    private fun read(name: String): String {
        val f = File(root, "internal/adapter/idebridge/testdata/$name")
        assertTrue(f.exists(), "정본 픽스처를 못 찾았다(${f.absolutePath}) — 옮겨졌으면 이 시험부터 고칠 것")
        return f.readText()
    }

    @Test
    fun `정본과 이 사본이 같은 사건을 같은 종류의 행으로 짓는다`() {
        val events = json.decodeFromString<List<LogEvent>>(read("fold_events.json"))
        assertTrue(events.size >= 20, "픽스처에서 사건을 ${events.size} 개밖에 못 읽었다 — 읽기가 깨진 것이다")
        val golden = json.parseToJsonElement(read("fold_rows.json")).jsonArray

        val shaper = Rows()
        events.forEach { shaper.feed(it) }
        val mine = shaper.list()

        assertEquals(golden.size, mine.size,
            "행 갯수가 정본과 다르다(정본 ${golden.size}, 이쪽 ${mine.size}) — 한 사건이 한쪽에서만 " +
                "행이 되거나, 한쪽만 접고 있다:\n" + mine.joinToString("\n") { "${it.who} ${it.text.take(40)}" })

        golden.forEachIndexed { i, g ->
            val want = g.jsonObject["who"]!!.jsonPrimitive.content
            val got = mine[i].who.name.lowercase()
            assertEquals(want, got,
                "[$i] 같은 사건을 정본은 `$want`, 이쪽은 `$got` 로 적는다 — 낱말이 같아진 것과 같은 " +
                    "낱말을 같은 사건에 붙이는 것은 다른 사실이다. 이쪽 글자: ${mine[i].text.take(50)}")
        }
    }

    /**
     * 구조 칸은 글자와 달리 **번역되지 않는다.** 도구 이름·호출 열쇠·성공 여부, 카운슬의 멤버·라운드·
     * 판정·렌즈·규칙·근거·확신. 여기서 갈리면 한쪽 화면이 실제로 다른 사실을 그린다.
     */
    @Test
    fun `번역되지 않는 칸은 정본과 같다`() {
        val events = json.decodeFromString<List<LogEvent>>(read("fold_events.json"))
        val golden = json.parseToJsonElement(read("fold_rows.json")).jsonArray
        val shaper = Rows()
        events.forEach { shaper.feed(it) }
        val mine = shaper.list()

        var checked = 0
        golden.forEachIndexed { i, g ->
            val o = g.jsonObject
            val r = mine[i]
            fun same(key: String, ours: String?) {
                val theirs = o[key]?.jsonPrimitive?.content ?: return
                checked++
                assertEquals(theirs, ours, "[$i] `$key` 가 갈린다 — 정본 `$theirs`, 이쪽 `$ours`")
            }
            same("callId", r.callId.ifEmpty { null })
            same("member", r.member)
            same("decision", r.decision)
            same("lens", r.lens)
            same("rule", r.rule)
            same("cite", r.cite)
            same("keep", r.keep)
            o["round"]?.jsonPrimitive?.content?.let { checked++; assertEquals(it, r.round.toString(), "[$i] `round` 가 갈린다") }
            o["ok"]?.jsonPrimitive?.content?.let { checked++; assertEquals(it, r.ok?.toString(), "[$i] `ok` 가 갈린다") }
            o["silent"]?.jsonPrimitive?.content?.let { checked++; assertEquals(it, r.silent.toString(), "[$i] `silent` 가 갈린다") }
            o["opened"]?.jsonPrimitive?.content?.let { checked++; assertEquals(it, r.opened.toString(), "[$i] `opened` 가 갈린다") }
            o["confidence"]?.jsonPrimitive?.content?.let { checked++; assertEquals(it, r.confidence?.toString(), "[$i] `confidence` 가 갈린다") }
        }
        // ⚠ 바닥은 「무언가를 쟀다」의 유일한 증거다. 픽스처가 구조 칸 없는 행만 남으면 이 규칙은
        // 위반이 없어서가 아니라 **잴 것이 없어서** 초록이 된다.
        assertTrue(checked >= 15, "구조 칸을 $checked 개밖에 대조하지 않았다 — 픽스처가 얕아졌거나 읽기가 깨졌다")
        assertTrue(JsonArray(emptyList()).isEmpty() && JsonObject(emptyMap()).isEmpty())
    }
}
