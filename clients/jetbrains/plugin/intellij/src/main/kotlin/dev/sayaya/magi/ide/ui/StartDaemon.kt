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

    /** 프로젝트당 한 번. 백오프 재접속이 따로 도므로 여기서 되풀이할 이유가 없다. */
    private val tried = java.util.Collections.synchronizedSet(mutableSetOf<String>())

    fun ifAbsent(project: Project) {
        if (!LocalPrefs.autostart(project)) return
        val base = project.basePath ?: return
        val sock = Workspace(project).socket() ?: return
        ApplicationManager.getApplication().executeOnPooledThread {
            if (project.isDisposed) return@executeOnPooledThread
            when (val r = DaemonClient.reach(sock)) {
                is Reach.Listening -> Unit // 이미 있다
                // **모름은 없음이 아니다.** 여기서 「해 봤다」를 안 찍는 것도 그래서다 — 일시적
                // 사정으로 못 물어본 것을 영구 포기로 바꾸지 않는다(리뷰 R6).
                is Reach.CouldNotAsk -> LOG.info("magi: 데몬을 물어볼 수 없어 안 띄운다 — ${r.why}")
                is Reach.Absent, is Reach.Refused -> {
                    // 「해 봤다」는 **실제로 띄우기로 정한 자리**에서만 찍는다. 앞에서 찍으면
                    // 못 물어봤든 사람이 미뤘든 네트워크가 끊겼든 그 IDE 내내 기능이 죽고,
                    // 되살릴 다른 경로가 없다(백오프 재접속은 붙기만 하지 띄우지 않는다).
                    if (!tried.add(base)) return@executeOnPooledThread
                    com.intellij.openapi.util.Disposer.register(project) { tried.remove(base) }
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
        // 로그는 소켓 옆에 둔다 — 「왜 안 떴나」를 물을 때 사람이 소켓부터 찾기 때문이다.
        val log = java.io.File(sock.toString() + ".ide.log")
        val started = runCatching {
            ProcessBuilder(bin.toString(), "-daemon")
                .directory(java.io.File(base))
                .apply {
                    // **사람의 셸이 아는 환경을 그대로 준다**([Shell]). 전에는 IDE 의 얇은 환경에
                    // `MAGI_CONFIG_DIR` 만 우리 계산으로 덮었는데, 그건 어긋남을 고치는 것이
                    // 아니라 어긋난 쪽으로 못박는 것이었다 — 셸에서 그 값을 쓰는 사람의 엔진이
                    // 빈 설정 디렉토리에서 뜨고, 키도 안 물려받았다(리뷰 R2). 소켓을 계산하는
                    // 쪽도 같은 근거를 본다(`Workspace.socket`).
                    environment().putAll(Shell.env())
                }
                .redirectInput(ProcessBuilder.Redirect.INHERIT)
                .redirectErrorStream(true)
                .redirectOutput(ProcessBuilder.Redirect.appendTo(log))
                .start()
        }.getOrElse { e ->
            LOG.warn("magi: 데몬을 못 띄웠다", e)
            tell(project, MagiBundle.msg("core.start.failed", e.message ?: MagiBundle.msg("common.noreason")))
            return
        }
        LOG.info("magi: 데몬을 띄웠다 — $bin -daemon (pid ${started.pid()}, 로그 $log)")
        // 성공은 침묵이다. 붙었는지는 상태 표시줄과 링크 점이 말하고, 그 둘이 이미 그 일을 한다.
        // 다만 **곧바로 죽으면** 그건 사람이 알아야 한다 — 조용한 실패는 「띄웠다」로 보인다.
        ApplicationManager.getApplication().executeOnPooledThread {
            if (!started.waitFor(3, java.util.concurrent.TimeUnit.SECONDS)) return@executeOnPooledThread
            val tail = runCatching { Files.readAllLines(log.toPath()).takeLast(3).joinToString(" / ") }
                .getOrDefault("")
            // **경합은 소식이 아니다.** 창을 둘 열거나 사람이 터미널에서 켜는 것과 겹치면 데몬이
            // 곧바로 1 로 끝나는데(`daemon.Listen` 의 선점), 그건 사람이 할 일이 없는 일이다.
            // 전에는 이 자리가 그 경우에 경고를 띄웠다 — 주석은 안 띄운다고 적어 두고서(리뷰 R3).
            if (RACE.containsMatchIn(tail)) {
                LOG.info("magi: 다른 magi 가 이미 이 워크스페이스를 쥐고 있다 — 그대로 둔다")
                return@executeOnPooledThread
            }
            tell(project, MagiBundle.msg("core.start.died", started.exitValue().toString(), tail))
        }
    }

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
