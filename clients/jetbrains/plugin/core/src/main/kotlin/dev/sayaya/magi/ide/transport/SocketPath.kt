package dev.sayaya.magi.ide.transport

import java.nio.file.Path
import java.nio.file.Paths

/**
 * 데몬 소켓이 어디 있는지 계산한다.
 *
 * 여기는 magi 의 `internal/adapter/daemon/daemon.go` 를 **그대로 옮긴 자리**다. 한 글자라도
 * 어긋나면 증상이 "여기 데몬 없음"이고, 그 말은 참이 아니면서 참처럼 보인다. 그래서 각 함수에
 * 원본의 위치를 적어 둔다. 원본이 바뀌면 여기도 바뀌어야 하고, 그 사실을 테스트가 잡는다.
 */
object SocketPath {

    /** 유닉스 주소가 받는 길이. macOS 104, Linux 108 바이트인데 magi 가 100 에서 미리 끊는다. */
    const val MAX_SOCKET_PATH = 100

    /**
     * daemon.go:587 `WorkspaceKey`.
     *
     * 절대경로로 만들고 **심링크를 푼 뒤** 해싱한다. 푸는 단계를 빼면 `/tmp/x` 와
     * `/private/tmp/x` 가 서로 다른 소켓을 갖는다. macOS 에서 실제로 그렇게 갈렸다.
     */
    fun workspaceKey(workdir: Path): String {
        val abs = runCatching { workdir.toAbsolutePath() }.getOrDefault(workdir)
        val real = runCatching { abs.toRealPath() }.getOrDefault(abs)
        val s = real.toString()
        return sanitize(baseName(s)) + "-" + shortHash(s)
    }

    /** daemon.go:576 `SocketPath`. */
    fun of(configDir: Path, workdir: Path): Path =
        configDir.resolve("daemon-" + workspaceKey(workdir) + ".sock")

    /** 데몬이 소켓 옆에 자기를 적어 두는 파일. daemon.go:2474 `SessionFile`. */
    fun sessionFile(socket: Path): Path = Paths.get(socket.toString() + ".session")

    /**
     * daemon.go:605 `tooLong`. OS 는 이 실패를 "invalid argument" 로만 말하고 길이 얘기를 안 한다.
     * 그래서 사유를 여기서 만든다. null 이면 문제 없음.
     */
    fun tooLong(socket: Path): String? {
        val n = socket.toString().toByteArray(Charsets.UTF_8).size
        if (n <= MAX_SOCKET_PATH) return null
        return "소켓 경로가 ${n}바이트이고 OS 가 받는 것은 약 ${MAX_SOCKET_PATH}바이트다 — " +
            "MAGI_CONFIG_DIR 을 더 짧은 곳으로: $socket"
    }

    /**
     * platform.go:89 `ConfigDir` 를 옮긴 것.
     *
     * IDE 를 Dock 으로 띄우면 셸 프로필의 환경변수를 못 물려받는다. `MAGI_CONFIG_DIR` 을 쓰는
     * 사용자는 그래서 플러그인과 터미널이 서로 다른 경로를 계산하게 되므로, 호출자가 env 를
     * 넘길 수 있게 열어 둔다(설정에서 받은 값을 여기로 준다).
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
     * daemon.go:2330 `sanitize`.
     *
     * 원본은 **룬** 단위로 돈다. 자바의 char 단위로 돌면 서로게이트 쌍이 '-' 둘이 되어 이름이
     * 갈린다. 그래서 코드포인트로 걷는다.
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
     * daemon.go:2341 `shortHash` — FNV-1a 64비트를 base36 여덟 자리로.
     *
     * 두 곳이 자바에서 틀리기 쉽다. 첫째, 원본이 **바이트**를 XOR 하므로 UTF-8 바이트로 걷는다.
     * 둘째, 나눗셈과 나머지가 **부호 없는** 연산이다. Long 은 부호가 있어 상위 비트가 서면
     * `%` 가 음수를 내므로 `divideUnsigned`/`remainderUnsigned` 를 쓴다. 곱셈은 두 보수라
     * 하위 64비트가 같아 그대로 둔다.
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

    /** Go 의 filepath.Base 와 같은 답을 준다. 루트에서 fileName 이 null 인 것만 다르다. */
    internal fun baseName(path: String): String {
        val p = Paths.get(path)
        return p.fileName?.toString() ?: p.toString()
    }
}
