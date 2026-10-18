package dev.sayaya.magi.ide.model

import kotlinx.serialization.decodeFromString
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class OutgoingHandoffTest {
    @Test
    fun `two requests to one companion keep distinct receipts and questions`() {
        val jobs = Wire.json.decodeFromString<Jobs>("""{"outgoing":[
            {"receipt":"r1","to":"ui","request":"bundle?","started":"now"},
            {"receipt":"r2","to":"ui","request":"porting?","started":"now"}
        ]}""")
        assertEquals(listOf("r1", "r2"), jobs.outgoing?.map { it.receipt })
        assertEquals(listOf("bundle?", "porting?"), jobs.outgoing?.map { it.request })
        assertNull(Wire.json.decodeFromString<Jobs>("{}").outgoing)
    }
}
