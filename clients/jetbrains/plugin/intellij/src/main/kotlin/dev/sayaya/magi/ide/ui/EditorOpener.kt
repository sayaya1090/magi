package dev.sayaya.magi.ide.ui

import com.intellij.diff.DiffContentFactory
import com.intellij.diff.DiffManager
import com.intellij.diff.requests.SimpleDiffRequest
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.fileTypes.FileTypeManager
import com.intellij.openapi.fileTypes.PlainTextFileType
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Key
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.testFramework.LightVirtualFile
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Rows
import dev.sayaya.magi.ide.usecase.Who
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/**
 * 전사 결과 원문·승인 변경 diff·도구 편집 diff 를 IDE 편집창 및 Diff 뷰어로 여는 협력 객체입니다.
 *
 * 문서 생성, 열린 파일 식별(사용자 데이터 키 기반), 읽기 전용 가상 파일 구성을 담당하며,
 * UI 상태(초안, 알림 안내)나 대화 세션 관리는 보유하지 않습니다.
 */
internal class EditorOpener(
    private val project: Project,
    private val outputOpener: ((VirtualFile) -> Unit)? = null,
    private val approvalPatchOpener: ((VirtualFile) -> Unit)? = null,
    private val approvalDiffPresenter: ((SimpleDiffRequest) -> Unit)? = null,
) {
    companion object {
        /** 원문 출력 열기 식별자 키: [session, who.name, callId, outputSeq] */
        val OUTPUT_KEY: Key<List<String>> = Key.create("magi.output.source")

        /** 승인 패치 파일 식별자 키: [session, waiting.id] */
        val APPROVAL_PATCH_KEY: Key<List<String>> = Key.create("magi.approval.patch")
    }

    /**
     * 확정 답변 또는 도구 실행 결과 원문을 읽기 전용 편집창 탭으로 엽니다.
     * 이미 열려 있는 탭이 있으면 재사용합니다.
     */
    fun openOutput(session: String, row: Row) {
        val text = row.outputText ?: return
        val seq = row.outputSeq ?: return
        val identity = listOf(session, row.who.name, row.callId, seq.toString())
        val manager = FileEditorManager.getInstance(project)
        val file = manager.openFiles.firstOrNull { it.getUserData(OUTPUT_KEY) == identity } ?: run {
            val extension = if (row.who == Who.Agent) "md" else if (row.outputJson) "json" else "txt"
            val type = FileTypeManager.getInstance().getFileTypeByExtension(extension)
            LightVirtualFile("magi-output-$seq.$extension", type, text).apply {
                isWritable = false
                putUserData(OUTPUT_KEY, identity)
            }
        }
        if (outputOpener != null) outputOpener.invoke(file) else manager.openFile(file, true)
    }

    /**
     * 승인 요청에 첨부된 변경 사항을 IDE 편집기(두 면 diff 또는 원문 패치 탭)로 엽니다.
     */
    fun openApprovalDiff(session: String, w: Waiting) {
        val o = w.args as? JsonObject
        fun str(k: String) = (o?.get(k) as? JsonPrimitive)?.takeIf { it.isString }?.content
        val path = str("path") ?: "변경"
        // 판정은 core 의 한 벌에 위임한다 — 두 벌로 적힌 동안 FlexBool 모양("yes"·1)에서
        // 갈라졌었다(리뷰). 여기 것과 전사 것이 같은 함수를 부르므로 갈라질 자리가 없다.
        val sides = Rows.EditSides.of(w.what, o?.toString())
        if (sides != null) {
            val f = DiffContentFactory.getInstance()
            val request = SimpleDiffRequest(
                MagiBundle.msg("chat.diff.title.ok", sides.first),
                f.create(project, sides.second), f.create(project, sides.third),
                MagiBundle.msg("chat.diff.asked"), MagiBundle.msg("chat.diff.proposed"),
            )
            if (approvalDiffPresenter != null) approvalDiffPresenter.invoke(request)
            else DiffManager.getInstance().showDiff(project, request)
            return
        }
        val manager = FileEditorManager.getInstance(project)
        val identity = listOf(session, w.id)
        val vf = manager.openFiles.firstOrNull { it.getUserData(APPROVAL_PATCH_KEY) == identity } ?: run {
            // 파일 타입을 plain text 로 못박는다(라이브 실측): 이름이 .diff 면 IntelliJ 의
            // 패치 에디터가 잡는데, 코어의 write 승인 diff 는 헤더(---/+++/@@) 없는 헝크라
            // "Invalid patch file" 판이 선다 — 원문 diff 를 그대로 보여 주는 것이 계약이고
            // (재계산 금지), 항상 읽히는 쪽이 색입힘보다 먼저다.
            LightVirtualFile(
                "magi-승인-${path.substringAfterLast('/')}-${w.id.takeLast(6)}.diff",
                PlainTextFileType.INSTANCE, w.diff.orEmpty(),
            ).apply { isWritable = false; putUserData(APPROVAL_PATCH_KEY, identity) }
        }
        if (approvalPatchOpener != null) approvalPatchOpener.invoke(vf) else manager.openFile(vf, true)
    }

    /**
     * 완료된 도구 호출(Row)의 편집 전후 변경 내역을 두 면 Diff 뷰어로 엽니다.
     */
    fun openEditDiff(path: String, old: String, fresh: String) {
        val f = DiffContentFactory.getInstance()
        val request = SimpleDiffRequest(
            MagiBundle.msg("chat.diff.title.edit", path),
            f.create(project, old), f.create(project, fresh),
            MagiBundle.msg("chat.diff.before"), MagiBundle.msg("chat.diff.after"),
        )
        if (approvalDiffPresenter != null) approvalDiffPresenter.invoke(request)
        else DiffManager.getInstance().showDiff(project, request)
    }
}
