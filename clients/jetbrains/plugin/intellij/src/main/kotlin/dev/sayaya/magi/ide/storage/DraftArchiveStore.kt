package dev.sayaya.magi.ide.storage

import dev.sayaya.magi.ide.usecase.DraftArchive
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.nio.ByteBuffer
import java.nio.channels.FileChannel
import java.nio.file.Files
import java.nio.file.NoSuchFileException
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.nio.file.StandardOpenOption
import java.nio.file.attribute.PosixFilePermissions

/** Blocking I/O: call from the project's storage executor, never from Swing EDT.
 * The caller must hold exclusive ownership of this file for the instance lifetime.
 */
internal class DraftArchiveStore(private val file: Path, private val workspace: String, private val owner: String) {
    @Synchronized fun load(): DraftArchive? {
        val raw = try { Files.readString(file) } catch (_: NoSuchFileException) { return null }
        return DraftArchive.parse(raw, workspace).also { require(it.owner == owner) { "Foreign draft owner" } }
    }

    @Synchronized fun save(snapshot: DraftArchive) {
        val raw = Json.encodeToString(snapshot)
        val copy = DraftArchive.parse(raw, workspace)
        require(copy.owner == owner) { "Foreign draft owner" }
        val previous = load() // Refuse to overwrite corrupt, unsupported, or foreign data.
        require(previous == null || copy.revision > previous.revision) { "Stale draft archive revision" }
        val directory = file.toAbsolutePath().parent
        Files.createDirectories(directory)
        val attrs = if (Files.getFileStore(directory).supportsFileAttributeView("posix"))
            arrayOf(PosixFilePermissions.asFileAttribute(PosixFilePermissions.fromString("rw-------"))) else emptyArray()
        val temp = Files.createTempFile(directory, "draft-", ".tmp", *attrs)
        try {
            FileChannel.open(temp, StandardOpenOption.WRITE).use { channel ->
                val bytes = ByteBuffer.wrap(raw.toByteArray(Charsets.UTF_8))
                while (bytes.hasRemaining()) channel.write(bytes)
                channel.force(true)
            }
            // Unsupported atomic replacement is a visible failure, never delete-then-write.
            Files.move(temp, file, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
        } finally { Files.deleteIfExists(temp) }
    }
}
