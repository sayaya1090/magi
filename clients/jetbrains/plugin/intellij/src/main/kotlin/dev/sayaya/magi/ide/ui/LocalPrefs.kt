package dev.sayaya.magi.ide.ui

import com.intellij.ide.util.PropertiesComponent
import com.intellij.openapi.project.Project

/**
 * IDE 클라이언트의 로컬 동작 스위치 3종 관리.
 *
 * 웹 콘솔의 브라우저 로컬 저장소 스위치(`SettingsElement`의 lookover, autocomplete, suggest)와 동일한 위상을 가진다.
 * 에이전트 데몬 공용 설정이 아닌 사용자별 에디터 상호작용 선호도이므로 전역 데몬이 아닌 프로젝트 로컬 [PropertiesComponent]에 영속화한다.
 * (설계 원칙 §5.1의 "설정의 단일 진실 원천은 데몬"은 컴패니언의 에이전트 상태에 해당함).
 *
 * 기본값 역시 웹 콘솔과 동일하게 타이핑 검토(`lookWhileTyping`)만 비활성화(false) 상태로 시작한다.
 * 타이핑 중단 시마다 LLM 백엔드를 호출하는 비용은 사용자 선택에 맡기며, 나머지 자동완성 및 제안 스위치는
 * 라우팅된 빠른 모델 프로필이 없는 경우 데몬 백엔드가 자체적으로 침묵(무응답)하므로 기본 활성화되어도 안전하다.
 *
 * 이 로컬 설정은 데몬의 글로벌 기능을 강제하지 않는다. 데몬의 `[autocomplete]`가 꺼져 있으면 IDE에서 요청을 전송해도
 * 빈 응답이 반환된다. 즉, 본 설정은 "이 IDE 클라이언트가 해당 기능 엔드포인트를 호출할지 여부"를 결정한다.
 */
internal object LocalPrefs {

    private const val LOOK = "magi.lookWhileTyping"
    private const val COMPLETE = "magi.autocomplete"
    private const val SUGGEST = "magi.suggest"

    /**
     * 프로젝트 오픈 시 데몬 미실행 상태인 경우 자동 기동 여부.
     * 기본값은 활성화(true)이다 (워크스페이스가 결정된 상태에서 데몬이 없으면 자동 기동하는 원칙).
     * 단, 여러 프로젝트를 동시에 여는 환경에서 다중 데몬 프로세스 구동 부담을 회피하고자 하는 경우 비활성화할 수 있다.
     */
    private const val AUTOSTART = "magi.autostart"

    private fun p(project: Project) = PropertiesComponent.getInstance(project)

    fun look(project: Project): Boolean = p(project).getBoolean(LOOK, false)
    fun complete(project: Project): Boolean = p(project).getBoolean(COMPLETE, true)
    fun suggest(project: Project): Boolean = p(project).getBoolean(SUGGEST, true)
    fun autostart(project: Project): Boolean = p(project).getBoolean(AUTOSTART, true)

    fun setComplete(project: Project, on: Boolean) = p(project).setValue(COMPLETE, on, true)
    fun setSuggest(project: Project, on: Boolean) = p(project).setValue(SUGGEST, on, true)
    fun setLook(project: Project, on: Boolean) = p(project).setValue(LOOK, on, false)
    fun setAutostart(project: Project, on: Boolean) = p(project).setValue(AUTOSTART, on, true)
}
