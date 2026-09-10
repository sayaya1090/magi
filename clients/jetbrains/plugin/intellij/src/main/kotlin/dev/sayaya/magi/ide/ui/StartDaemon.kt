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
import dev.sayaya.magi.ide.usecase.DaemonProcess
import com.intellij.openapi.components.Service
import com.intellij.openapi.Disposable
import java.nio.file.Files
import java.nio.file.Path

/**
 * 프로젝트 오픈 시 워크스페이스 데몬이 미실행 상태이면 자동으로 기동합니다.
 *
 * 자동 기동 정책:
 * IDE에서 프로젝트가 열렸다는 것은 워크스페이스가 확정되었음을 의미하므로, 백그라운드 엔진이 없으면 자동으로 기동합니다.
 *
 * 기동 제외 조건:
 * - 이미 데몬이 소켓을 바인딩하고 있는 경우
 * - 데몬 상태를 확인할 수 없는 경우(`Reach.CouldNotAsk`) — 미확인 상태를 미기동으로 간주하여 이중 기동하는 충돌을 방지합니다.
 * - 프로젝트 설정에서 자동 기동(`LocalPrefs.autostart`)이 비활성화된 경우
 *
 * 프로세스 경합 제어:
 * 여러 IDE 창이 동시에 열리더라도 데몬의 소켓 바인딩 단계(`claimPath`, flock 기반)에서 경로가 선점되므로 단일 데몬만 기동에 성공합니다.
 */
internal object StartDaemon {

    private val LOG = Logger.getInstance(StartDaemon::class.java)

    /**
     * 프로젝트별 재기동 예산 관리 모델([Restarts]).
     * 데몬이 비정상 종료된 후 영구 미기동 상태로 방치되는 문제를 해결하기 위해 지수 백오프 기반 재시도 예산을 관리합니다.
     */
    private val budget = java.util.Collections.synchronizedMap(mutableMapOf<String, Restarts>())

    /** 기동 진행 중인 워크스페이스 타임스탬프 맵 (상태 표시줄의 '시작하는 중' 상태 표현용). */
    private val starting = java.util.Collections.synchronizedMap(mutableMapOf<String, Long>())

    /**
     * 현재 워크스페이스 데몬이 기동 진행 중인지 여부를 반환합니다.
     * 상태 표시줄에서 '실행되지 않음' 대신 '시작하는 중' 상태를 표시하기 위해 사용됩니다(첫 기동 실측 7초 소요 반영).
     */
    fun startingNow(sock: java.nio.file.Path): Boolean {
        val at = starting[sock.toString()] ?: return false
        return System.currentTimeMillis() - at < STARTING_WINDOW
    }

    /** 데몬 기동 타임아웃 상한 (30초, 콜드 스타트 실측 7초 기준 안전 여유폭 확보). */
    private const val STARTING_WINDOW = 30_000L

    /** 코어 자동 업데이트 재시작 대기 유예 시간 (5초, `syscall.Exec` 소켓 재생성 대기). */
    private const val RESTART_GRACE = 5_000L

    /**
     * 데몬 자동 기동 허용 여부를 판정합니다.
     * 단위 테스트 모드(`isUnitTestMode`)에서는 테스트 환경 오염 및 불필요한 프로세스 생성을 방지하기 위해 기동을 차단합니다.
     */
    fun enabled(project: Project): Boolean =
        !ApplicationManager.getApplication().isUnitTestMode && LocalPrefs.autostart(project)

    /**
     * 사용자가 명시적 액션(메뉴 등)으로 데몬을 기동할 때 호출됩니다.
     *
     * 수동 기동 특성:
     * 자동 기동 방지용 설정([enabled])과 재시도 예산 제약을 적용하지 않고 즉시 기동을 시도합니다.
     * 단, 단위 테스트 모드 가드는 그대로 유지됩니다.
     * 이미 실행 중인 경우 안내 메시지를 표시하고 종료합니다.
     */
    fun byHand(project: Project) {
        if (ApplicationManager.getApplication().isUnitTestMode) return
        val base = project.basePath ?: return
        val sock = Workspace(project).socket() ?: return
        val lifecycle = project.getService(OwnedCompanion::class.java)
        lifecycle.socket = sock
        val owner = lifecycle.process
        ApplicationManager.getApplication().executeOnPooledThread {
            if (project.isDisposed) return@executeOnPooledThread
            // 이미 소켓이 활성화되어 있으면 중복 기동하지 않고 안내합니다.
            if (DaemonClient.reach(sock) is Reach.Listening) {
                tell(project, MagiBundle.msg("start.already"))
                return@executeOnPooledThread
            }
            com.intellij.openapi.util.Disposer.register(project) { starting.remove(sock.toString()) }
            ensureBinaryThenStart(project, base, sock, owner, manual = true)
        }
    }

    fun ifAbsent(project: Project) {
        if (project.isDisposed || !enabled(project)) return
        val base = project.basePath ?: return
        val sock = Workspace(project).socket() ?: return
        val lifecycle = project.getService(OwnedCompanion::class.java)
        lifecycle.socket = sock
        val owner = lifecycle.process
        ApplicationManager.getApplication().executeOnPooledThread {
            if (project.isDisposed) return@executeOnPooledThread
            when (val r = DaemonClient.reach(sock)) {
                is Reach.Listening -> {
                    lifecycle.external = !owner.running
                    // 정상 연결 확인 시 재기동 예산 복구
                    budget[base]?.ok()
                    starting.remove(sock.toString())
                }
                // 데몬 상태 확인 실패 시 이중 기동 방지를 위해 기동을 보류합니다 (모름 != 없음).
                is Reach.CouldNotAsk -> LOG.info("magi: 데몬 상태 확인 불가로 기동 보류 — ${r.why}")
                is Reach.Absent, is Reach.Refused -> {
                    if (lifecycle.external) return@executeOnPooledThread
                    // 코어 자체 업데이트 유예 시간 대기:
                    // 유닉스 환경에서 `syscall.Exec`를 통한 자가 업데이트 시 소켓이 일시 재생성되므로 유예 시간 후 재확인합니다.
                    Thread.sleep(RESTART_GRACE)
                    if (DaemonClient.reach(sock) is Reach.Listening) {
                        LOG.info("magi: 유예 시간 내 데몬 재연결 확인 (업데이트 재시작 감지)")
                        budget[base]?.ok()
                        return@executeOnPooledThread
                    }
                    if (project.isDisposed) return@executeOnPooledThread
                    // 실제 기동 결정 시점에만 재기동 예산을 차감합니다.
                    val b = budget.getOrPut(base) { Restarts() }
                    if (!b.take(System.currentTimeMillis())) return@executeOnPooledThread
                    com.intellij.openapi.util.Disposer.register(project) { budget.remove(base); starting.remove(sock.toString()) }
                    ensureBinaryThenStart(project, base, sock, owner)
                }
            }
        }
    }

    /**
     * 코어를 받아오는 일은 **앱에 하나**다. 창마다가 아니다.
     *
     * 바이너리는 계정에 하나이고 자리도 하나인데(`<config>/bin/<판>/magi`) 받으러 가는 것은
     * 프로젝트마다였다. 창을 셋 켜 놓고 플러그인을 깔면 셋이 나란히 여기 도착해 셋 다 「없다」를
     * 보고, 셋 다 묻고, 셋 다 같은 파일을 같은 경로로 받았다 — 실사용 보고: 받는다는 대화가
     * 여러 번 떴다.
     *
     * 이 클래스의 머리글이 경합을 이미 이야기하는데, 그것은 **데몬 기동**의 경합이다. 소켓
     * 경로는 `Listen` 이 선점하므로 하나만 서고 나머지는 거절당한다. 받아오기는 그 한 걸음
     * 앞이고 거기엔 선점할 경로가 없다. 거절을 기억하는 자리(DECLINED)는 이미 앱 수준인데
     * 묻는 쪽만 아니었던 것이다.
     */
    private val fetching = dev.sayaya.magi.ide.usecase.OnceAcross<Path?>()

    private fun ensureBinaryThenStart(project: Project, base: String, sock: Path, owner: DaemonProcess, manual: Boolean = false) {
        CoreBinary.found()?.let { return start(project, it, base, sock, owner) }
        // 한 번 미룬 사람에게 프로젝트마다·재시작마다 모달을 들이밀지 않는다(리뷰 R11).
        // 이 기억은 앱 수준이다 — 거절은 이 프로젝트가 아니라 그 사람의 뜻이다.
        if (!manual && com.intellij.ide.util.PropertiesComponent.getInstance().getBoolean(DECLINED, false)) return
        // 기다린 창도 **제 데몬을 띄운다.** 소켓은 워크스페이스마다 다르니 그쪽은 나뉘는 것이
        // 맞고, 물러나 버리면 그 창은 이 IDE 가 사는 동안 데몬을 못 띄운다(백오프 재접속은
        // 붙기만 하지 띄우지 않는다).
        fetching.join { flight -> askAndFetch(project, flight) }
            .whenComplete { bin, error ->
                if (error != null) {
                    LOG.warn("magi: 코어 준비 실패", error)
                    tell(project, MagiBundle.msg("core.get.failed", error.message ?: "unknown"))
                }
                if (bin != null && !project.isDisposed) start(project, bin, base, sock, owner)
            }
    }

    private fun askAndFetch(project: Project, flight: java.util.concurrent.CompletableFuture<Path?>) {
        // 프로젝트 종료 시 다운로드 Future를 취소 완료 처리합니다.
        com.intellij.openapi.util.Disposer.register(project) { flight.complete(null) }
        // 다운로드 대상 바이너리 버전 결정:
        // 풀 스레드에서 최신 호환 버전을 조회하여 초기 사용자의 중복 업데이트 대기 시간을 방지합니다.
        val pick = CoreBinary.resolve()
        val rel = pick.release
        val asset = rel.asset(
            System.getProperty("os.name").orEmpty(), System.getProperty("os.arch").orEmpty(),
        )
        val host = asset?.let { rel.host(it) } ?: return run {
            LOG.info("magi: 현재 아키텍처에 맞는 코어 바이너리 또는 다운로드 URL 부재 — 다운로드 요청 생략")
            flight.complete(null)
        }
        // 바이너리 다운로드 전 사용자 명시적 동의 확인:
        // 신뢰성 확보를 위해 다운로드 대상 호스트 미러 주소를 다이얼로그에 명시합니다(리뷰 R7).
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
                flight.complete(null)
                return@invokeLater
            }
            com.intellij.ide.util.PropertiesComponent.getInstance().setValue(DECLINED, false)
            object : Task.Backgroundable(project, MagiBundle.msg("core.get.title"), true) {
                override fun run(indicator: ProgressIndicator) {
                    val bin = runCatching { CoreBinary.download(indicator, pick) }.getOrElse { e ->
                        // 사용자의 취소 동작은 정상 중단으로 처리하며 플랫폼 계약에 따라 ProcessCanceledException을 재전파합니다(리뷰 R4).
                        flight.complete(null)
                        if (e is com.intellij.openapi.progress.ProcessCanceledException) throw e
                        LOG.warn("magi: 코어 바이너리 다운로드 실패", e)
                        tell(project, MagiBundle.msg("core.get.failed", e.message ?: MagiBundle.msg("common.noreason")))
                        flight.complete(null)
                        return
                    }
                    flight.complete(bin)
                }
            }.queue()
        }, project.disposed)
    }

    private fun start(project: Project, bin: Path, base: String, sock: Path, owner: DaemonProcess) {
        if (project.isDisposed) return
        val log = java.io.File(sock.toString() + ".ide.log")
        starting[sock.toString()] = System.currentTimeMillis()
        var child: Process? = null
        try {
            SocketPath.tooLong(sock)?.let { throw java.io.IOException(it) }
            Files.createDirectories(sock.parent)
            child = owner.launch {
                ProcessBuilder(bin.toString(), "--daemon")
                    .directory(java.io.File(base))
                    .apply { environment().putAll(Shell.env()) }
                    .redirectErrorStream(true)
                    .redirectOutput(ProcessBuilder.Redirect.appendTo(log))
                    .start()
            } ?: return
            child.outputStream.close()
            val deadline = System.nanoTime() + java.util.concurrent.TimeUnit.SECONDS.toNanos(30)
            while (!project.isDisposed && child.isAlive && System.nanoTime() < deadline) {
                val published = dev.sayaya.magi.ide.transport.Published.of(sock)
                if (published?.pid?.toLong() == child.pid() && DaemonClient.reach(sock) is Reach.Listening) {
                    LOG.info("magi: 데몬 정상 기동 완료 — $bin (로그: $log)")
                    return
                }
                Thread.sleep(100)
            }
            if (project.isDisposed) {
                owner.stop(child)
                return
            }
            if (!child.isAlive && DaemonClient.reach(sock) is Reach.Listening) return
            val reason = if (child.isAlive) "startup timed out after 30s" else "exit ${child.exitValue()}"
            owner.stop(child)
            tell(project, MagiBundle.msg("core.start.died", reason, tail(log)))
        } catch (e: Exception) {
            child?.let { owner.stop(it) }
            if (e is InterruptedException) Thread.currentThread().interrupt()
            LOG.warn("magi: 데몬 기동 실패", e)
            tell(project, MagiBundle.msg("core.start.died", e.message ?: "start failed", tail(log)))
        } finally {
            starting.remove(sock.toString())
        }
    }

    private fun tail(log: java.io.File): String = runCatching {
        java.io.RandomAccessFile(log, "r").use { f ->
            val size = minOf(f.length(), 8192L).toInt()
            f.seek(f.length() - size)
            val bytes = ByteArray(size)
            f.readFully(bytes)
            String(bytes, Charsets.UTF_8).lineSequence().filter { it.isNotBlank() }.toList().takeLast(3).joinToString(" / ")
        }
    }.getOrDefault("")

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

/** Project disposal covers closing the project, IDE exit and plugin unload. */
@Service(Service.Level.PROJECT)
internal class OwnedCompanion : Disposable {
    @Volatile var socket: Path? = null
    val process = DaemonProcess { child ->
        val address = socket
        // A bounded worker lets project disposal proceed while the daemon drains its log.
        Thread({
            try {
                if (address != null && child.isAlive &&
                    dev.sayaya.magi.ide.transport.Published.of(address)?.pid?.toLong() == child.pid()) {
                    DaemonClient.connect(address, 1_000).use {
                        it.exchange(dev.sayaya.magi.ide.model.Request(method = "shutdown"))
                    }
                    child.waitFor(2, java.util.concurrent.TimeUnit.SECONDS)
                }
            } catch (e: Exception) {
                if (e is InterruptedException) Thread.currentThread().interrupt()
            } finally {
                if (child.isAlive) {
                    child.destroy()
                    if (!child.waitFor(2, java.util.concurrent.TimeUnit.SECONDS)) child.destroyForcibly()
                }
            }
        }, "magi-companion-stop").start()
    }
    @Volatile var external = false
    private var retry: java.util.concurrent.ScheduledFuture<*>? = null

    fun watch(project: Project) {
        if (retry != null || com.intellij.openapi.application.ApplicationManager.getApplication().isUnitTestMode) return
        retry = com.intellij.util.concurrency.AppExecutorUtil.getAppScheduledExecutorService()
            .scheduleWithFixedDelay({
                if (!project.isDisposed && !external) StartDaemon.ifAbsent(project)
            }, 15, 15, java.util.concurrent.TimeUnit.SECONDS)
    }

    override fun dispose() {
        retry?.cancel(false)
        process.close()
    }
}
