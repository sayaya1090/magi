package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class SchedulesTest {
    @Test fun `묻는 잡은 명령과 시한을 안 보낸다`() {
        // 커맨드 잡을 고쳤다가 마음을 바꾼 자리 — 칸엔 글자가 남아 있다.
        val e = Schedules.edit(running = false, prompt = " 어제 것 정리해 ", command = "make", timeout = "20m")
        assertEquals(Schedules.Edit("어제 것 정리해", "", ""), e)
    }

    @Test fun `도는 잡은 묻는 말을 안 보낸다`() {
        val e = Schedules.edit(running = true, prompt = "어제 것 정리해", command = " make test ", timeout = " 20m ")
        assertEquals(Schedules.Edit("", "make test", "20m"), e)
    }

    @Test fun `시한은 비워도 된다`() {
        assertEquals("", Schedules.edit(true, "", "make", "   ").timeout)
    }
}
