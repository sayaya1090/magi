package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File
import java.util.Properties

/**
 * 받아올 자리를 정하는 규칙. **설정 파일 실물을 읽어서** 잰다 — 규칙만 재고 파일은 안 보면,
 * 누가 그 파일의 열쇠 이름을 바꿔도 시험은 초록이고 사용자만 「받을 수 없다」를 본다.
 */
class CoreReleaseTest {

    /** 플러그인에 실제로 실리는 그 파일. 시험이 짐작한 값이 아니라 배포되는 값을 본다. */
    private val shipped: Map<String, String> by lazy {
        val f = File(
            File(System.getProperty("user.dir")).parentFile,
            "intellij/src/main/resources/magi/core-release.properties",
        )
        assertTrue(f.isFile, "${f.absolutePath} 가 없다 — 이 시험이 아무것도 안 보고 있다")
        Properties().apply { f.inputStream().use { load(it) } }
            .entries.associate { it.key.toString() to it.value.toString() }
    }

    @Test
    fun `배포되는 설정이 네 열쇠를 다 갖는다`() {
        for (k in listOf("core.version", "core.asset", "core.url", "core.checksums")) {
            assertTrue(shipped[k]?.isNotBlank() == true, "설정에 「$k」 가 없다 — 받아올 자리를 모른다")
        }
    }

    @Test
    fun `이 기계들의 자산 이름은 릴리스가 실제로 내는 이름이다`() {
        val r = CoreRelease(shipped)
        // 이 여섯이 v0.29.0 릴리스에 실제로 올라간 자산이다(실측).
        assertEquals("magi_darwin_arm64.tar.gz", r.asset("Mac OS X", "aarch64"))
        assertEquals("magi_darwin_amd64.tar.gz", r.asset("Mac OS X", "x86_64"))
        assertEquals("magi_linux_arm64.tar.gz", r.asset("Linux", "aarch64"))
        assertEquals("magi_linux_amd64.tar.gz", r.asset("Linux", "amd64"))
        assertEquals("magi_windows_amd64.zip", r.asset("Windows 11", "amd64"))
        assertEquals("magi_windows_arm64.zip", r.asset("Windows 11", "aarch64"))
    }

    @Test
    fun `모르는 기계는 지어낸 이름 대신 모른다고 한다`() {
        val r = CoreRelease(shipped)
        assertNull(r.asset("SunOS", "aarch64"), "없는 판을 받으러 가면 404 가 「네트워크 오류」로 보인다")
        assertNull(r.asset("Linux", "riscv64"))
    }

    @Test
    fun `주소에 안 채운 자리가 남으면 안 준다`() {
        val r = CoreRelease(shipped)
        val u = r.url("magi_linux_amd64.tar.gz")!!
        assertTrue(u.startsWith("https://"), "받는 주소가 https 가 아니다: $u")
        assertTrue("magi_linux_amd64.tar.gz" in u && shipped["core.version"]!! in u, u)
        val v = mapOf("core.version" to "1.0")
        assertNull(CoreRelease(v + ("core.url" to "https://x/{who}")).url("a"), "못 채운 자리를 그대로 부르지 않는다")
        // 규칙을 **여기서** 잰다. 배포되는 값이 https 인 것만 재면, 그 값을 갈아 끼우는 사람은
        // 이 시험을 안 돌린다 — 맞는 답이 틀린 근거로 맞는 자리다(리뷰 R7).
        assertNull(CoreRelease(v + ("core.url" to "http://x/a")).url("a"), "http 로 실행 파일을 받지 않는다")
        assertNull(CoreRelease(mapOf("core.url" to "https://x/a")).url("a"), "버전이 비면 v/ 로 끝나는 404 가 된다")
        // 자산 이름도 같은 엄격도여야 한다 — 지어낸 이름은 조용한 404 다(리뷰 R9).
        assertNull(
            CoreRelease(mapOf("core.asset" to "magi_{os}_{arch}_{flavor}.{ext}")).asset("Linux", "amd64"),
            "안 채운 자리가 남은 이름을 그대로 받으러 가지 않는다",
        )
        // 체크섬과 파일이 다른 데서 오면 그 확인은 확인이 아니다.
        assertEquals(
            false,
            CoreRelease(
                v + mapOf(
                    "core.asset" to "a.tar.gz", "core.url" to "https://one/{asset}",
                    "core.checksums" to "https://two/checksums.txt",
                ),
            ).sameOrigin("a.tar.gz"),
            "근원과 대상이 다른 출처인데 통과했다",
        )
    }

    @Test
    fun `배포되는 설정은 인증서 검증을 켠 채로 나간다`() {
        // 기본이 꺼짐이어야 한다. 사설 CA 는 사정이지만, 그 사정은 **그 기계에서 켜는 것**이지
        // 모두에게 배포되는 값이 아니다.
        assertEquals(false, CoreRelease(shipped).insecure, "배포 설정이 인증서 검증을 끄고 나간다")
    }

    @Test
    fun `켜는 말과 안 켜는 말을 가른다`() {
        for (on in listOf("true", "TRUE", " yes ", "1", "on")) {
            assertTrue(CoreRelease(mapOf("core.insecure" to on)).insecure, "「$on」 을 못 읽었다")
        }
        for (off in listOf("false", "no", "0", "", "아무거나")) {
            assertEquals(false, CoreRelease(mapOf("core.insecure" to off)).insecure, "「$off」 를 켬으로 읽었다")
        }
        assertEquals(false, CoreRelease(emptyMap()).insecure, "값이 없으면 꺼짐이다")
    }

    @Test
    fun `확인은 인증서 검증의 뒷면이다`() {
        // 둘이 갈리면 「인증서는 안 따지는데 체크섬은 확인한다」는 어중간한 상태가 생긴다 —
        // 그건 보장처럼 보이기만 하는 자리다.
        assertTrue(CoreRelease(shipped).verifies, "배포 설정이 확인을 끄고 나간다")
        assertEquals(false, CoreRelease(mapOf("core.insecure" to "true")).verifies)
    }

    @Test
    fun `최신을 고를 때 남의 열차를 안 탄다`() {
        // 실측한 함정 그대로다: 날짜상 최신은 `web-v0.2.0` 이었고, 코어 자산은 그 태그에 없다.
        val json = """
            [{"tag_name":"web-v0.2.0"},{"tag_name":"jetbrains-v0.1.0"},
             {"tag_name":"v0.29.0"},{"tag_name":"v0.28.0"}]
        """.trimIndent()
        assertEquals("0.29.0", CoreRelease(shipped).pickLatest(json))
    }

    @Test
    fun `판 번호는 숫자로 견준다`() {
        // 문자열로 견주면 `0.9` 가 `0.10` 을 이긴다 — 열 번째 판이 나오는 날 조용히 옛것을 받는다.
        val json = """[{"tag_name":"v0.9.0"},{"tag_name":"v0.10.0"}]"""
        assertEquals("0.10.0", CoreRelease(shipped).pickLatest(json))
    }

    @Test
    fun `목록 순서에 안 기댄다`() {
        val json = """[{"tag_name":"v0.28.0"},{"tag_name":"v0.29.0"},{"tag_name":"v0.27.0"}]"""
        assertEquals("0.29.0", CoreRelease(shipped).pickLatest(json))
    }

    @Test
    fun `고를 것이 없으면 null 이고 그때는 바닥으로 간다`() {
        assertNull(CoreRelease(shipped).pickLatest("""[{"tag_name":"web-v0.2.0"}]"""))
        assertNull(CoreRelease(shipped).pickLatest("쓰레기"))
    }

    @Test
    fun `배포되는 설정은 최신을 따라간다`() {
        val r = CoreRelease(shipped)
        assertTrue(r.tracksLatest, "핀만 보면 처음 쓰는 사람이 옛 판을 받는다")
        assertTrue(r.releasesUrl()?.startsWith("https://") == true, "최신을 물어볼 자리가 없다")
        assertEquals("0.29.0", r.at("0.29.0").version)
    }

    @Test
    fun `체크섬 표는 두 칸짜리 줄만 받는다`() {
        val t = CoreRelease.checksums(
            """
            287404a723fcf6a6798b319847b2bf9870adce6aa1030e19b1d945605484e5a4  magi_darwin_arm64.tar.gz
            헛소리
            deadbeef  짧은해시
            """.trimIndent(),
        )
        assertEquals(1, t.size, "형식이 어긋난 줄이 표에 들어왔다: $t")
        assertEquals(
            "287404a723fcf6a6798b319847b2bf9870adce6aa1030e19b1d945605484e5a4",
            t["magi_darwin_arm64.tar.gz"],
        )
    }

    @Test
    fun `실행 파일 이름은 윈도우만 다르다`() {
        val r = CoreRelease(shipped)
        assertEquals("magi", r.binaryName("Mac OS X"))
        assertEquals("magi", r.binaryName("Linux"))
        assertEquals("magi.exe", r.binaryName("Windows 11"))
    }
}
