package dev.sayaya.magi.ide.storage

import dev.sayaya.magi.ide.usecase.DraftArchive
import org.junit.Assert.*
import org.junit.Test
import java.nio.file.Files

class DraftArchiveStoreTest {
    private fun snapshot(revision: Long) = DraftArchive(1, "file:///work", "owner", revision, emptyList(), emptyList())

    @Test fun recreationCorruptionAndRevisionOrdering() {
        val dir = Files.createTempDirectory("magi-drafts-")
        try {
            val file = dir.resolve("drafts.json")
            val store = DraftArchiveStore(file, "file:///work", "owner")
            assertNull(store.load())
            store.save(snapshot(1))
            assertEquals(snapshot(1), DraftArchiveStore(file, "file:///work", "owner").load())
            assertThrows(IllegalArgumentException::class.java) { store.save(snapshot(1)) }
            assertThrows(IllegalArgumentException::class.java) { DraftArchiveStore(file, "file:///other", "owner").load() }
            assertThrows(IllegalArgumentException::class.java) { DraftArchiveStore(file, "file:///work", "other").load() }
            assertEquals(snapshot(1), store.load())
            Files.writeString(file, "{broken")
            assertThrows(Exception::class.java) { store.save(snapshot(2)) }
            assertEquals("{broken", Files.readString(file))
            Files.delete(file)
            store.save(snapshot(3))
            assertEquals(snapshot(3), store.load())
            Files.list(dir).use { assertEquals(1L, it.count()) }
        } finally { dir.toFile().deleteRecursively() }
    }

    @Test fun failedWriteDoesNotDestroyDataOrPreventRetry() {
        val dir = Files.createTempDirectory("magi-drafts-")
        try {
            val blocker = dir.resolve("blocked")
            Files.writeString(blocker, "keep")
            val store = DraftArchiveStore(blocker.resolve("drafts.json"), "file:///work", "owner")
            assertThrows(Exception::class.java) { store.save(snapshot(1)) }
            assertEquals("keep", Files.readString(blocker))
            Files.delete(blocker)
            store.save(snapshot(1))
            assertEquals(snapshot(1), store.load())
        } finally { dir.toFile().deleteRecursively() }
    }
}
