package dev.sayaya.magi.ide.usecase

/**
 * **어느 판의 코어를, 어디서 받아오나.** 값은 코드가 아니라 설정 파일에서 온다
 * (`magi/core-release.properties`) — 저장소를 옮기거나 사내 미러를 쓰는 사람이 코틀린을
 * 고치지 않고 그 파일만 갈아 끼울 수 있어야 한다(사용자 지시).
 *
 * 자산 이름 규약은 **코어 릴리스가 이미 쓰는 것**이다(goreleaser 자산, 자기업데이트가 읽는
 * 그 이름). 여기서 새 규약을 만들면 같은 사실이 두 벌이 되고, 그중 하나는 언젠가 갈라진다.
 *
 * 순수 함수만 둔다 — 네트워크도 파일도 안 만진다. 그래서 여기서 시험이 된다.
 */
class CoreRelease(private val conf: Map<String, String>) {

    val version: String get() = conf["core.version"].orEmpty()

    /**
     * 인증서 검증을 끄고 받는가. **기본은 꺼짐**이고, 켜는 자리는 설정 파일이다.
     *
     * 사내 미러가 사설 CA 로 서 있으면 검증이 막는다 — 실제로 흔한 사정이라 길을 둔다. 다만
     * **무엇을 잃는지 분명히 한다**: 체크섬을 같은 채널로 받아 오므로, 중간에 있는 쪽은 파일과
     * 체크섬을 **둘 다** 바꿀 수 있다. 그러면 sha256 검증은 「전송이 깨지지 않았다」까지만
     * 말하고 「낸 것이 맞다」는 못 말한다. 그래서 이 값이 켜지면 동의 대화가 그 사실을 적는다.
     *
     * 그래서 이 값이 켜지면 **체크섬을 아예 안 받는다**([verifies]). 약한 검사는 없는 검사보다
     * 나쁘다 — 「체크섬 확인함」이 로그에 남으면 사람은 무결성이 보장됐다고 읽는다. 못 하는
     * 것을 하는 척하지 않는 편이 낫고, 깨진 아카이브는 어차피 푸는 단계에서 터진다.
     *
     * 평문 http 는 여기서도 안 받는다(`fill` 이 https 만 통과시킨다) — 사설 CA 는 사정이지만
     * 평문은 사정이 아니다.
     */
    val insecure: Boolean get() = conf["core.insecure"]?.trim()?.lowercase() in setOf("true", "yes", "1", "on")

    /**
     * 받은 것을 확인하나. [insecure] 의 뒷면이고 **이름을 따로 준 이유**가 있다 — 부르는 쪽이
     * 「인증서를 안 따진다」가 아니라 「확인을 안 한다」를 물어야 하는 자리이기 때문이다.
     */
    val verifies: Boolean get() = !insecure

    /**
     * 설정이 다 실렸나. **안 실린 것과 「이 기계에 판이 없는 것」은 다른 사실이다** — 가르지
     * 않으면 리소스를 못 읽었을 때 사용자가 자기 기계 탓이라고 듣는다(리뷰 R8).
     */
    val configured: Boolean
        get() = listOf("core.version", "core.asset", "core.url", "core.checksums")
            .all { conf[it]?.isNotBlank() == true }

    /**
     * 이 기계가 받을 자산 이름. 모르는 조합이면 **null** — 지어낸 이름으로 404 를 받아
     * 「네트워크 오류」라고 말하느니, 「이 기계에 맞는 판이 없다」고 말하는 편이 낫다.
     */
    fun asset(osName: String, archName: String): String? {
        val os = os(osName) ?: return null
        val arch = arch(archName) ?: return null
        // 윈도우만 zip 이다(goreleaser 기본) — 확장자를 OS 가 정한다.
        val ext = if (os == "windows") "zip" else "tar.gz"
        return conf["core.asset"].orEmpty()
            .replace("{os}", os).replace("{arch}", arch).replace("{ext}", ext)
            .takeIf { it.isNotBlank() }
            // 안 채운 자리가 남으면 **이름을 지어낸 것**이다(리뷰 R9). 주소 쪽만 막고 여기를
            // 안 막아 두 경로의 엄격도가 갈려 있었다 — 지어낸 이름은 조용한 404 로 끝난다.
            ?.takeIf { "{" !in it }
    }

    fun url(asset: String): String? = fill(conf["core.url"], asset)
    fun checksumsUrl(): String? = fill(conf["core.checksums"], "")

    private fun fill(template: String?, asset: String): String? {
        if (version.isBlank()) return null // 빈 버전은 `…/download/v/…` 라는 조용한 404 가 된다
        return template?.takeIf { it.isNotBlank() }
            ?.replace("{version}", version)
            ?.replace("{asset}", asset)
            ?.takeIf { "{" !in it }
            // **https 만.** 실행할 파일을 받아 오는 길이라 스킴을 규칙으로 못박는다. 배포되는
            // 값이 https 인 것만 재면, 그 값을 갈아 끼우는 사람은 그 시험을 안 돌린다(리뷰 R7).
            ?.takeIf { it.startsWith("https://") }
    }

    /**
     * 받을 곳의 호스트. 동의 대화가 **어디서 받는지**를 보여야 한다 — 미러로 갈아 끼울 수
     * 있다는 것이 이 설계의 자랑인데, 갈아 끼운 사실이 사람에게 안 보이면 자랑이 아니다.
     */
    fun host(asset: String): String? = url(asset)?.let {
        runCatching { java.net.URI(it).host }.getOrNull()
    }

    /**
     * 체크섬과 파일이 **같은 출처**인가. 체크섬이 신뢰의 근원인데 근원과 대상이 다른 데서 오면
     * 그 확인은 확인이 아니다.
     */
    fun sameOrigin(asset: String): Boolean {
        val a = host(asset) ?: return false
        val b = checksumsUrl()?.let { runCatching { java.net.URI(it).host }.getOrNull() } ?: return false
        return a == b
    }

    /** 받은 파일 안에서 실행 파일의 이름. 윈도우만 `.exe`. */
    fun binaryName(osName: String): String = if (os(osName) == "windows") "magi.exe" else "magi"

    private fun os(name: String): String? = when {
        name.startsWith("Mac", true) || name.startsWith("Darwin", true) -> "darwin"
        name.startsWith("Windows", true) -> "windows"
        name.startsWith("Linux", true) -> "linux"
        else -> null
    }

    /** JVM 의 `os.arch` 어휘를 릴리스 자산의 어휘로. 그 둘은 같은 말을 다르게 부른다. */
    private fun arch(name: String): String? = when (name.lowercase()) {
        "aarch64", "arm64" -> "arm64"
        "x86_64", "amd64" -> "amd64"
        else -> null
    }

    companion object {
        /**
         * `checksums.txt` 한 장에서 이름 → sha256. **받은 것이 낸 것인지 확인할 유일한 근거**라,
         * 형식이 어긋나면 조용히 빈 표를 주지 않고 그 줄을 버린다(빈 표는 검증을 건너뛰는 것과
         * 같아 보이는데, 부르는 쪽은 「없으면 못 받는다」로 다뤄야 한다).
         */
        fun checksums(text: String): Map<String, String> = text.lineSequence()
            .mapNotNull { line ->
                val parts = line.trim().split(Regex("\\s+"))
                if (parts.size == 2 && parts[0].length == 64) parts[1].removePrefix("*") to parts[0] else null
            }.toMap()
    }
}
