package dev.sayaya.magi.ide.usecase

import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.*
import org.junit.jupiter.api.Assertions.*
import org.junit.jupiter.api.Test
import java.io.File

class DraftArchiveTest {
    @Test fun `answer model exports inactive sessions and restores without entering answer mode`() {
        val state = AnswerDrafts()
        state.bind("s1", "q1", "general one", "question")
        state.enter("general one")
        state.edit("submitted")
        state.begin("submitted")
        state.leaveAfterSubmit()
        state.bind("s2", null, "general one")
        state.edit("general two")
        val archive = DraftArchive(1, "file:///work", "owner-a", 1, state.exportEntries("owner-a", "/work"), emptyList())
        assertTrue(archive.entries.any { it.kind == "attempt" && it.text == "submitted" })
        assertTrue(archive.entries.any { it.sessionId == "s2" && it.text == "general two" })
        val next = AnswerDrafts()
        next.bind("s2", null, "new input")
        next.importArchive(archive, "file:///work")
        next.importArchive(archive, "file:///work")
        assertEquals("new input", next.generalText())
        assertNull(next.active)
        assertFalse(next.busy(AnswerDrafts.Key("s1", "q1")))
        assertEquals(archive.entries.size, next.recoveries.size)
        val unknown = next.recoveries.first { it.archived?.status == "unknown" }
        next.deleteRecovery(unknown.id)
        next.importArchive(archive, "file:///work")
        assertFalse(next.recoveries.any { it.id == unknown.id })
        val persisted = DraftArchive(1, "file:///work", "owner-b", 2,
            next.exportEntries("owner-b", "/work"), next.exportedDeletions())
        val restarted = AnswerDrafts()
        restarted.importArchive(persisted, "file:///work")
        restarted.importArchive(archive, "file:///work")
        assertFalse(restarted.recoveries.any { it.id == unknown.id })
    }

    @Test fun `shared archive fixtures preserve content and reject invalid files`() {
        val root = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .first { File(it, "clients/contract/draft-archive-fixtures.json").isFile }
        val cases = Json.parseToJsonElement(File(root, "clients/contract/draft-archive-fixtures.json").readText()).jsonArray
        for (value in cases) {
            val c = value.jsonObject
            val raw = c.getValue("raw").jsonPrimitive.content
            val workspace = c.getValue("workspace").jsonPrimitive.content
            val name = c.getValue("name").jsonPrimitive.content
            if (!c.getValue("valid").jsonPrimitive.boolean) {
                assertThrows(Exception::class.java, { DraftArchive.parse(raw, workspace) }, name)
            } else {
                val a = DraftArchive.parse(raw, workspace)
                val before = Json.encodeToString(a)
                assertEquals(c.getValue("expected"), Json.encodeToJsonElement(a.restore()), name)
                assertEquals(before, Json.encodeToString(a), name)
                assertEquals(a, DraftArchive.parse(before, workspace), name)
            }
        }
    }
}
