package dev.sayaya.magi.ide.ui

import com.intellij.openapi.editor.Editor
import com.intellij.openapi.fileEditor.FileDocumentManager
import dev.sayaya.magi.ide.model.FileRef

/**
 * 편집기에서 **참조를 뜨는 한 자리**.
 *
 * 세 곳이 이 일을 각자 적고 있었다 — 우클릭 액션, Alt+Enter 의 「물어보기」, Alt+Enter 의
 * 「추가」. 그 안에는 리뷰로 산 교훈이 하나 들어 있다: **발췌는 코어가 디스크에서 읽으므로**
 * (`internal/app/refs.go` 의 `renderRef`) 붙이기 전에 저장해야 한다. 안 그러면 버퍼로 센
 * 줄번호가 저장 안 한 디스크와 갈려, 다른 텍스트가 "에이전트가 본 것"으로 영속된다.
 * 그 교훈이 세 곳에 흩어져 있으면 한 곳만 고치는 날이 온다.
 *
 * **다만 셋이 똑같지는 않았다.** 「선택이 없을 때」에서 갈렸고, 그건 실수가 아니라 자리마다
 * 옳은 답이 달라서다 — 우클릭은 파일을 겨누고, Alt+Enter 는 캐럿이 선 줄을 겨눈다. 합치면서
 * 그 갈림을 감추지 않고 [WhenBare] 로 **이름을 붙여** 부르는 쪽이 고르게 한다.
 */
internal object Attach {

    /** 고른 것이 없을 때 무엇을 붙이나. */
    enum class WhenBare {
        /** 파일 전체 — 우클릭 「채팅에 추가」. 손이 파일을 겨누고 있다. */
        WholeFile,

        /** 캐럿이 선 줄들 — Alt+Enter. 「이 코드」가 가리키는 것은 지금 그 줄이다. */
        CaretLines,

        /** 아무것도 — 고른 것이 있을 때만 서는 자리. */
        Nothing,
    }

    /**
     * [editor] 에서 참조를 뜬다. **문서를 먼저 저장한다**(위 사유).
     *
     * 캐럿마다 하나다. 멀티캐럿 선택의 나머지가 소리 없이 빠지면 「사라지는 첨부 없음」이
     * 클라이언트에서 깨진다. 줄은 에디터 셈법(1-기준 포함)이고, 계약의 낱말 그대로다
     * ([FileRef] 의 `lines`).
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
            // 선택 끝이 줄머리에 걸치면 그 줄은 실제로 안 골라진 것이다.
            val to = doc.getLineNumber((c.selectionEnd - 1).coerceAtLeast(c.selectionStart)) + 1
            // 한 줄이면 "5" — 세 자리가 같은 표기라야 같은 줄의 칩이 중복으로 안 선다.
            FileRef(path, if (from == to) "$from" else "$from-$to")
        }
    }
}
