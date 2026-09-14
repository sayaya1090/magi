package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.model.BridgeOp
import dev.sayaya.magi.ide.model.BridgeRow
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.StringReader
import java.io.StringWriter
import java.io.BufferedReader
import java.io.BufferedWriter

/**
 * **브리지의 문을 읽는 규칙** — 자식 프로세스 없이 전부 잰다.
 *
 * 스트림 이음매가 있는 이유가 이것이다: 이 저장소의 다른 탐침 시험들은 `#!/bin/sh` 픽스처를 쓰고,
 * 그것이 윈도우에서 안 돌아 값을 치렀다(#195). 프레이밍과 분배는 플랫폼과 무관한 사실이므로
 * 플랫폼과 무관하게 재는 것이 맞다.
 */
class BridgeRowsTest {

    /** 받은 것을 순서대로 적어 두는 싱크. 무엇이 언제 왔는지가 이 묶음이 재는 것이다. */
    private class Said : BridgeRows.Sink {
        val log = mutableListOf<String>()
        var rows: List<BridgeRow> = emptyList()
        val ops = mutableListOf<BridgeOp>()
        override fun reset(rows: List<BridgeRow>, events: Int) {
            this.rows = rows
            log += "reset(${rows.size}, $events)"
        }
        override fun changed(ops: List<BridgeOp>) {
            this.ops += ops
            log += "changed(${ops.joinToString(",") { "${it.op}:${it.id}" }})"
        }
        override fun ended(why: String) { log += "ended(${why.ifBlank { "-" }})" }
        override fun failed(why: String) { log += "failed($why)" }
    }

    private fun follow(vararg lines: String, diagnose: String = ""): Pair<Said, String> {
        val said = Said()
        val sent = StringWriter()
        var stopped = false
        val c = BridgeRows(
            incoming = BufferedReader(StringReader(lines.joinToString("\n") + if (lines.isEmpty()) "" else "\n")),
            outgoing = BufferedWriter(sent),
            stop = { stopped = true },
            diagnose = { diagnose },
        )
        c.use { it.follow("s_1", said) }
        assertTrue(stopped, "close() 가 끝내는 법을 안 불렀다")
        return said to sent.toString()
    }

    @Test
    fun `문에 묻는 것은 그 대화를 계속 달라는 것이다`() {
        val (_, sent) = follow("""{"id":1,"ok":true,"sub":1,"rows":[],"events":0}""")
        // ⚠ 한 줄로 끝나고 **flush 돼야** 한다. 버퍼에 남으면 자식은 우리 말을 기다리고 우리는 자식의
        // 답을 기다린다 — 기한이 없으면 그 교착은 영원하다.
        assertTrue(sent.endsWith("\n"), "물음이 줄로 끝나지 않는다: $sent")
        assertTrue(""""method":"rows"""" in sent, "rows 문을 안 두드린다: $sent")
        assertTrue(""""live":true""" in sent, "한 번짜리로 물었다 — 계속 받는 문이 아니다: $sent")
        assertTrue(""""session":"s_1"""" in sent, "어느 대화인지 안 말한다: $sent")
    }

    @Test
    fun `첫 프레임은 대화 전체이고 그 뒤는 변화다`() {
        val (said, _) = follow(
            """{"id":1,"ok":true,"sub":1,"events":4,"rows":[{"id":"1","who":"user","text":"hi","at":"2026-09-14T10:00:00Z"}]}""",
            """{"sub":1,"ops":[{"op":"add","id":"2","row":{"id":"2","who":"agent","text":"there"},"after":"1"}]}""",
            """{"sub":1,"ops":[{"op":"grow","id":"2","text":"!"}]}""",
            """{"sub":1,"done":true}""",
        )
        assertEquals(listOf("reset(1, 4)", "changed(add:2)", "changed(grow:2)", "ended(-)"), said.log)
        // 행의 칸이 실제로 들어온다 — 모델이 부분적이면 조용히 비는 그 자리다.
        assertEquals("2026-09-14T10:00:00Z", said.rows.single().at, "시간이 안 들어왔다")
        assertEquals("user", said.rows.single().who)
        assertEquals("1", said.ops.first().after, "자리를 가리키는 이름이 안 들어왔다")
    }

    @Test
    fun `거절은 구독이 아니라 답의 실패다`() {
        val (said, _) = follow("""{"id":1,"ok":false,"error":"no magi daemon at /x/d.sock"}""")
        assertEquals(1, said.log.size)
        assertTrue("no magi daemon" in said.log.single(), "사유가 그대로 안 온다: ${said.log}")
        assertTrue(said.log.single().startsWith("failed("), "거절을 끝으로 읽었다: ${said.log}")
    }

    /**
     * ⚠ **끝을 말하지 않고 끝나는 것이 가장 비싼 경우다.** 화면은 계속 살아 있다고 말하고, 사람은
     * 조용한 대화와 죽은 스트림을 못 가른다. 문 쪽 계약이 그것을 막고(사유를 싣는다), 이쪽은 파이프가
     * 그냥 끝나는 경우까지 같은 말을 해야 한다.
     */
    @Test
    fun `말없이 끝난 파이프도 사유를 만든다`() {
        val (started, _) = follow(
            """{"id":1,"ok":true,"sub":1,"rows":[],"events":0}""",
            diagnose = "브리지가 1 로 끝났습니다: bad workspace",
        )
        assertEquals(listOf("reset(0, 0)", "ended(브리지가 1 로 끝났습니다: bad workspace)"), started.log)

        // 시작도 못 한 채 끝났으면 그것은 **답의 실패**다 — 구독은 서지 않았다.
        val (never, _) = follow(diagnose = "브리지가 2 로 끝났습니다")
        assertEquals(listOf("failed(브리지가 2 로 끝났습니다)"), never.log)

        // 그리고 아무 진단도 없을 때조차 침묵하지 않는다.
        val (mute, _) = follow()
        assertEquals(1, mute.log.size)
        assertTrue(mute.log.single().startsWith("failed("), "말없이 끝난 것을 말없이 넘겼다: ${mute.log}")
    }

    /**
     * stderr 가 stdout 에 섞이면 이 자리에 온다. [BridgeRows.open] 이 그것을 막지만, 막는 것과
     * **왔을 때 조용히 넘기지 않는 것**은 다른 일이다 — 버리면 「답이 이상하다」로 보인다.
     */
    @Test
    fun `JSON 이 아닌 줄은 조용히 버리지 않는다`() {
        val (said, _) = follow("magi ide-bridge: something went wrong")
        assertTrue(said.log.single().startsWith("failed("), "JSON 아닌 줄을 넘겼다: ${said.log}")
        assertTrue("something went wrong" in said.log.single(), "무엇이 왔는지 안 말한다: ${said.log}")
    }
    @Test
    fun `구독 뒤 진단 없는 EOF 는 사용자 종료가 아니다`() {
        val (said, _) = follow("""{"id":1,"ok":true,"sub":1,"rows":[],"events":0}""")
        assertEquals(listOf("reset(0, 0)", "ended(브리지가 종료 통지 없이 연결을 닫았습니다)"), said.log)
    }

    @Test
    fun `사용자 종료는 읽기를 깨워도 사유가 없고 한 번만 닫는다`() {
        val said = Said()
        var stops = 0
        lateinit var c: BridgeRows
        val reader = object : BufferedReader(StringReader("")) {
            override fun readLine(): String? {
                c.close()
                return null
            }
        }
        c = BridgeRows(reader, BufferedWriter(StringWriter()), { stops++ }, { "not an error" })
        c.follow("s_1", said)
        c.close()
        assertEquals(listOf("ended(-)"), said.log)
        assertEquals(1, stops)
    }

}
