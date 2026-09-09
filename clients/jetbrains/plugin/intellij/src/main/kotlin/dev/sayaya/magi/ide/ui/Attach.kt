package dev.sayaya.magi.ide.ui

import com.intellij.openapi.editor.Editor
import com.intellij.openapi.fileEditor.FileDocumentManager
import dev.sayaya.magi.ide.model.FileRef

/**
 * 에디터 내 파일 및 라인 참조([FileRef]) 생성 유틸리티.
 *
 * 우클릭 팝업 메뉴, Alt+Enter 인텐션 등에서 공통으로 호출된다.
 * 코어 데몬이 디스크에서 파일을 판독(`internal/app/refs.go`의 `renderRef`)하므로,
 * 버퍼 상의 라인 번호와 디스크 내용 간의 불일치를 방지하기 위해 참조 생성 전 문서를 디스크에 선반영한다.
 * 선택 영역 부재 시의 동작 정책은 [WhenBare] 열거형으로 지정한다.
 */
internal object Attach {

    /** 선택 영역 부재 시 참조 생성 전략. */
    enum class WhenBare {
        /** 파일 전체 참조 — 에디터 우클릭 컨텍스트 메뉴 등. */
        WholeFile,

        /** 현재 캐럿이 위치한 라인 참조 — Alt+Enter 인텐션 액션 등. */
        CaretLines,

        /** 참조 미생성 (빈 목록). */
        Nothing,
    }

    /**
     * [editor]로부터 파일 참조 목록을 생성한다. 미저장 문서는 디스크에 선저장된다.
     * 멀티캐럿 선택 영역을 모두 수집하며, 에디터 기준 1-based 라인 번호를 적용한다.
     */
    fun refs(editor: Editor, path: String, whenBare: WhenBare): List<FileRef> {
        FileDocumentManager.getInstance().saveDocument(editor.document)
        val doc = editor.document
        val picked = editor.caretModel.allCarets.filter { it.hasSelection() }
        if (picked.isEmpty()) return when (whenBare) {
            WhenBare.WholeFile -> listOf(FileRef(path))
            WhenBare.CaretLines ->
                editor.caretModel.allCarets.map { FileRef(path, "${doc.getLineNumber(it.offset) + 1}") }
            WhenBare.Nothing -> emptyList()
        }
        return picked.map { c ->
            val from = doc.getLineNumber(c.selectionStart) + 1
            // 선택 영역 끝이 라인 첫 오프셋에 걸치는 경우 해당 라인은 선택 범위에서 제외
            val to = doc.getLineNumber((c.selectionEnd - 1).coerceAtLeast(c.selectionStart)) + 1
            // 단일 라인인 경우 "5", 다중 라인인 경우 "5-10" 형식으로 일관되게 표기
            FileRef(path, if (from == to) "$from" else "$from-$to")
        }
    }
}
