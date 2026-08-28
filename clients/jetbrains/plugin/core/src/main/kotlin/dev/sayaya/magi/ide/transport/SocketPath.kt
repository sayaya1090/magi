package dev.sayaya.magi.ide.transport

import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths

/**
 * 데몬 소켓이 어디 있는지 계산한다.
 *
 * 여기는 magi 의 `internal/adapter/daemon/daemon.go` 를 **그대로 옮긴 자리**다. 한 글자라도
 * 어긋나면 증상이 "여기 데몬 없음"이고, 그 말은 참이 아니면서 참처럼 보인다. 그래서 각 함수에
 * 원본을 **심볼 이름으로** 짚는다. 줄 번호로 적었다가 하루 만에 다섯 개가 다 밀렸다
 * (587→616, 576→605, 2474→2569, 2330→2425, 2341→2436). 원본을 추적하는 것이 이 파일의 존재
 * 이유인데 가리키는 손가락이 제일 먼저 썩으면 곤란하다. 심볼은 grep 으로 늘 찾힌다.
 */
object SocketPath {

    /** 유닉스 주소가 받는 길이. macOS 104, Linux 108 바이트인데 magi 가 100 에서 미리 끊는다. */
    const val MAX_SOCKET_PATH = 100

    /**
     * daemon.go 의 `WorkspaceKey`.
     *
     * 절대경로로 만들고 **심링크를 푼 뒤** 해싱한다. 푸는 단계를 빼면 `/tmp/x` 와
     * `/private/tmp/x` 가 서로 다른 소켓을 갖는다. macOS 에서 실제로 그렇게 갈렸다.
     */
    fun workspaceKey(workdir: Path): String {
        val abs = runCatching { workdir.toAbsolutePath() }.getOrDefault(workdir)
        val real = evalSymlinks(abs)
        val s = real.toString()
        return sanitize(baseName(s)) + "-" + shortHash(s)
    }

    /**
     * Go 의 `filepath.EvalSymlinks` 와 같은 걸음. **`toRealPath()` 를 쓰면 안 된다.**
     *
     * 자바의 `toRealPath()` 는 심링크를 풀면서 **대소문자까지 정규화한다.** 실측: 대소문자를
     * 무시하는 볼륨에서 `.../casedir` 을 물으면 `.../CaseDir` 을 돌려준다. Go 는 lstat 로
     * 컴포넌트를 걷기만 해서 준 대로 돌려준다. 그러면 IDE 가 온디스크와 다른 대소문자로 경로를
     * 넘겼을 때 JVM 과 Go 가 서로 다른 문자열을 해싱하고, 소켓이 둘로 갈리고, 증상은
     * **"여기 데몬 없음"** 이다 — 심링크를 푸는 단계가 애초에 막으려던 바로 그 증상이다.
     *
     * 소켓 이름의 주인은 데몬이므로 맞춰야 하는 쪽은 이쪽이다. 그래서 심링크만 푼다.
     *
     * `normalize()` 는 쓰지 않는다. 그것은 `..` 를 **어휘적으로** 지우는데 Go 는 풀고 나서
     * 물러난다 — `/a/link/..` 에서 `link → /b/c` 면 Go 는 `/b`, 어휘적 처리는 `/a` 다. 대신
     * `..` 를 걷는 중에 처리해서 **해소된 dest** 를 되감는다. 그러면 입력의 `..` 와 링크
     * 타깃의 `..` 가 같은 규칙 하나로 덮인다.
     */
    internal fun evalSymlinks(path: Path): Path {
        var current = path
        var restarts = 0
        while (true) {
            // 전체 반복 상한, Go 와 **같은 숫자**로 둔다. Go 는 255번째 확장에서 ELOOP 를 내고
            // 그러면 WorkspaceKey 가 안 푼 경로를 쓰므로 여기서도 입력을 그대로 돌려준다. 세는
            // 단위도 같다 — 사슬 길이가 아니라 총 확장 횟수다. 한때 64였는데, 주석이 255라 적고
            // 코드가 다른 숫자면 다음 사람이 둘 중 무엇이 의도인지 물어야 한다(실측: 70단 사슬에서
            // Go 는 끝까지 풀고 64 판본은 입력을 냈다).
            if (restarts++ > 255) return path
            var out = current.root ?: return path
            val parts = current.toList()
            var restarted = false
            for (i in parts.indices) {
                val name = parts[i].toString()
                if (name == ".") continue
                if (name == "..") {
                    // Go 는 `..` 를 **어휘적으로** 지우지 않고 **이미 해소된 dest** 를 한 칸
                    // 되감는다. 실측: alink -> $TMP/b/c 일 때
                    //   EvalSymlinks(".../alink/..") = ".../b"      ← 되감기
                    //   어휘적 처리라면                ".../evt4"    ← 다른 답
                    // 링크 타깃이 `..` 로 시작하는 경우(Homebrew 가 그리는 모양)도 여기서 접힌다.
                    // 앞 컴포넌트의 존재는 이미 아래 exists 가 확인했으므로 다시 보지 않는다.
                    out = out.parent ?: out
                    continue
                }
                val next = out.resolve(name)
                if (Files.isSymbolicLink(next)) {
                    val target = runCatching { Files.readSymbolicLink(next) }.getOrNull() ?: return path
                    // **타깃 + 남은 컴포넌트로 경로를 새로 만들고 처음부터 다시 걷는다.**
                    // 이 자리를 이어 걷기로 두면 절대 타깃 안쪽의 링크가 안 풀린다. 실측:
                    //   hop -> $TMP/real, entry -> $TMP/hop/x 일 때
                    //   go     = /private/tmp/…/real/x
                    //   이어 걷기 = /tmp/…/hop/x      ← hop 도 /tmp 도 안 풀린 답
                    // macOS 는 /var·/tmp 가 이미 심링크라 특이한 배치가 아니다.
                    var rebuilt = if (target.isAbsolute) target else out.resolve(target)
                    for (j in i + 1 until parts.size) rebuilt = rebuilt.resolve(parts[j])
                    current = rebuilt
                    restarted = true
                    break
                }
                // Go 는 컴포넌트마다 lstat 하고 하나라도 없으면 에러를 낸다. 그러면 WorkspaceKey 는
                // `if err == nil` 이라 안 푼 abs 를 그대로 쓴다. 반쯤 푼 경로는 Go 가 절대 내지
                // 않는 답이라 여기서도 내면 안 된다.
                if (!Files.exists(next)) return path
                out = next
            }
            if (!restarted) return out
        }
    }

    /** daemon.go 의 `SocketPath`. */
    fun of(configDir: Path, workdir: Path): Path =
        configDir.resolve("daemon-" + workspaceKey(workdir) + ".sock")

    /** 데몬이 소켓 옆에 자기를 적어 두는 파일. daemon.go 의 `SessionFile`. */
    fun sessionFile(socket: Path): Path = Paths.get(socket.toString() + ".session")

    /**
     * daemon.go 의 `tooLong`. OS 는 이 실패를 "invalid argument" 로만 말하고 길이 얘기를 안 한다.
     * 그래서 사유를 여기서 만든다. null 이면 문제 없음.
     */
    fun tooLong(socket: Path): String? {
        val n = socket.toString().toByteArray(Charsets.UTF_8).size
        if (n <= MAX_SOCKET_PATH) return null
        return "소켓 경로가 ${n}바이트이고 OS 가 받는 것은 약 ${MAX_SOCKET_PATH}바이트다 — " +
            "MAGI_CONFIG_DIR 을 더 짧은 곳으로: $socket"
    }

    /**
     * platform.go 의 `ConfigDir`(메서드다 — `func (OS) ConfigDir()`) 를 옮긴 것.
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
     * daemon.go 의 `sanitize`.
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
     * daemon.go 의 `shortHash` — FNV-1a 64비트를 base36 여덟 자리로.
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

/**
 * 데몬이 소켓 옆에 공표한 것을 읽는다.
 *
 * 어느 대화에 붙을지를 **넘겨짚지 않기 위해** 있다. "이 워크스페이스의 최신 세션"으로 고르면
 * 며칠 도는 데몬에서 그사이 누가 연 대화를 열게 된다 — daemon.go 가 레코드를 두는 사유가 그것이다.
 */
object Published {
    fun of(socket: java.nio.file.Path): dev.sayaya.magi.ide.model.Published? = runCatching {
        val text = java.nio.file.Files.readString(SocketPath.sessionFile(socket))
        dev.sayaya.magi.ide.model.Wire.json.decodeFromString(
            dev.sayaya.magi.ide.model.Published.serializer(), text
        )
    }.getOrNull()
}
