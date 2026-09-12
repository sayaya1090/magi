package dev.sayaya.magi.ide.transport

import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths

/**
 * 데몬 소켓 파일 경로를 산출한다.
 *
 * 이 구현은 `internal/adapter/daemon/publish.go`의 경로 결정 로직과 1:1로 일치해야 한다.
 * 경로가 1바이트라도 불일치할 경우 데몬이 구동 중임에도 "데몬 없음"으로 오판단하게 된다.
 * 코드 참조는 라인 번호 변동(과거 587, 576, 2474, 2330, 2341 등이 빈번히 이동함)에 취약하므로
 * 원본 구현의 **심볼 식별자(Symbol Name)**를 명시하여 추적성을 보장하며, 이는 CI의 `tools/citecheck.py`에서 검증된다.
 */
object SocketPath {

    /** 유닉스 주소가 받는 길이. macOS 104, Linux 108 바이트인데 magi 가 100 에서 미리 끊는다. */
    const val MAX_SOCKET_PATH = 100

    /**
     * `publish.go`의 `WorkspaceKey`에 대응한다.
     *
     * 절대 경로 변환 후 **심볼릭 링크를 해소(evalSymlinks)**하여 해시를 산출한다.
     * 심볼릭 링크 해소를 거치지 않을 경우 macOS 등에서 `/tmp/x`와 `/private/tmp/x`가 서로 다른 소켓 경로를 갖는 결함이 발생한다.
     */
    fun workspaceKey(workdir: Path): String {
        val abs = runCatching { workdir.toAbsolutePath() }.getOrDefault(workdir)
        val real = evalSymlinks(abs)
        return keyOf(real.toString())
    }

    /**
     * 경로 **문자열**에서 열쇠를 짓는다 — 규칙 전체가 여기 있고, 여기만 있다.
     *
     * ⚠ **이음매인 이유가 측정이다.** 위쪽은 `Paths.get` 을 거치는데, darwin 에서 `c:\\Users\\…` 는
     * 드라이브가 아니라 **파일 이름 하나**다(역슬래시가 평범한 글자다) — `toAbsolutePath()` 가 앞에
     * 작업 디렉터리를 붙여 버려서, 윈도우의 그 철자를 이 기계에서는 `workspaceKey` 로 만들 수가 없다.
     * 규칙은 순수한 문자열 계산이니 문자열을 받는 자리를 열어 두면 **어느 플랫폼에서든** 재인다.
     *
     * 한 번만 맞추고 아래 둘이 **같은 글자를 읽는다**: 이름은 기준 디렉터리를, 해시는 경로 전체를
     * 나르므로 두 반쪽이 철자에 대해 다른 의견을 가지면 그것이 또 세 번째 답이다(드라이브 뿌리에
     * 열린 워크스페이스가 정확히 그 자리다).
     */
    internal fun keyOf(path: String): String {
        val s = driveCased(path)
        return sanitize(baseName(s)) + "-" + shortHash(s)
    }

    /**
     * 드라이브 문자를 **코어가 적는 대로** 적는다.
     *
     * ⚠ **해시는 문자열에 대한 것이고, 호스트가 주는 철자는 믿을 수 없다.** 윈도우의 Go
     * `filepath.EvalSymlinks` 는 드라이브 문자를 대문자로 정규화한다 — 실측 2026-09-12,
     * `c:\Users\…` → `C:\Users\…`. 짝인 VS Code 클라이언트는 `Uri.fsPath` 에서 **소문자**를
     * 받았고(Node 의 `path.resolve` 도 `realpathSync` 도 받은 대소문자를 그대로 둔다), 그래서 한
     * 디렉터리를 두 문자열로 해싱했다: 창은 `daemon-magi-dwj5mk5h.sock` 을 찾고 그 워크스페이스의
     * 데몬은 `daemon-magi-x7wu42uu.sock` 에 있었다(실물 VS Code, 같은 날). 그 실패는 **설계상
     * 조용하다** — 창은 소켓을 못 찾고, 컴패니언이 있는 트리에 대해 「안 돌고 있다」고 말하고,
     * 두 번째를 띄우자고 한다.
     *
     * 이 판이 오늘 그 철자를 받는다는 증거는 없다(이 기계는 darwin 이고, IntelliJ 가 무엇을 주는지
     * 안 쟀다). 그래서 고치는 이유는 증상이 아니라 **계약**이다: 열쇠는 「코어가 적는 대로의 경로」에
     * 대한 것이고, 받은 대로 흘려보내는 것은 호스트가 코어와 같은 철자를 준다는 **운에 기대는** 일이다.
     * 바로 그 운이 짝에서 떨어졌다.
     *
     * POSIX 는 안 스친다 — 절대 경로가 `/` 로 시작하므로 이 규칙이 맞을 자리가 없다.
     */
    internal fun driveCased(p: String): String =
        if (p.length >= 2 && p[0] in 'a'..'z' && p[1] == ':') p[0].uppercaseChar() + p.substring(1) else p

    /**
     * Go 표준 라이브러리 `filepath.EvalSymlinks`의 동작을 재현한다.
     *
     * 자바 표준 `Path.toRealPath()`는 심볼릭 링크 해소 시 대소문자를 무시하는 볼륨(Case-insensitive filesystem)에서
     * 온디스크 대소문자로 경로를 강제 정규화한다 (실측: `.../casedir` 조회 시 `.../CaseDir` 반환).
     * 반면 Go의 `filepath.EvalSymlinks`는 각 경로 요소를 순회하되 기존 입력 대소문자를 보존하므로,
     * `toRealPath()` 사용 시 JVM과 Go 간 해시 입력 문자열이 달라져 소켓 불일치("데몬 없음") 현상이 발생한다.
     * 소켓 파일명의 권한은 데몬(Go)에 있으므로 클라이언트 역시 Go와 동일하게 대소문자 강제 정규화 없이 심볼릭 링크만 해소해야 한다.
     *
     * 또한 어휘적(Lexical) 경로 정규화(`Path.normalize()`)는 심볼릭 링크 경로에서 부정확한 상위 디렉토리로 이동하므로
     * (예: `/a/link/..`에서 `link -> /b/c`일 때 Go는 `/b`로 해석하나 어휘적 처리는 `/a`로 해석함),
     * 경로 컴포넌트 순회 중 `..`를 만났을 때 이미 해소된 목적지(`out`)의 부모로 되감는(rewind) 방식을 취한다.
     */
    internal fun evalSymlinks(path: Path): Path {
        var current = path
        var restarts = 0
        while (true) {
            // 재귀 해소 반복 상한은 Go(255회 제한, 초과 시 ELOOP 반환)와 동일하게 유지한다.
            // 초과 시 WorkspaceKey는 원본 경로를 사용하므로 여기서도 입력 경로를 그대로 반환한다.
            // 사슬 길이가 아닌 총 확장 횟수를 기준으로 하며, 과거 64회 설정 시 70단계 링크 체인 실측에서 Go와의 동작 불일치가 발생했음.
            if (restarts++ > 255) return path
            var out = current.root ?: return path
            val parts = current.toList()
            var restarted = false
            for (i in parts.indices) {
                val name = parts[i].toString()
                if (name == ".") continue
                if (name == "..") {
                    // Go의 어휘적(Lexical) 제거가 아닌 이미 해소된 대상 디렉토리 되감기(rewind) 동작 재현:
                    // 실측: alink -> $TMP/b/c 일 때, EvalSymlinks(".../alink/..") = ".../b" (어휘적 처리는 ".../evt4"로 불일치).
                    // 상대 경로로 시작하는 링크 대상(예: Homebrew 심볼릭 링크 구조)도 이 로직을 통해 정규화된다.
                    out = out.parent ?: out
                    continue
                }
                val next = out.resolve(name)
                if (Files.isSymbolicLink(next)) {
                    val target = runCatching { Files.readSymbolicLink(next) }.getOrNull() ?: return path
                    // 링크 타깃 경로와 나머지 미해소 컴포넌트를 결합하여 처음부터 재순회한다.
                    // 이를 순차 순회로 처리할 경우 중첩된 심볼릭 링크(예: macOS의 /var, /tmp 기본 링크 배치)가 정상 해소되지 않는다:
                    // 실측: hop -> $TMP/real, entry -> $TMP/hop/x 일 때, 순차 순회는 /tmp/.../hop/x로 남아 미해소됨.
                    var rebuilt = if (target.isAbsolute) target else out.resolve(target)
                    for (j in i + 1 until parts.size) rebuilt = rebuilt.resolve(parts[j])
                    current = rebuilt
                    restarted = true
                    break
                }
                // Go의 요소별 lstat 실패 시 원본 경로 폴백 동작 준수 (불완전하게 해소된 부분 경로 반환 방지).
                if (!Files.exists(next)) return path
                out = next
            }
            if (!restarted) return out
        }
    }

    /** `publish.go`의 `SocketPath`에 대응한다. */
    fun of(configDir: Path, workdir: Path, env: (String) -> String? = { System.getenv(it) }): Path =
        (env("MAGI_SOCKET_DIR")?.trim()?.takeIf { it.isNotEmpty() }?.let { Paths.get(it) } ?: configDir).resolve("daemon-" + workspaceKey(workdir) + ".sock")

    /** 데몬이 활성 세션 정보를 기록하는 메타데이터 파일 경로 (`publish.go`의 `SessionFile`). */
    fun sessionFile(socket: Path): Path = Paths.get(socket.toString() + ".session")

    /**
     * `publish.go`의 `tooLong` 검사 로직.
     * OS는 바인딩 실패 시 구체적인 길이 제한 대신 "invalid argument" 에러만 반환하므로,
     * 클라이언트 측에서 경로 길이 초과 원인을 사전 검증하여 안내한다 (null 반환 시 정상).
     */
    fun tooLong(socket: Path): String? {
        val n = socket.toString().toByteArray(Charsets.UTF_8).size
        if (n <= MAX_SOCKET_PATH) return null
        // 설정 디렉토리 전체가 아닌 소켓 디렉토리만 단축 위치로 이전하도록 MAGI_SOCKET_DIR 환경변수를 안내한다.
        // 과거 MAGI_CONFIG_DIR 변경 권고 시 config.toml 및 플러그인 설정이 분리되는 부작용이 발생했음.
        return "소켓 경로가 ${n}바이트이고 OS 가 받는 것은 약 ${MAX_SOCKET_PATH}바이트다 — " +
            "MAGI_SOCKET_DIR 을 더 짧은 곳으로: $socket"
    }

    /**
     * `platform.go`의 `ConfigDir`에 대응한다.
     *
     * macOS Dock 등을 통해 IDE가 기동된 경우 셸 프로필 환경변수가 상속되지 않을 수 있으므로,
     * 설정 등에서 전달된 환경변수 맵을 우선 조회할 수 있도록 람다 파라미터를 제공한다.
     */
    fun configDir(
        env: (String) -> String? = { System.getenv(it) },
        os: String = System.getProperty("os.name").orEmpty(),
        home: String = System.getProperty("user.home").orEmpty(),
    ): Path {
        env("MAGI_CONFIG_DIR")?.trim()?.takeIf { it.isNotEmpty() }?.let { return Paths.get(it) }
        val base = when {
            os.startsWith("Mac") -> Paths.get(home, "Library", "Application Support")
            os.startsWith("Windows") ->
                env("AppData")?.takeIf { it.isNotBlank() }?.let { Paths.get(it) }
                    ?: Paths.get(home, "AppData", "Roaming")
            else -> env("XDG_CONFIG_HOME")?.takeIf { it.isNotBlank() }?.let { Paths.get(it) }
                ?: Paths.get(home, ".config")
        }
        return base.resolve("magi")
    }

    /**
     * `publish.go`의 `sanitize`에 대응한다.
     *
     * Go 원본은 유니코드 룬(Rune) 단위로 순회한다. 자바의 16비트 `char` 단위로 순회할 경우
     * 서러게이트 쌍(Surrogate Pair)이 각각 '-'로 치환되어 2바이트 불일치가 발생하므로,
     * 코드포인트(`codePointAt`) 단위로 순회하여 문자열을 치환한다.
     */
    internal fun sanitize(s: String): String {
        val sb = StringBuilder(s.length)
        var i = 0
        while (i < s.length) {
            val cp = s.codePointAt(i)
            val keep = cp in 'a'.code..'z'.code || cp in 'A'.code..'Z'.code ||
                cp in '0'.code..'9'.code || cp == '-'.code || cp == '_'.code
            sb.append(if (keep) cp.toChar() else '-')
            i += Character.charCount(cp)
        }
        return sb.toString()
    }

    /**
     * `publish.go`의 `shortHash`에 대응한다. FNV-1a 64비트 해시를 base36 8자리 문자열로 인코딩한다.
     *
     * 1. Go 원본과 동일하게 UTF-8 바이트 스트림 단위로 XOR 연산을 수행한다.
     * 2. 자바의 부호 있는(Signed) Long 연산에서 최상위 비트가 설정될 경우 `%` 연산자가 음수를 반환하므로,
     *    `Long.divideUnsigned` 및 `Long.remainderUnsigned`를 사용하여 부호 없는 64비트 정수 연산을 보장한다.
     */
    internal fun shortHash(s: String): String {
        var h = 1469598103934665603L
        for (b in s.toByteArray(Charsets.UTF_8)) {
            h = h xor (b.toLong() and 0xFF)
            h *= 1099511628211L
        }
        val digits = "0123456789abcdefghijklmnopqrstuvwxyz"
        val out = StringBuilder(8)
        repeat(8) {
            out.append(digits[java.lang.Long.remainderUnsigned(h, 36).toInt()])
            h = java.lang.Long.divideUnsigned(h, 36)
        }
        return out.toString()
    }

    /** Go의 `filepath.Base`와 동일한 파일명을 반환한다 (루트 경로에서 fileName이 null인 경우 원본 문자열 반환). */
    internal fun baseName(path: String): String {
        val p = Paths.get(path)
        return p.fileName?.toString() ?: p.toString()
    }
}

/**
 * 데몬 소켓과 함께 기록된 활성 세션 메타데이터(`.session`)를 조회한다.
 *
 * 단순히 "해당 워크스페이스의 최신 세션"을 임의로 연결할 경우 다른 인스턴스나 사용자가 연 대화에
 * 오접속할 위험이 있으므로, 데몬이 소켓 페어로 직접 공표한 세션 ID를 진실 원천으로 삼는다.
 */
object Published {
    fun of(socket: java.nio.file.Path): dev.sayaya.magi.ide.model.Published? = runCatching {
        val text = java.nio.file.Files.readString(SocketPath.sessionFile(socket))
        dev.sayaya.magi.ide.model.Wire.json.decodeFromString(
            dev.sayaya.magi.ide.model.Published.serializer(), text
        )
    }.getOrNull()
}
