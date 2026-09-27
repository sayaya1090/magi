package dev.sayaya.magi.ide.usecase

import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.*
import org.junit.jupiter.api.Assertions.*
import org.junit.jupiter.api.Test
import java.io.File

class DraftArchiveTest {
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
