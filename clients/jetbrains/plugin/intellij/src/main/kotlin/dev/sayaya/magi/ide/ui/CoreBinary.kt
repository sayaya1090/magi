package dev.sayaya.magi.ide.ui

import com.intellij.openapi.diagnostic.Logger
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.util.io.Decompressor
import dev.sayaya.magi.ide.transport.SocketPath
import dev.sayaya.magi.ide.usecase.CoreRelease
import java.nio.file.Files
import java.nio.file.Path
import java.util.Properties

/**
 * **magi 실행 파일을 손에 넣는 자리.**
 *
 * 사용자 요구는 「플러그인만 설치하면 쓸 수 있어야 한다」다. 그래서 셋을 이 순서로 본다:
 * 이미 깔려 있나(PATH) → 전에 받아 둔 것이 있나(캐시) → 없으면 **묻고** 받는다.
 * 받는 자리는 코드가 아니라 설정 파일이 정한다(`magi/core-release.properties`).
 *
 * **묻고 받는다.** 네트워크에서 실행 파일을 가져와 돌리는 일은 조용히 할 것이 아니다.
 * 그리고 받은 것이 낸 것인지 `checksums.txt` 로 확인한다 — 확인 못 하면 안 쓴다.
 * (검역 딱지는 이 경로에 안 붙는다: 실측으로 확인했다 — 브라우저가 아닌 내려받기는
 * `com.apple.quarantine` 를 안 남기고, 받은 바이너리가 그대로 `--version` 을 답했다.)
 */
internal object CoreBinary {

    private val LOG = Logger.getInstance(CoreBinary::class.java)

    val release: CoreRelease by lazy {
        val p = Properties()
        runCatching {
            CoreBinary::class.java.classLoader
                .getResourceAsStream("magi/core-release.properties")!!.use { p.load(it) }
        }.onFailure { LOG.warn("magi: 코어 릴리스 설정을 못 읽었다", it) }
        CoreRelease(p.entries.associate { it.key.toString() to it.value.toString() })
    }

    /** 전에 받아 둔 것이 사는 자리. magi 자신의 설정 디렉토리 아래라 사람이 찾을 수 있다. */
    fun cached(): Path = Shell.configDir()
        .resolve("bin").resolve(release.version)
        .resolve(release.binaryName(System.getProperty("os.name").orEmpty()))

    /**
     * 전에 받아 둔 **아무 판**. 데몬은 제자리에서 자기를 갱신하므로(코어 기본 켜짐) `bin/0.29.0`
     * 안의 파일이 며칠 뒤 0.30 이 되어 있다 — 플러그인의 판 번호만 보고 「없다」고 하면 이미
     * 최신인 것을 또 받는다(리뷰 R12).
     */
    private fun anyCached(): Path? {
        val name = release.binaryName(System.getProperty("os.name").orEmpty())
        val bin = Shell.configDir().resolve("bin")
        return runCatching {
            Files.list(bin).use { s ->
                s.map { it.resolve(name) }.filter { Files.isExecutable(it) }.findFirst().orElse(null)
            }
        }.getOrNull()
    }

    /**
     * 지금 쓸 수 있는 실행 파일. **PATH 가 먼저다** — 사람이 이미 깔아 둔 것이 있으면 그것이
     * 그 사람의 판이고, 우리가 받아 둔 것으로 덮어쓰면 버전이 둘이 된다.
     */
    fun found(): Path? = onPath() ?: cached().takeIf { Files.isExecutable(it) } ?: anyCached()

    private fun onPath(): Path? {
        val name = release.binaryName(System.getProperty("os.name").orEmpty())
        // **사람의 셸이 아는 PATH** 다([Shell]) — IDE 의 것으로 보면 Dock 으로 띄운 macOS 에서
        // 이 가지가 통째로 죽고, 깔려 있는 magi 를 못 찾아 둘째 판을 받는다(리뷰 R1).
        return Shell.path().asSequence()
            .map { java.nio.file.Paths.get(it).resolve(name) }
            .firstOrNull { Files.isExecutable(it) }
    }

    /** 받아서 캐시에 놓는다. 성공하면 그 경로, 실패하면 **사유**를 던진다(조용한 실패 금지). */
    fun download(indicator: ProgressIndicator): Path {
        if (!release.configured) error(MagiBundle.msg("core.get.nourl"))
        val osName = System.getProperty("os.name").orEmpty()
        val asset = release.asset(osName, System.getProperty("os.arch").orEmpty())
            ?: error(MagiBundle.msg("core.get.noasset", osName, System.getProperty("os.arch").orEmpty()))
        val url = release.url(asset) ?: error(MagiBundle.msg("core.get.nourl"))
        val sumsUrl = release.checksumsUrl() ?: error(MagiBundle.msg("core.get.nourl"))

        // **확인할 수 있을 때만 확인한다.** 인증서 검증을 끈 채 체크섬을 받아 오면 그 표도
        // 같은 연결로 오므로, 중간에 있는 쪽이 파일과 표를 둘 다 바꿀 수 있다. 그 상태에서
        // 「체크섬 확인함」이 로그에 남으면 사람은 무결성이 보장됐다고 읽는다 — 약한 검사는
        // 없는 검사보다 나쁘다(사용자 결정). 그래서 그때는 표를 아예 안 받는다. 깨진 아카이브는
        // 어차피 푸는 단계에서 터진다.
        val want = if (!release.verifies) {
            LOG.warn("magi: core.insecure=true — 체크섬을 확인하지 않고 받는다")
            null
        } else {
            indicator.text = MagiBundle.msg("core.get.asking")
            if (!release.sameOrigin(asset)) error(MagiBundle.msg("core.get.mixed"))
            CoreRelease.checksums(read(sumsUrl))[asset] ?: error(MagiBundle.msg("core.get.nosum", asset))
        }

        val tmp = Files.createTempDirectory("magi-core")
        try {
        val archive = tmp.resolve(asset)
        indicator.text = MagiBundle.msg("core.get.downloading", release.version)
        request(url).saveToFile(archive.toFile(), indicator)

        // **다르면 안 쓴다.** 「받긴 받았다」와 「낸 것을 받았다」는 다른 사실이고, 실행할
        // 파일에서 그 둘을 같이 다루면 안 된다.
        if (want != null && !sha256(archive).equals(want, ignoreCase = true)) {
            error(MagiBundle.msg("core.get.badsum", asset))
        }

        indicator.text = MagiBundle.msg("core.get.unpacking")
        val out = tmp.resolve("out")
        if (asset.endsWith(".zip")) Decompressor.Zip(archive).extract(out)
        else Decompressor.Tar(archive).extract(out)

        val name = release.binaryName(osName)
        val bin = Files.walk(out).use { s -> s.filter { it.fileName?.toString() == name }.findFirst() }
            .orElseThrow { IllegalStateException(MagiBundle.msg("core.get.nobinary", name)) }
        val dest = cached()
        Files.createDirectories(dest.parent)
        Files.move(bin, dest, java.nio.file.StandardCopyOption.REPLACE_EXISTING)
        runCatching { dest.toFile().setExecutable(true, false) }
        LOG.info("magi: 코어를 받았다 — $dest (${release.version})")
        return dest
        } finally {
            // 릴리스 tarball 은 수십 MB 다. 재시도마다 한 벌씩 쌓이게 두지 않는다(리뷰 R10).
            runCatching { com.intellij.openapi.util.io.NioFiles.deleteRecursively(tmp) }
        }
    }

    private fun read(url: String): String = request(url).readString()

    /**
     * 이 두 주소에만 쓰는 요청. `core.insecure` 가 켜져 있으면 **여기서만** 인증서와 호스트
     * 이름을 안 따진다 — IDE 전체의 SSL 설정은 안 건드린다. 켤 때마다 로그에 남긴다: 조용히
     * 느슨해지는 것이 이 자리에서 제일 나쁜 모양이다.
     */
    private fun request(url: String): com.intellij.util.io.RequestBuilder {
        val r = com.intellij.util.io.HttpRequests.request(url)
        if (!release.insecure) return r
        LOG.warn("magi: core.insecure=true — 인증서 검증 없이 받는다($url)")
        return r.hostNameVerifier { _, _ -> true }.tuner { c ->
            (c as? javax.net.ssl.HttpsURLConnection)?.sslSocketFactory = lenient()
        }
    }

    /** 아무 인증서나 받는 소켓 팩토리. 이 클래스의 두 요청 밖으로 새지 않는다. */
    private fun lenient(): javax.net.ssl.SSLSocketFactory {
        val trustAll = arrayOf<javax.net.ssl.TrustManager>(object : javax.net.ssl.X509TrustManager {
            override fun checkClientTrusted(c: Array<java.security.cert.X509Certificate>?, a: String?) = Unit
            override fun checkServerTrusted(c: Array<java.security.cert.X509Certificate>?, a: String?) = Unit
            override fun getAcceptedIssuers(): Array<java.security.cert.X509Certificate> = emptyArray()
        })
        return javax.net.ssl.SSLContext.getInstance("TLS")
            .apply { init(null, trustAll, java.security.SecureRandom()) }.socketFactory
    }

    private fun sha256(p: Path): String {
        val md = java.security.MessageDigest.getInstance("SHA-256")
        Files.newInputStream(p).use { ins ->
            val buf = ByteArray(1 shl 16)
            while (true) {
                val n = ins.read(buf)
                if (n <= 0) break
                md.update(buf, 0, n)
            }
        }
        return md.digest().joinToString("") { "%02x".format(it) }
    }
}
