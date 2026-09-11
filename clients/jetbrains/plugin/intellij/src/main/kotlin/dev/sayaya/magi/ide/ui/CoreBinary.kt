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
 * magi 코어 실행 파일 탐색 및 자동 다운로드 관리자.
 *
 * 1. 시스템 PATH 조회 ([Shell.path])
 * 2. 로컬 캐시 디렉토리 확인 ([cached], [anyCached])
 * 3. 사용자 승인 후 원격 릴리스 다운로드 (`magi/core-release.properties`)
 *
 * 원격 다운로드 시 `checksums.txt` 기반 무결성 검증을 필수로 수행한다.
 * (IDE 플러그인 HTTP 클라이언트를 통한 다운로드는 브라우저 다운로드와 달리 macOS의 `com.apple.quarantine` 속성이 부착되지 않음이 실측 확인됨)
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

    /** 지정 버전의 캐시 바이너리 경로 (사용자 설정 디렉토리 하위 `bin/<version>/`에 저장). */
    fun cached(version: String = release.version): Path = Shell.configDir()
        .resolve("bin").resolve(version)
        .resolve(release.binaryName(System.getProperty("os.name").orEmpty()))

    /**
     * 캐시 디렉토리 내 임의의 실행 가능한 바이너리를 검색한다.
     * 데몬의 자체 업데이트 기능으로 인해 디렉토리 버전명과 실제 바이너리 버전이 일치하지 않을 수 있으므로,
     * 불필요한 중복 다운로드를 방지하기 위해 기존 바이너리의 존재를 검사한다 (리뷰 R12).
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
     * 실행 가능한 magi 바이너리를 탐색한다.
     * 사용자 환경에 직접 설치된 버전을 우선 존중하기 위해 PATH를 캐시보다 먼저 검색한다.
     */
    fun found(): Path? = onPath() ?: cached().takeIf { Files.isExecutable(it) } ?: anyCached()

    private fun onPath(): Path? {
        val name = release.binaryName(System.getProperty("os.name").orEmpty())
        // macOS GUI 런처 환경에서도 정확한 탐색을 보장하기 위해 로그인 셸의 PATH를 사용한다 (리뷰 R1).
        return Shell.path().asSequence()
            .map { java.nio.file.Paths.get(it).resolve(name) }
            .firstOrNull { Files.isExecutable(it) }
    }

    /** 선택된 릴리스 정보 및 자산 다운로드 URL. */
    class Pick(val release: CoreRelease, val assetUrl: String?)

    /**
     * 다운로드 대상 릴리스 버전을 결정한다.
     * `track=latest` 설정 시 원격 릴리스 목록을 조회하여 최신 버전을 선별하며, 조회 실패 시 번들 기본 버전으로 폴백한다.
     */
    fun resolve(): Pick {
        val r = release
        // 최신 버전 조회 엔드포인트가 설정된 경우 우선 조회
        if (r.tracksLatest) r.latestUrl()?.let { u ->
            runCatching { r.readLatest(read(u)) }.getOrNull()?.let { v ->
                if (v != r.version) LOG.info("magi: 코어가 알린 최신은 $v (설정의 바닥은 ${r.version})")
                return Pick(r.at(v), null)
            }
            LOG.info("magi: 코어의 판 알림을 못 읽었다 — 릴리스 목록으로 간다")
        }
        val url = r.releasesUrl()
        if (!r.tracksLatest && url == null) return Pick(r, null)
        val json = runCatching { read(url ?: return Pick(r, null)) }.getOrElse { e ->
            LOG.info("magi: 릴리스 목록을 못 물어봤다 — ${r.version} 으로 간다 (${e.message})")
            return Pick(r, null)
        }
        val picked = (if (r.tracksLatest) r.pickLatest(json) else null) ?: r.version
        if (picked != r.version) LOG.info("magi: 최신 코어는 $picked (설정의 바닥은 ${r.version})")
        val at = r.at(picked)
        // 사설 저장소인 경우 자산 API 주소를 추출하여 사용
        val asset = at.asset(System.getProperty("os.name").orEmpty(), System.getProperty("os.arch").orEmpty())
        val direct = asset?.let { at.assetUrlFrom(json, "v$picked", it) }
        return Pick(at, direct)
    }

    /** 원격 릴리스를 다운로드하여 캐시에 배치한다. */
    fun download(indicator: ProgressIndicator, pick: Pick = Pick(this.release, null)): Path {
        val release = pick.release
        if (!release.configured) error(MagiBundle.msg("core.get.nourl"))
        val osName = System.getProperty("os.name").orEmpty()
        val asset = release.asset(osName, System.getProperty("os.arch").orEmpty())
            ?: error(MagiBundle.msg("core.get.noasset", osName, System.getProperty("os.arch").orEmpty()))
        val url = pick.assetUrl ?: release.url(asset) ?: error(MagiBundle.msg("core.get.nourl"))
        val sumsUrl = release.checksumsUrl() ?: error(MagiBundle.msg("core.get.nourl"))

        // core.insecure=true 설정 시 인증되지 않은 연결로 체크섬을 수신하는 위장 위험을 방지하기 위해 체크섬 검증을 건너뛴다.
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
            request(url, octet = pick.assetUrl != null).saveToFile(archive.toFile(), indicator)

            // 다운로드 아카이브 SHA-256 체크섬 검증
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
            val dest = cached(release.version)
            Files.createDirectories(dest.parent)
            Files.move(bin, dest, java.nio.file.StandardCopyOption.REPLACE_EXISTING)
            runCatching { dest.toFile().setExecutable(true, false) }
            LOG.info("magi: 코어를 받았다 — $dest (${release.version})")
            return dest
        } finally {
            // 디스크 공간 낭비 방지를 위해 임시 다운로드 디렉토리를 즉시 정리한다 (리뷰 R10).
            runCatching { com.intellij.openapi.util.io.NioFiles.deleteRecursively(tmp) }
        }
    }

    /**
     * 이 바이너리가 무엇을 할 수 있는지 — **띄워 보기 전에 묻는다.**
     *
     * `magi ide-bridge --features` 는 데몬에 안 붙고 한 줄 JSON 으로 답한다(docs/IDE_BRIDGE §5).
     * 구형 바이너리는 그 옵션을 **거절**하고 0 이 아닌 코드로 끝나며, 그것이 「이 빌드엔 그
     * 기능이 없다」의 유일한 근거다 — 도움말 글자에서 낱말을 찾는 것은 추측이다.
     *
     * 어떤 실패든 빈 집합이다(거절·깨진 줄·시한 초과). 부르는 쪽에게 셋은 같은 뜻이다: 이
     * 바이너리에 새 모드를 쓰지 마라.
     *
     * 계약의 5초를 여기서도 지킨다. 탐침이 답을 안 주면 그 바이너리는 어차피 안 될 것이다.
     */
    fun features(bin: Path): Set<String> = runCatching {
        val p = ProcessBuilder(bin.toString(), "ide-bridge", "--features")
            .redirectErrorStream(false).start()
        val line = p.inputStream.bufferedReader(Charsets.UTF_8).use { it.readLine() }.orEmpty()
        if (!p.waitFor(5, java.util.concurrent.TimeUnit.SECONDS)) {
            p.destroyForcibly()
            return emptySet()
        }
        if (p.exitValue() != 0) return emptySet()
        val got = kotlinx.serialization.json.Json.parseToJsonElement(line)
            .let { it as? kotlinx.serialization.json.JsonObject } ?: return emptySet()
        val list = got["features"] as? kotlinx.serialization.json.JsonArray ?: return emptySet()
        list.mapNotNull { (it as? kotlinx.serialization.json.JsonPrimitive)?.content }.toSet()
    }.getOrElse {
        LOG.info("magi: 기능 조회 실패 — 새 모드 없이 간다 (${it.message})")
        emptySet()
    }

    private fun read(url: String): String = request(url).readString()

    /**
     * 인증 토큰 획득. 보안을 위해 설정에는 환경 변수 이름만 두고 실제 토큰 값은 셸 환경에서 조회한다 ([Shell]).
     */
    private fun token(): String? = release.tokenEnv()?.let { Shell.env()[it] }?.takeIf { it.isNotBlank() }

    /**
     * HTTP 요청 빌더 생성.
     * Authorization, Accept 헤더 및 core.insecure 설정에 따른 SSL 컨텍스트를 단일 tuner 람다에서 구성한다.
     */
    private fun request(url: String, octet: Boolean = false): com.intellij.util.io.RequestBuilder {
        val t = token()
        if (t != null) LOG.info("magi: 자격 증명을 실어 보낸다(${release.tokenEnv()})")
        val ssl = if (release.insecure) lenient() else null
        if (ssl != null) LOG.warn("magi: core.insecure=true — 인증서 검증 없이 받는다($url)")
        var r = com.intellij.util.io.HttpRequests.request(url).tuner { c ->
            if (t != null) c.setRequestProperty("Authorization", release.tokenScheme() + " " + t)
            // 사설 저장소 바이너리 수신을 위해 octet-stream 헤더 지정
            if (octet) c.setRequestProperty("Accept", "application/octet-stream")
            if (ssl != null) (c as? javax.net.ssl.HttpsURLConnection)?.sslSocketFactory = ssl
        }
        if (ssl != null) r = r.hostNameVerifier { _, _ -> true }
        return r
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
