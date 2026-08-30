package dev.sayaya.magi.ide.ui

import com.intellij.ide.util.PropertiesComponent
import com.intellij.openapi.project.Project

/**
 * **이 화면의 취향** 셋 — 웹 콘솔이 브라우저-로컬로 두는 그 스위치들과 같은 자리다
 * (`SettingsElement` 의 lookover·autocomplete·suggest). 컴패니언의 설정이 아니라 보는
 * 사람의 것이라 데몬이 아니라 프로젝트 로컬에 산다: §5.1 의 「값은 데몬에」는 컴패니언의
 * 상태 이야기다.
 *
 * 기본값도 웹과 같다 — 훑어보기만 꺼짐. 멈출 때마다 백엔드를 쓰는 비용은 고를 일이고,
 * 나머지 둘은 라우팅된 빠른 프로필이 없으면 **데몬이 스스로 침묵**하므로 켜 둬도 조용하다.
 *
 * 이것이 magi 쪽 스위치를 대신하지 않는다: 데몬의 `[autocomplete]` 가 꺼져 있으면 여기서
 * 켜도 빈 답이 온다. 여기 것은 **이 IDE 가 그 문을 두드릴지**를 정한다.
 */
internal object LocalPrefs {

    private const val LOOK = "magi.lookWhileTyping"
    private const val COMPLETE = "magi.autocomplete"
    private const val SUGGEST = "magi.suggest"

    private fun p(project: Project) = PropertiesComponent.getInstance(project)

    fun look(project: Project): Boolean = p(project).getBoolean(LOOK, false)
    fun complete(project: Project): Boolean = p(project).getBoolean(COMPLETE, true)
    fun suggest(project: Project): Boolean = p(project).getBoolean(SUGGEST, true)

    fun setComplete(project: Project, on: Boolean) = p(project).setValue(COMPLETE, on, true)
    fun setSuggest(project: Project, on: Boolean) = p(project).setValue(SUGGEST, on, true)
    fun setLook(project: Project, on: Boolean) = p(project).setValue(LOOK, on, false)
}
