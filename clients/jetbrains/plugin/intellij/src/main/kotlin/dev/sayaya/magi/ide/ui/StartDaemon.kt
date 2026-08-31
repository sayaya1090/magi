package dev.sayaya.magi.ide.ui

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.diagnostic.Logger
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.openapi.progress.Task
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.Messages
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.SocketPath
import dev.sayaya.magi.ide.usecase.Reach
import dev.sayaya.magi.ide.usecase.Restarts
import java.nio.file.Files
import java.nio.file.Path

/**
 * 프로젝트가 열릴 때 **이 워크스페이스의 데몬이 없으면 띄운다**(사용자 결정).
 *
 * 근거는 사용자의 문장 그대로다: IDE 가 프로젝트를 열었다는 것은 워크스페이스가 이미 정해졌다는
 * 뜻이고, 그 자리에 데몬이 없으면 켜 주는 것이 맞다. 그전에는 사람이 터미널에서 켜야 했고,
 * 그때까지 플러그인은 「실행되지 않았습니다」만 반복했다.
 *
 * **안 켜는 자리를 분명히 한다.**
 *  - 이미 듣고 있으면 안 켠다(당연).
 *  - 「물어볼 수 없었다」면 안 켠다 — 모르는 것을 없는 것으로 읽고 둘째 데몬을 띄우면, 한
 *    워크스페이스에 엔진 둘이 같은 스토어를 쓴다. 모름은 없음이 아니다.
 *  - 스위치가 꺼져 있으면 안 켠다.
 *
 * **경합은 데몬이 정리한다.** 창을 둘 열면 둘 다 여기 오는데, 데몬의 `Listen` 이 경로를
 * 선점하므로(`claimPath`) 하나만 서고 나머지는 「이미 듣고 있다」로 거절당한다. 그 거절은
 * 실패가 아니므로 화면에 안 싣는다 — 사람이 할 일이 없는 소식은 소식이 아니다.
 */
internal object StartDaemon {

    private val LOG = Logger.getInstance(StartDaemon::class.java)

    /**
     * 프로젝트마다 되살리기 예산. **한 번만 시도하던 것을 고친 자리다** — 그 한 번이 실패하거나
     * 떴다가 나중에 죽으면 되살릴 길이 없었다(백오프 재접속은 붙기만 하지 띄우지 않는다).
     * 라이브에서 데몬이 2분 반 만에 나갔고, 그 뒤 10분 동안 아무도 다시 띄우지 않았다.
     */
    private val budget = java.util.Collections.synchronizedMap(mutableMapOf<String, Restarts>())

    /** 방금 띄운 워크스페이스들 — 화면이 「띄우는 중」이라고 말할 수 있게. */
    private val starting = java.util.Collections.synchronizedMap(mutableMapOf<String, Long>())

    /**
     * 이 워크스페이스를 방금 띄웠고 아직 안 붙었나. 상태 표시줄이 이것을 물어 「실행되지 않음」
     * 대신 「시작하는 중」을 그린다 — **띄워 놓고 화면이 아무 말도 안 하면**, 사람은 아무 일도
     * 안 일어났다고 읽는다(라이브 실측: 7초 동안 「실행되지 않음」이었다).
     */
    fun startingNow(sock: java.nio.file.Path): Boolean {
        val at = starting[sock.toString()] ?: return false
        return System.currentTimeMillis() - at < STARTING_WINDOW
    }

    /** 뜨는 데 걸리는 시간의 상한. 실측 7초(진짜 설정, 첫 기동) — 넉넉히 잡는다. */
    private const val STARTING_WINDOW = 30_000L

    /** 갱신 재시작이 같은 경로에 다시 서는 데 주는 말미. 그 창은 짧다(exec 한 번). */
    private const val RESTART_GRACE = 5_000L

    /**
     * 지금 띄워도 되나. **시험 안에서는 안 된다.**
     *
     * 헤드리스 시험은 프로젝트를 만들고 열므로 이 활동이 그대로 돈다 — 그래서 `:intellij:test`
     * 를 돌릴 때마다 임시 워크스페이스마다 **진짜 데몬이 하나씩 떴다**(실측: 설정 디렉토리에
     * `daemon-unitTest_…` 로그가 열 개 넘게 쌓였다). CI 에서도 돈다. 시험이 남의 기계에
     * 프로세스를 남기는 것은 시험이 아니다.
     *
     * 판정을 함수로 뺀 이유: 가드를 코드에 묻어 두면 그 가드가 도는지를 잴 자리가 없다.
     */
    fun enabled(project: Project): Boolean =
        !ApplicationManager.getApplication().isUnitTestMode && LocalPrefs.autostart(project)

    fun ifAbsent(project: Project) {
        if (!enabled(project)) return
        val base = project.basePath ?: return
        val sock = Workspace(project).socket() ?: return
        ApplicationManager.getApplication().executeOnPooledThread {
            if (project.isDisposed) return@executeOnPooledThread
            when (val r = DaemonClient.reach(sock)) {
                is Reach.Listening -> {
                    // 붙었으면 예산을 되돌린다 — 오래 도는 IDE 에서 「예전에 실패함」이 영구
                    // 금지가 되면 안 된다.
                    budget[base]?.ok()
                    starting.remove(sock.toString())
                }
                // **모름은 없음이 아니다.** 여기서 「해 봤다」를 안 찍는 것도 그래서다 — 일시적
                // 사정으로 못 물어본 것을 영구 포기로 바꾸지 않는다(리뷰 R6).
                is Reach.CouldNotAsk -> LOG.info("magi: 데몬을 물어볼 수 없어 안 띄운다 — ${r.why}")
                is Reach.Absent, is Reach.Refused -> {
                    // **잠깐 기다렸다 다시 본다.** 코어의 자동 업데이트는 유휴에 스스로
                    // 재시작하는데, 유닉스에서는 `syscall.Exec` 라 소켓이 잠깐 사라졌다 **같은
                    // 경로에** 다시 선다(프로세스는 한 번도 안 죽는다). 그 창을 죽음으로 읽으면
                    // 갱신할 때마다 헛기동을 한 번씩 한다. 여기 온 것은 어차피 데몬이 없을
                    // 때뿐이라 몇 초 기다리는 값이 싸다.
                    Thread.sleep(RESTART_GRACE)
                    if (DaemonClient.reach(sock) is Reach.Listening) {
                        LOG.info("magi: 잠깐 사이에 다시 섰다 — 갱신 재시작으로 보인다")
                        budget[base]?.ok()
                        return@executeOnPooledThread
                    }
                    // 「해 봤다」는 **실제로 띄우기로 정한 자리**에서만 찍는다. 앞에서 찍으면
                    // 못 물어봤든 사람이 미뤘든 네트워크가 끊겼든 그 IDE 내내 기능이 죽고,
                    // 되살릴 다른 경로가 없다(백오프 재접속은 붙기만 하지 띄우지 않는다).
                    val b = budget.getOrPut(base) { Restarts() }
                    if (!b.take(System.currentTimeMillis())) return@executeOnPooledThread
                    com.intellij.openapi.util.Disposer.register(project) { budget.remove(base); starting.remove(sock.toString()) }
                    ensureBinaryThenStart(project, base, sock)
                }
            }
        }
    }

    private fun ensureBinaryThenStart(project: Project, base: String, sock: Path) {
        CoreBinary.found()?.let { return start(project, it, base, sock) }
        // 한 번 미룬 사람에게 프로젝트마다·재시작마다 모달을 들이밀지 않는다(리뷰 R11).
        // 이 기억은 앱 수준이다 — 거절은 이 프로젝트가 아니라 그 사람의 뜻이다.
        if (com.intellij.ide.util.PropertiesComponent.getInstance().getBoolean(DECLINED, false)) return
        // **받을 판을 여기서 정한다.** 핀은 바닥일 뿐이다 — 먼저 옛 판을 받아 놓고 데몬이
        // 스스로 갱신하기를 기다리는 것은, 처음 쓰는 사람에게 두 번 기다리라는 말이다.
        // 이 자리는 이미 풀 스레드라 목록 한 번 물어보는 것이 화면을 안 막는다.
        val pick = CoreBinary.resolve()
        val rel = pick.release
        val asset = rel.asset(
            System.getProperty("os.name").orEmpty(), System.getProperty("os.arch").orEmpty(),
        )
        val host = asset?.let { rel.host(it) } ?: return run {
            LOG.info("magi: 이 기계에 맞는 코어 자산이 없거나 받을 주소가 없다 — 안 묻는다")
        }
        // **묻는다.** 네트워크에서 실행 파일을 받아 돌리는 일을 조용히 하지 않는다. 그리고
        // **어디서** 받는지를 보인다 — 미러로 갈아 끼울 수 있다는 것이 이 설계의 자랑인데,
        // 갈아 끼운 사실이 사람에게 안 보이면 자랑이 아니다(리뷰 R7).
        ApplicationManager.getApplication().invokeLater({
            val yes = Messages.showYesNoDialog(
                project,
                MagiBundle.msg("core.get.body", rel.version, host) +
                    if (rel.insecure) "\n\n" + MagiBundle.msg("core.get.insecure") else "",
                MagiBundle.msg("core.get.title"),
                MagiBundle.msg("core.get.yes"), MagiBundle.msg("core.get.no"), null,
            ) == Messages.YES
            if (!yes) {
                com.intellij.ide.util.PropertiesComponent.getInstance().setValue(DECLINED, true)
                return@invokeLater
            }
            object : Task.Backgroundable(project, MagiBundle.msg("core.get.title"), true) {
                override fun run(indicator: ProgressIndicator) {
                    val bin = runCatching { CoreBinary.download(indicator, pick) }.getOrElse { e ->
                        // 취소는 실패가 아니다. 플랫폼 계약상 이 예외는 삼키면 안 되고, 삼키면
                        // 사람이 누른 「취소」가 에러 풍선으로 돌아온다(리뷰 R4).
                        if (e is com.intellij.openapi.progress.ProcessCanceledException) throw e
                        LOG.warn("magi: 코어를 못 받았다", e)
                        tell(project, MagiBundle.msg("core.get.failed", e.message ?: MagiBundle.msg("common.noreason")))
                        return
                    }
                    if (!project.isDisposed) start(project, bin, base, sock)
                }
            }.queue()
        }, project.disposed)
    }

    private fun start(project: Project, bin: Path, base: String, sock: Path) {
        // 자식이 남기는 말이 서는 자리. 데몬 **자신의** 출력은 코어가 `<소켓>.log` 로 보내므로
        // 여기 담기는 것은 띄우는 명령의 말이다(성공 한 줄, 또는 왜 못 섰는지).
        val log = java.io.File(sock.toString() + ".ide.log")
        starting[sock.toString()] = System.currentTimeMillis()
        val detached = run(bin, base, log, detach = true)
        val outcome = when {
            detached == null -> null // 못 띄웠다 — run 이 이미 말했다
            detached == 0 -> 0
            // **옛 코어에는 그 플래그가 없다.** 플러그인이 받아오는 것은 릴리스이고, `--detach`
            // 보다 앞선 판이 얼마든지 깔려 있을 수 있다. 그때 Go 의 flag 는 2 로 끝나며
            // "not defined" 를 적는다 — 그 한 경우만 옛 방식으로 되돌린다(그때 데몬은 IDE 와
            // 함께 죽는다. 매뉴얼이 그 갈림을 적는다).
            tail(log).contains("not defined") -> {
                LOG.info("magi: 이 코어에는 --detach 가 없다 — 옛 방식으로 띄운다(IDE 와 함께 죽는다)")
                run(bin, base, log, detach = false)
            }
            else -> detached
        }
        if (outcome == 0) {
            LOG.info("magi: 데몬이 섰다 — $bin (로그 $log)")
            return
        }
        starting.remove(sock.toString())
        val tail = tail(log)
        // **경합은 소식이 아니다.** 창을 둘 열거나 사람이 터미널에서 켜는 것과 겹치면 기동이
        // 거절되는데, 그건 사람이 할 일이 없는 일이다.
        if (RACE.containsMatchIn(tail)) {
            LOG.info("magi: 다른 magi 가 이미 이 워크스페이스를 쥐고 있다 — 그대로 둔다")
            return
        }
        if (outcome != null) tell(project, MagiBundle.msg("core.start.died", outcome.toString(), tail))
    }

    /**
     * 한 번 띄운다. **끝날 때까지 기다린다** — `--detach` 는 소켓이 답할 때 돌아오므로
     * exit 0 은 「띄웠다」가 아니라 **「서 있다」**다. 그래서 3초 짐작이 필요 없어졌고,
     * 먼저 죽으면 그 사유가 자식의 마지막 말로 온다.
     *
     * 못 띄운 것(바이너리가 없다 등)과 띄웠는데 실패한 것은 다른 사건이라 갈라 돌려준다:
     * null 은 앞엣것이고, 그때는 여기서 말한다.
     */
    private fun run(bin: Path, base: String, log: java.io.File, detach: Boolean): Int? {
        val argv = mutableListOf(bin.toString(), "--daemon")
        if (detach) argv += "--detach"
        val p = runCatching {
            ProcessBuilder(argv)
                .directory(java.io.File(base))
                .apply { environment().putAll(Shell.env()) }
                .redirectErrorStream(true)
                .redirectOutput(ProcessBuilder.Redirect.appendTo(log))
                .start()
        }.getOrElse { e ->
            LOG.warn("magi: 데몬을 못 띄웠다", e)
            return null
        }
        // 넉넉히 기다린다: 진짜 설정의 첫 기동이 실측 7초였다. 안 끝나면 죽이지 않는다 —
        // 서는 중일 수 있고, 서면 상태 표시줄이 알아챈다.
        return if (p.waitFor(90, java.util.concurrent.TimeUnit.SECONDS)) p.exitValue() else null
    }

    private fun tail(log: java.io.File): String = runCatching {
        Files.readAllLines(log.toPath()).takeLast(3).joinToString(" / ")
    }.getOrDefault("")

    /** 데몬이 「이미 누가 쥐고 있다」로 끝난 것. 문구는 `daemon.Listen` 과 `claim_unix.go` 의 것. */
    private val RACE = Regex("already (listening|starting or running)")

    private const val DECLINED = "magi.core.download.declined"

    private fun tell(project: Project, text: String) {
        // 프로젝트를 닫는 것은 정상 동작이다. 죽은 프로젝트에 알림을 밀면 서비스를 꺼내다
        // 터진다 — 이 자리가 이 저장소에서 유일하게 **비동기로** 알리는 호출자다(리뷰 R5).
        if (project.isDisposed) return
        NotificationGroupManager.getInstance()
            .getNotificationGroup("magi")
            .createNotification(text, NotificationType.WARNING)
            .notify(project)
    }
}
