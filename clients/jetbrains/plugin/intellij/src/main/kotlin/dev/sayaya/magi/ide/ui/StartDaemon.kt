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
import dev.sayaya.magi.ide.usecase.Launches
import dev.sayaya.magi.ide.usecase.Move
import dev.sayaya.magi.ide.usecase.Phase
import dev.sayaya.magi.ide.usecase.Progress
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
     * 프로젝트별 재기동 예산 관리 모델([Launches]).
     * 데몬이 비정상 종료된 후 영구 미기동 상태로 방치되는 문제를 해결하기 위해 지수 백오프 기반 재시도 예산을 관리합니다.
     */
    private val budget = java.util.Collections.synchronizedMap(mutableMapOf<String, Launches>())

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
    /**
     * 갱신 재시작을 기다려 주는 시간 — **정책의 유예와 같은 값이다.**
     *
     * 예전엔 여기 5초가 따로 적혀 있었다. [Launches.graceMs] 도 5초라 우연히 맞았을 뿐이고,
     * 둘 중 하나만 고치면 이 잠이 끝난 직후 정책이 「아직 유예 중」이라며 거절하거나(안 뜬다),
     * 정책이 허락하는데 아직 죽어 가는 데몬과 소켓을 다투게 된다. 한 곳에서 온다.
     */
    private val RESTART_GRACE = Launches().graceMs

    /**
     * 워크스페이스마다, 준비 확인에서 데몬이 댄 **소유 계보**.
     *
     * 「소켓이 유예 안에 돌아왔나」는 교체를 **추론**하는 것이고, 이것은 그것을 **사실**로 만든다 —
     * 같은 계보를 알리는 후계는 이 창의 데몬이 스스로 갱신한 것이고, 다른 계보는 남의 것이다.
     * 계보를 안 싣는 코어에서는 비어 있고, 그때는 예전처럼 유예로만 판단한다.
     */
    private val lineage = java.util.concurrent.ConcurrentHashMap<String, String>()

    /**
     * 워크스페이스마다 이 창이 어느 상태에 있나 — `docs/CLIENT_LIFECYCLE` §3 을 [Progress] 로.
     *
     * ⚠ **여기 필요한 것은 `project.isDisposed` 가 아니다.** 이 맵들은 **창 사이에 공유된다**
     * (오브젝트 수준이고 워크스페이스 경로로 키를 잡는다). 그래서 「이 창이 아직 살아 있나」로는
     * 닫히고 다시 열린 창을 못 가른다 — 떠났던 비동기가 돌아와 **새 창의 상태에 옛 결과를
     * 쓴다.** §3 이 닫힘에 세대를 묶어 둔 이유가 그것이고, 떠나는 쪽이 번호를 들고 간다.
     */
    private val progress = java.util.concurrent.ConcurrentHashMap<String, Progress>()

    private fun progressOf(base: String): Progress = progress.computeIfAbsent(base) { Progress() }

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
                    // ⚠ **붙었다고 곧바로 용서하지 않는다.** 예전엔 여기서 예산을 통째로 되돌렸는데,
                    // 그러면 떴다가 2초 만에 죽기를 되풀이하는 데몬이 매 폴마다 용서받아 영원히
                    // 재시도된다 — 준비 시한을 넘긴 적이 없으니 실패로 한 번도 안 세인다.
                    // [Launches.connected] 가 「붙은 채로 얼마나 지났나」를 스스로 세고, 안정 구간을
                    // 넘겼을 때만 지운다(`clients/contract/lifecycle-policy.json`).
                    budget.getOrPut(base) { Launches() }.connected(System.currentTimeMillis())
                    progressOf(base).on(Move.Answered)
                    starting.remove(sock.toString())
                }
                // 데몬 상태 확인 실패 시 이중 기동 방지를 위해 기동을 보류합니다 (모름 != 없음).
                is Reach.CouldNotAsk -> LOG.info("magi: 데몬 상태 확인 불가로 기동 보류 — ${r.why}")
                is Reach.Absent, is Reach.Refused -> {
                    if (lifecycle.external) return@executeOnPooledThread
                    // 코어 자체 업데이트 유예 시간 대기:
                    // 유닉스 환경에서 `syscall.Exec`를 통한 자가 업데이트 시 소켓이 일시 재생성되므로 유예 시간 후 재확인합니다.
                    // ⚠ **손실은 여기서 적는다 — 잠들기 전에.** 유예는 「끊긴 뒤 5초는 새로 안
                    // 띄운다」이고, 잠든 뒤에 적으면 그 5초가 방금 시작한 것이 되어 [Launches.may]
                    // 가 언제나 유예로 거절한다(이 배선의 첫 판이 그랬다). 아래 `Thread.sleep` 이
                    // 바로 그 5초를 실제로 보내므로, 시각을 여기 박아 두면 둘이 같은 창을 말한다.
                    budget.getOrPut(base) { Launches() }.lost(System.currentTimeMillis())
                    // 끊긴 것을 상태에도 적는다. 예산만 아는 사실은 화면과 갈릴 수 있다.
                    progressOf(base).on(Move.Lost)
                    Thread.sleep(RESTART_GRACE)
                    if (DaemonClient.reach(sock) is Reach.Listening) {
                        LOG.info("magi: 유예 시간 내 데몬 재연결 확인 (업데이트 재시작 감지)")
                        // 사람이 시킨 갱신 교체는 **실패가 아니다** — 프로세스가 바뀐 것은 맞지만
                        // 아무것도 안 깨졌다. 그렇다고 예산을 지우지도 않는다: 아직 붙어 있기만 하다.
                        // 남의 데몬이 그 자리를 차지한 것이면 교체가 아니다 — 계보가 그것을
                        // 가른다. 계보를 모르는 코어에서는 판단하지 않고 예전대로 교체로 센다.
                        if (dev.sayaya.magi.ide.usecase.Generation.foreign(
                                dev.sayaya.magi.ide.transport.Published.of(sock), lineage[base]) == true) {
                            LOG.info("magi: 그 자리에 선 것은 다른 계보의 데몬이다 — 교체로 세지 않는다")
                            return@executeOnPooledThread
                        }
                        budget.getOrPut(base) { Launches() }.replaced(System.currentTimeMillis())
                        return@executeOnPooledThread
                    }
                    if (project.isDisposed) return@executeOnPooledThread
                    // 실제 기동 결정 시점에만 재기동 예산을 차감합니다.
                    val b = budget.getOrPut(base) { Launches() }
                    val now = System.currentTimeMillis()
                    val verdict = b.may(now)
                    if (verdict == Launches.Verdict.Blocked) progressOf(base).on(Move.Exhausted)
                    if (verdict != Launches.Verdict.Allow) return@executeOnPooledThread
                    // 유예를 지나 다시 보는 것이 §3 의 `Retry` 다. 없는 전이면 이 창은 그 상태에서
                    // 다시 볼 자리가 아니고(닫는 중이거나 이미 닫혔다), 그때는 띄우지 않는다 —
                    // 반환값을 버리면 상태 기계는 적어 두기만 하고 아무것도 통제하지 않는다.
                    val phase = progressOf(base)
                    if (phase.phase == Phase.Backoff && !phase.on(Move.Retry)) {
                        LOG.info("magi: 이 상태(${phase.phase})에서는 다시 보지 않는다")
                        return@executeOnPooledThread
                    }
                    if (phase.phase == Phase.Closing || phase.phase == Phase.Closed) {
                        LOG.info("magi: 창이 닫히는 중이라 기동하지 않는다")
                        return@executeOnPooledThread
                    }
                    b.spawned(now)
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
        val phase = progressOf(base)
        // 떠나면서 들고 가는 번호. 돌아왔을 때 이것이 낡았으면 이 일의 결과는 남의 창 것이다.
        val mine = phase.generation
        // ⚠ **반환값을 쓴다.** 닫는 중이거나 이미 닫힌 상태에서 `Absent` 는 없는 전이이고, 그때
        // 띄우면 아무도 끄지 않는 데몬이 남는다. 적어 두기만 하는 상태 기계는 문서를 한 벌 더 쓴
        // 것이라는 말이 여기에도 그대로 적용된다.
        if (!phase.on(Move.Absent)) {
            LOG.info("magi: 이 상태(${phase.phase})에서는 기동하지 않는다")
            return
        }
        com.intellij.openapi.util.Disposer.register(project) {
            // 닫힘은 두 걸음이다 — 들어가고(세대가 오른다), 정리가 끝나면 닫힌다.
            phase.on(Move.Close)
            phase.on(Move.Settled)
        }
        val log = java.io.File(sock.toString() + ".ide.log")
        starting[sock.toString()] = System.currentTimeMillis()
        var child: Process? = null
        try {
            SocketPath.tooLong(sock)?.let { throw java.io.IOException(it) }
            Files.createDirectories(sock.parent)
            // ⚠ **쓰기 전에 묻는다.** 소유 모드가 없는 코어는 그 플래그를 거절하고 2로 끝나며,
            // 창은 멀쩡한 바이너리를 두고 「기동 실패」를 알린다 — 설계 §4 가 적은 그대로 구형
            // 코어에는 옛 수명을 그대로 준다.
            val owned = CoreBinary.features(bin).contains("owned-daemon-v1")
            child = owner.launch {
                ProcessBuilder(
                    if (owned) listOf(bin.toString(), "--daemon", "--client-owned")
                    else listOf(bin.toString(), "--daemon"),
                )
                    .directory(java.io.File(base))
                    .apply { environment().putAll(Shell.env()) }
                    .redirectErrorStream(true)
                    .redirectOutput(ProcessBuilder.Redirect.appendTo(log))
                    .start()
            } ?: return
            if (owned) {
                // ⚠ **이 스트림을 닫으면 안 된다 — 그것이 소유자의 파이프다.**
                //
                // 옛 코드는 여기서 곧바로 `child.outputStream.close()` 했다. 소유 모드에서 그것은
                // 「소유자가 떠났다」는 신호라, 데몬이 뜨자마자 스스로 끝낸다. 창이 사는 동안
                // 열어 두는 것이 이 모드의 전부다(설계 검토 R3).
                //
                // 자바는 Node 와 달리 자식이 죽을 때 이 스트림을 대신 닫아 주지 않는다 — 그것이
                // 검토의 R2 가 VS Code 쪽에서 잡은 함정이고, 이쪽에는 그 함정이 없다.
                lifecycleOf(project).hold(child.outputStream)
            } else {
                child.outputStream.close()
            }
            val deadline = System.nanoTime() + java.util.concurrent.TimeUnit.SECONDS.toNanos(30)
            while (!project.isDisposed && child.isAlive && System.nanoTime() < deadline) {
                val published = dev.sayaya.magi.ide.transport.Published.of(sock)
                // ⚠ **「누군가 듣는다」는 「내가 띄운 자식이 답한다」가 아니다.** 기록의 pid 는
                // **기록이** 우리 자식을 가리킨다는 말이고, 소켓에 답하는 것이 그 기록의
                // 프로세스라는 말은 아무도 안 했다 — 둘은 갈릴 수 있다(앞 데몬의 기록이 남음 ·
                // 교체가 도는 중 · 같은 경로를 푼 남의 컴패니언). 답하는 쪽에 직접 묻는다.
                // ⚠ **남은 준비 시간으로 묶는다.** 기본 인내심은 120초라, 소켓이 연결만 받고
                // 답하지 않으면 이 한 번의 조회가 바깥의 30초 기한을 그대로 넘긴다 — 기한을 건
                // 쪽이 먼저 서야 기한이라는, 검토 R6 에서 배운 그 규칙이다.
                val left = java.util.concurrent.TimeUnit.NANOSECONDS
                    .toMillis(deadline - System.nanoTime()).coerceIn(250L, 5_000L)
                val hello = runCatching {
                    DaemonClient.connect(sock, left).use { it.exchange(
                        dev.sayaya.magi.ide.model.Request(method = "about")) }
                }.getOrNull()
                if (!phase.still(mine)) return
                if (dev.sayaya.magi.ide.usecase.Generation.same(published, hello, child.pid())) {
                    // 확인한 계보를 붙든다. 계보는 자기 갱신을 건너 물려받으므로, 핸들이 죽은
                    // 후계도 같은 계보를 알리면 이 창의 데몬이다.
                    hello?.owner?.takeIf { it.isNotBlank() }?.let { lineage[base] = it }
                    phase.on(Move.Answered)
                    LOG.info("magi: 데몬 정상 기동 완료 — $bin (로그: $log)")
                    // 떴다. **아직 아무것도 용서하지 않는다** — 그 판정은 붙은 채로 안정 구간을
                    // 넘겼을 때 [Launches.connected] 가 한다.
                    budget[base]?.ready(System.currentTimeMillis())
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
            // ⚠ **안 세면 영영 다시 시도한다.** 이동 구간(60초에 3회)은 시간이 지우고, 연속
            // 실패는 안 지운다 — 그런데 그 셈에 **아무도 1을 안 더하고 있었다.** 준비 시한을
            // 넘기거나 뜨자마자 죽는 데몬은 붙은 적이 없어 [Launches.lost] 로도 안 세이므로,
            // 1분이 지나면 예산이 돌아와 같은 실패를 무한히 되풀이한다(설계 검토 R4).
            phase.on(Move.LaunchFailed)
            budget[base]?.failed(System.currentTimeMillis())
            tell(project, MagiBundle.msg("core.start.died", reason, tail(log)))
        } catch (e: Exception) {
            child?.let { owner.stop(it) }
            if (e is InterruptedException) Thread.currentThread().interrupt()
            LOG.warn("magi: 데몬 기동 실패", e)
            budget[base]?.failed(System.currentTimeMillis())
            tell(project, MagiBundle.msg("core.start.died", e.message ?: "start failed", tail(log)))
        } finally {
            starting.remove(sock.toString())
        }
    }

    /** 이 프로젝트의 수명 서비스. 파이프를 맡길 자리가 거기다. */
    private fun lifecycleOf(project: Project): OwnedCompanion =
        project.getService(OwnedCompanion::class.java)

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
    /**
     * 소유자의 파이프. **창이 사는 동안 열려 있어야 한다.**
     *
     * 쥐고 있는 것이 곧 「이 창이 아직 있다」이고, 놓는 것이 곧 「갔다」이다 — IDE 가 강제로
     * 죽어 `dispose` 가 안 돌아도 OS 가 마지막 쓰기 끝을 닫아 주므로 데몬이 그것을 본다.
     * 손잡이를 끊는 것과 달리 이 신호는 **못 놓칠 수가 없다**.
     */
    @Volatile private var ownerPipe: java.io.OutputStream? = null

    fun hold(pipe: java.io.OutputStream) {
        ownerPipe?.let { runCatching { it.close() } } // 앞 세대의 것이 남아 있으면 먼저 놓는다
        ownerPipe = pipe
    }

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
        // 파이프를 **먼저** 놓는다. 소유 데몬은 그 EOF 로 스스로 풀리므로, 아래 `close()` 의
        // 소켓 종료·강제 종료는 그것이 안 통할 때를 위한 둘째 줄이 된다.
        ownerPipe?.let { runCatching { it.close() } }
        ownerPipe = null
        process.close()
    }
}
