package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.options.Configurable
import com.intellij.openapi.project.Project
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBTextField
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.usecase.Activity
import dev.sayaya.magi.ide.model.ConfigItem
import dev.sayaya.magi.ide.usecase.Companion
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets
import javax.swing.JComboBox
import javax.swing.JComponent
import javax.swing.SwingUtilities

/**
 * Settings › Tools › magi — 영속적인 환경 설정을 관리하는 패널입니다(`docs/UI.ko.md` §5).
 *
 * 상태 관리 원칙 (Single Source of Truth):
 * 설정값의 진실 원천은 magi 데몬입니다. IntelliJ 플랫폼의 `PersistentStateComponent`에 중복 저장할 경우
 * IDE 캐시와 데몬 설정 간의 불일치가 발생할 수 있으므로, 창이 열릴 때 데몬에서 조회하고([reset]),
 * 적용 시 데몬에 기록하며([apply]), 기록 직후 재조회하여 검증합니다(`docs/UI.ko.md` §5.1).
 *
 * 읽기 전용 항목 처리:
 * 데몬에 쓰기 API가 제공되지 않는 항목(백엔드 프로바이더 `/providers`, 서브에이전트, 컨텍스트 윈도우 등)은 편집 불가 텍스트로 렌더링합니다.
 * 자동완성 활성화 여부 역시 magi 전역의 `[autocomplete]` 설정을 따르며 플러그인에서 중복 플래그를 생성하지 않습니다.
 *
 * 상태 가시성:
 * 현재 모델 및 백엔드 프로필은 데몬 `status` 응답 필드를 파싱하여 표시하며, 응답이 누락된 경우에만 미수신으로 안내합니다.
 */
class MagiConfigurable(private val project: Project) : Configurable {

    private val workspace by lazy { Workspace(project) }

    // ── 상태 섹션 — 과거 우측 도구 창에 상시 노출되던 세부 정보를 설정 패널 내부로 통합(2026-08-29 결정).
    // 상시 감시는 상태 표시줄이 수행하며, 설정 창 진입 시 reset()을 통해 최신 상태를 자동 동기화합니다.
    private val doing = Look.wide()
    private val perm = Look.wide()
    private val sessionL = Look.wide().apply { font = Look.mono() }
    /**
     * 현재 활성화된 모델 및 요청 대상 백엔드 정보.
     * 데몬 `status` 응답에서 추출하여 변경 대상의 기준점을 사용자에게 명확히 제시합니다.
     */
    private val completeWhy = Look.note("", Look.faint) as javax.swing.JTextArea
    private val modelNow = Look.wide()
    private val backendNow = Look.wide()
    private val outside = Look.flow(Look.warn)
    /**
     * 권한 모드 선택 콤보박스.
     * 데이터 모델은 프로토콜 상수 토큰을 저장하고 표시는 플랫폼 렌더러를 통해 현지화 라벨로 변환합니다.
     */
    private val permission = Look.narrowCombo<String>(12, prototype = false).apply {
        Perms.TOKENS.forEach(::addItem)
        // 플랫폼 렌더러 적용: 표준 Swing 렌더러 대신 플랫폼 렌더러를 사용하여 IDE 테마의 선택 색상과 패딩을 준수합니다.
        renderer = com.intellij.ui.SimpleListCellRenderer.create<String> { label, value, _ ->
            label.text = Perms.label(value)
        }
    }
    /**
     * IDE 로컬 설정 컴포넌트 — 데몬의 전역 구성이 아닌 IDE 플러그인 레벨의 동작 방식을 제어합니다.
     */
    private val lookTyping = Look.check(MagiBundle.msg("set.look.box"))
    private val autoComplete = Look.check(MagiBundle.msg("set.complete.box"))
    private val composerSuggest = Look.check(MagiBundle.msg("set.suggest.box"))
    private val autostart = Look.check(MagiBundle.msg("set.autostart.box"))
    private val model = Look.narrowCombo<String>(24).apply { isEditable = true }
    private val backend = Look.narrowField()
    private val said = Look.flow()

    /** 마지막으로 데몬에서 읽어온 상태 직렬화 값. [isModified] 변경 감지 시 기준 스냅샷으로 사용됩니다. */
    private var read: String? = null

    /**
     * 데몬이 `config-get` 엔드포인트로 열거한 설정 키와 동적 입력 컴포넌트 맵.
     * 설정 키를 클라이언트에 하드코딩하지 않고 데몬 동적 메타데이터를 기반으로 UI를 생성하여 신규 설정 항목을 자동 지원합니다.
     */
    private var byDoor: List<ConfigItem> = emptyList()
    private var choices: List<String> = emptyList()
    private val doorFields = LinkedHashMap<String, javax.swing.text.JTextComponent>()
    private val doorPane = javax.swing.JPanel(GridBagLayout())

    // 표시 이름은 번들 리소스에서 단일 참조합니다.
    override fun getDisplayName() = MagiBundle.msg("configurable.magi")

    override fun createComponent(): JComponent {
        val p = JBPanel<JBPanel<*>>(GridBagLayout()).apply { border = JBUI.Borders.empty(8, 12) }
        var y = 0
        // 최상단 전폭 알림 배너 배치:
        // 데몬 비연결 시 아래의 설정값들이 유효하지 않음을 사용자가 즉시 인지할 수 있도록 최상단에 전폭(width=2)으로 배치합니다(2026-09-09 사용자 피드백 반영).
        p.add(said, GridBagConstraints().apply {
            gridx = 0; gridy = y++; gridwidth = 2
            anchor = GridBagConstraints.LINE_START
            fill = GridBagConstraints.HORIZONTAL
            weightx = 1.0
            insets = Insets(0, 0, 8, 0)
        })
        fun head(text: String) {
            p.add(Look.gutter(text), GridBagConstraints().apply {
                gridx = 0; gridy = y; gridwidth = 2; anchor = GridBagConstraints.LINE_START
                insets = Insets(if (y == 0) 0 else 14, 0, 2, 0)
            })
            y++
        }
        fun row(name: String, c: JComponent) {
            // 이름 칸도 폭을 요구하지 않는다 — 칸을 다 줄여 놔도 이름이 안 줄면 판이 안 좁혀진다.
            p.add(Look.wide().apply { text = name; foreground = Look.faint }, GridBagConstraints().apply {
                gridx = 0; gridy = y; anchor = GridBagConstraints.LINE_START; insets = Insets(4, 0, 4, 12)
            })
            p.add(c, GridBagConstraints().apply {
                gridx = 1; gridy = y; weightx = 1.0; fill = GridBagConstraints.HORIZONTAL
                anchor = GridBagConstraints.LINE_START; insets = Insets(4, 0, 4, 0)
            })
            y++
        }
        // 설명문은 **접히는 라벨**이다. 한 줄로 펴는 라벨은 제 글자 길이만큼 폭을 요구하고,
        // 그 요구가 설정 판 전체를 벌린다 — 드롭다운에서 이미 겪은 그 기전이다(가이드라인 G9).
        fun note(text: String) {
            p.add(Look.note(text), GridBagConstraints().apply {
                gridx = 1; gridy = y; weightx = 1.0; fill = GridBagConstraints.HORIZONTAL
                anchor = GridBagConstraints.LINE_START; insets = Insets(0, 0, 8, 0)
            })
            y++
        }
        head(MagiBundle.msg("set.now"))
        row(MagiBundle.msg("set.doing"), doing)
        row(MagiBundle.msg("set.permission"), perm)
        row(MagiBundle.msg("set.session"), sessionL)
        row(MagiBundle.msg("set.model.now"), modelNow)
        row(MagiBundle.msg("set.backend.now"), backendNow)
        row(" ", outside)
        head(MagiBundle.msg("set.settings"))
        row(MagiBundle.msg("set.mode"), permission)
        note(MagiBundle.msg("set.mode.why"))
        row(MagiBundle.msg("set.look"), lookTyping)
        note(MagiBundle.msg("set.look.why"))
        row(MagiBundle.msg("set.complete"), autoComplete)
        // 자동완성 미동작 사유(`completeWhy`)를 표시합니다. 타건마다 팝업으로 알리는 대신 설정 화면에 안내하여
        // 라우팅 키(`autocomplete.code_profile`) 미설정 등 기능이 활성화되지 않는 원인을 사용자가 명확히 인지하도록 합니다.
        row("", completeWhy)
        row(MagiBundle.msg("set.suggest"), composerSuggest)
        row(MagiBundle.msg("set.autostart"), autostart)
        note(MagiBundle.msg("set.local.why"))
        row(MagiBundle.msg("set.model"), model)
        note(MagiBundle.msg("set.model.why"))
        row(MagiBundle.msg("set.backend"), backend)
        note(MagiBundle.msg("set.backend.why"))
        // 데몬의 설정 열거 엔드포인트에서 전달받은 동적 설정 키들을 렌더링합니다.
        // 클라이언트 코드에 설정 키를 하드코딩하지 않고 데몬 메타데이터를 기반으로 UI를 구성함으로써,
        // 데몬 측에 신규 설정 키가 추가되더라도 클라이언트 코드 수정 없이 자동으로 입력 항목이 확장됩니다.
        p.add(doorPane, GridBagConstraints().apply {
            gridx = 0; gridy = y++; gridwidth = 2
            anchor = GridBagConstraints.LINE_START
            fill = GridBagConstraints.HORIZONTAL
            weightx = 1.0
        })
        row(MagiBundle.msg("set.cron"), Look.note(MagiBundle.msg("set.cron.none"), Look.body))
        row(MagiBundle.msg("set.more"), Look.note(MagiBundle.msg("set.more.none"), Look.body))
        // 플릿·대기 작업은 설정보다 빈번히 조회되므로 도구 창(magi 패널)에서 관리합니다(docs/UI.ko.md §4.2).
        return p
    }

    override fun isModified(): Boolean =
        // 데몬에서 설정값을 성공적으로 읽어온 경우(`read != null`)에만 변경 여부를 비교합니다.
        // 미연결 상태에서 기본 선택값(`ask`)과 null을 비교하여 항상 변경된 것으로 오판정되어,
        // 단순 설정창 조회 후 저장 시 원치 않는 `ask` 권한이 전송되는 결함을 방지합니다.
        (read != null && (permission.selectedItem as? String) != read) ||
            (model.selectedItem as? String).orEmpty().isNotBlank() ||
            backend.text.isNotBlank() ||
            // 모든 동적 및 로컬 설정 항목의 변경 여부를 검사합니다.
            // IntelliJ 플랫폼은 isModified()가 true를 반환할 때만 apply()를 호출하므로,
            // 검사 누락 시 UI 변경 사항이 실제 설정에 반영되지 않고 유실되는 문제를 방지합니다.
            byDoor.any { doorFields[it.key]?.text?.trim() != it.value.orEmpty().trim() } ||
            lookTyping.isSelected != LocalPrefs.look(project) ||
            autoComplete.isSelected != LocalPrefs.complete(project) ||
            composerSuggest.isSelected != LocalPrefs.suggest(project) ||
            autostart.isSelected != LocalPrefs.autostart(project)

    /**
     * 데몬이 제공한 설정 키 목록을 기반으로 동적 설정 패널(`doorPane`)을 구성합니다.
     *
     * - 설정 파일 파싱 실패 계층은 값보다 우선하여 오류 메시지(`unreadable`)로 표시합니다(설정 오류와 미설정 상태의 시각적 구분).
     * - 각 설정 키의 적용 시점(`applies`) 및 출처(`source`)를 함께 안내하여 재기동 필요 여부를 명시합니다.
     */
    private fun paintDoor() {
        doorPane.removeAll()
        doorFields.clear()
        var dy = 0
        fun line(c: java.awt.Component, x: Int, w: Int, ins: Insets) {
            doorPane.add(c, GridBagConstraints().apply {
                gridx = x; gridy = dy; gridwidth = w
                anchor = GridBagConstraints.LINE_START
                if (x == 1) { weightx = 1.0; fill = GridBagConstraints.HORIZONTAL }
                insets = ins
            })
        }
        for (item in byDoor) {
            item.unreadable?.takeIf { it.isNotBlank() }?.let {
                line(Look.note(it, Look.error), 0, 2, Insets(4, 0, 0, 0)); dy++
            }
            // 프로필 타입 설정 키의 경우 선택 가능한 후보 목록(ComboBox)을 제공합니다.
            // 데몬 메타데이터(`profile: true`)에 기반하여 렌더링 형태를 결정하므로 클라이언트별 키 하드코딩을 방지합니다.
            val f: javax.swing.text.JTextComponent = if (item.profile) {
                val combo = JComboBox<String>().apply {
                    isEditable = true
                    addItem("")
                    choices.forEach(::addItem)
                    selectedItem = item.value.orEmpty()
                }
                // 편집 가능한 ComboBox의 내부 에디터 컴포넌트를 참조하여 텍스트 필드와 동일한 인터페이스로 값을 읽습니다.
                combo.editor.editorComponent as javax.swing.text.JTextComponent
            } else {
                javax.swing.JTextField(item.value.orEmpty(), 24)
            }
            doorFields[item.key] = f
            line(javax.swing.JLabel(item.key), 0, 1, Insets(4, 0, 4, 12))
            line((f.parent as? JComboBox<*>) ?: f, 1, 1, Insets(4, 0, 4, 0))
            dy++
            val why = listOfNotNull(
                item.doc?.takeIf { it.isNotBlank() },
                item.applies?.takeIf { it.isNotBlank() }?.let { MagiBundle.msg("set.applies", it) },
                item.source?.takeIf { it.isNotBlank() }?.let { MagiBundle.msg("set.from", it) },
            ).joinToString(" · ")
            if (why.isNotBlank()) { line(Look.note(why, Look.body), 1, 1, Insets(0, 0, 6, 0)); dy++ }
        }
        doorPane.revalidate(); doorPane.repaint()
    }

    override fun apply() {
        val mode = permission.selectedItem as? String
        val pick = (model.selectedItem as? String).orEmpty().trim()
        val prof = backend.text.trim()
        workspace.onDaemon({ tell(MagiBundle.msg("set.failed", it)) }) { comp ->
            val gripes = mutableListOf<String>()
            if (mode != null && read != null && mode != read) comp.setPermission(mode).also {
                if (!it.ok) gripes += MagiBundle.msg("chat.notsent", MagiBundle.msg("set.gripe.permission"), it.error ?: MagiBundle.msg("set.noreason"))
            }
            if (pick.isNotBlank()) comp.setModel(pick).also {
                if (!it.ok) gripes += MagiBundle.msg("chat.notsent", MagiBundle.msg("set.gripe.model"), it.error ?: MagiBundle.msg("set.noreason"))
            }
            if (prof.isNotBlank()) comp.useBackend(prof).also {
                if (!it.ok) gripes += MagiBundle.msg("chat.notsent", MagiBundle.msg("set.gripe.backend"), it.error ?: MagiBundle.msg("set.noreason"))
            }
            // 변경된 설정 키만 선별적으로 데몬에 반영합니다.
            // 전체 키를 무조건 저장할 경우 수정하지 않은 항목까지 현재 계층에 오버라이드되어 상위 설정 소스(`source`)의 상속 관계가 훼손되는 문제를 방지합니다.
            for (item in byDoor) {
                val now = doorFields[item.key]?.text?.trim() ?: continue
                if (now == item.value.orEmpty().trim()) continue
                comp.configSet(item.key, now).also {
                    if (!it.ok) gripes += MagiBundle.msg("chat.notsent", item.key, it.error ?: MagiBundle.msg("set.noreason"))
                }
            }
            pull(comp)
            LookWhileTyping.setEnabled(project, lookTyping.isSelected)
            LocalPrefs.setComplete(project, autoComplete.isSelected)
            LocalPrefs.setSuggest(project, composerSuggest.isSelected)
            LocalPrefs.setAutostart(project, autostart.isSelected)
            if (gripes.isEmpty()) tell(MagiBundle.msg("set.applied"))
            else tell(gripes.joinToString(" · "), trouble = true)
            SwingUtilities.invokeLater { model.selectedItem = ""; backend.text = "" }
        }
    }

    override fun reset() {
        sayOutside() // IDE 자체 분석 정보는 데몬 통신 전에 먼저 즉시 표시합니다 (비연결 워크스페이스에서도 경고 표시 필요)
        local() // IDE 로컬 설정은 데몬 연결 여부와 무관하게 즉시 로드합니다
        workspace.onDaemon({ tell(MagiBundle.msg("set.unreachable", it), trouble = true) }) { comp -> pull(comp); tell(" ") }
    }

    /**
     * IDE 로컬 구성 스위치 초기화.
     * 이 설정들은 `PropertiesComponent`에 저장되는 IDE 전용 설정이므로 데몬 응답을 대기하지 않고 즉시 반영합니다.
     * 과거 데몬 콜백([pull]) 내부에서 초기화하던 구조에서는 데몬 비연결 시 체크박스가 기본값(해제)으로 노출되어,
     * 확인 클릭 시 사용자의 기존 설정이 해제 상태로 덮어써지는 심각한 결함이 실측되었습니다(2026-09-01).
     */
    private fun local() {
        lookTyping.isSelected = LocalPrefs.look(project)
        autoComplete.isSelected = LocalPrefs.complete(project)
        composerSuggest.isSelected = LocalPrefs.suggest(project)
        autostart.isSelected = LocalPrefs.autostart(project)
    }

    /**
     * 워크스페이스 외부 프로젝트 루트 감지 및 경고 표시.
     * 컴패니언이 접근할 수 없는 외부 루트가 존재할 경우 상태 표시줄과 일관된 문구(`status.outside`)로 경고를 노출합니다(리뷰 R9).
     */
    private fun sayOutside() {
        val out = workspace.rootsOutsideWorkspace()
        if (out.isEmpty()) return say(outside, " ")
        say(outside, MagiBundle.msg("status.outside", out.size) + " — " +
            MagiBundle.msg("set.outside.what") + "\n" + out.joinToString("\n"))
    }

    private fun say(label: javax.swing.text.JTextComponent, text: String) =
        SwingUtilities.invokeLater { label.text = text }


    /** 데몬 런타임 상태를 화면에 동기화합니다. */
    private fun pull(comp: Companion) {
        val f = comp.facts()
        val cfg = comp.configGet().let { if (it.ok) it.config.orEmpty() else emptyList() }
        // 프로필 후보 목록은 프로필 타입 설정 키가 1개 이상 존재할 때만 조회하여 불필요한 네트워크 왕복을 방지합니다.
        val picks = if (cfg.any { it.profile }) {
            comp.profiles().let { r -> if (r.ok) r.profiles.orEmpty().map { it.name } else emptyList() }
        } else emptyList()
        SwingUtilities.invokeLater {
            byDoor = cfg
            choices = picks
            paintDoor()
            doing.text = when (val a = Activity.of(f)) {
                // 상태 표시줄과 동일한 리소스 키를 사용하여 다중 UI 간 의미적 일관성을 유지합니다(리뷰 R9).
                Activity.Waiting -> MagiBundle.msg("status.waiting")
                is Activity.Doing -> a.what
                Activity.Unsaid -> MagiBundle.msg("status.attached")
            }
            perm.text = Perms.label(f.permission)
            // 거절 메시지는 데몬이 반환한 원문 문자열을 그대로 표시하고, 정형화된 코드 사유(off, unrouted 등)만 리소스 번역을 적용합니다.
            val assist = dev.sayaya.magi.ide.usecase.Assist
            // 기정의된 리소스 키에 매핑되는 오류 코드만 번역 문장으로 변환하고,
            // 미정의 코드는 데몬이 반환한 원문 그대로 표시하여 리소스 번들 누락 키(`!key!`)가 노출되지 않도록 합니다(VS Code 클라이언트와 동일 규칙).
            completeWhy.text = assist.lastRefused?.let { MagiBundle.msg("set.complete.refused", it) }
                ?: assist.lastEmpty?.let { code ->
                    val key = dev.sayaya.magi.ide.usecase.Assist.emptyKey(code)
                    if (key != null) MagiBundle.msg(key, code) else code
                }.orEmpty()
            modelNow.text = f.model ?: MagiBundle.msg("set.unsaid")
            backendNow.text = f.backend ?: MagiBundle.msg("set.unsaid")
            // 데몬이 반환한 권한 모드가 표준 토큰 목록(`Perms.TOKENS`)에 없는 경우 콤보박스 모델에 동적으로 추가합니다.
            // 편집 불가 콤보박스가 미등록 값을 기본값(`ask`)으로 자동 롤백시켜, 사용자가 수정하지 않았음에도 `isModified`가 참이 되어 의도치 않은 저장이 발생하는 결함을 방지합니다(리뷰 R6).
            f.permission?.takeIf { it !in Perms.TOKENS }?.let { unknown ->
                val model = permission.model as javax.swing.DefaultComboBoxModel<String>
                if (model.getIndexOf(unknown) < 0) model.addElement(unknown)
            }
            sessionL.text = f.session
        }
        val m = comp.models()
        SwingUtilities.invokeLater {
            read = f.permission
            if (f.permission != null) permission.selectedItem = f.permission
            model.removeAllItems()
            model.addItem("")
            m.models?.forEach { model.addItem(it) }
            model.selectedItem = ""
            m.why?.let { tell(MagiBundle.msg("set.models.why", it)) }
        }
    }

    /**
     * 상태 메시지 및 강조 색상 갱신.
     *
     * ⚠ 메시지 텍스트와 색상 스타일(`trouble`)을 단일 메서드에서 원자적으로 갱신하여 텍스트와 상태 색상이 불일치하는 렌더링 결함을 방지합니다.
     * 데몬 미연결 상태에서는 표시된 필드값이 실제 런타임 상태가 아니므로, 사용자가 인지하지 못한 채 저장하여 덮어쓰지 않도록 오류 색상(`Look.warn`)으로 명확히 경고합니다.
     */
    private fun tell(text: String, trouble: Boolean = false) = SwingUtilities.invokeLater {
        said.text = text
        said.foreground = if (trouble) Look.warn else Look.faint
    }
}
