package dev.sayaya.magi.ide.ui

import com.intellij.codeInsight.inline.completion.InlineCompletionEvent
import com.intellij.codeInsight.inline.completion.InlineCompletionProvider
import com.intellij.codeInsight.inline.completion.InlineCompletionProviderID
import com.intellij.codeInsight.inline.completion.InlineCompletionRequest
import com.intellij.codeInsight.inline.completion.elements.InlineCompletionGrayTextElement
import com.intellij.codeInsight.inline.completion.suggestion.InlineCompletionSingleSuggestion
import com.intellij.codeInsight.inline.completion.suggestion.InlineCompletionSuggestion
import com.intellij.openapi.application.readAction
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.usecase.Assist
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * 커서 자리에 회색 글씨로 이어붙일 것을 띄운다 — 콘솔의 그 기능을 IDE 로 옮긴 것이다.
 *
 * 부르는 것은 데몬의 `complete` 이고, 콘솔의 `/complete` 라우트가 부르는 것과 **같은 메서드**다
 * (`daemon.go` 의 `Client.CompleteCode`). 두 화면이 같은 문을 쓰므로 켜고 끄는 스위치도 하나다 —
 * magi 쪽 `[autocomplete]` 설정이 정하고, 꺼져 있으면 데몬이 빈 문자열을 돌려준다. **플러그인이
 * 두 번째 스위치를 만들지 않는다**(설계 문서 §5).
 *
 * 접두와 접미를 둘 다 보낸다. 커서 **뒤**를 모르는 모델은 이미 닫힌 괄호를 또 닫는다.
 *
 * **선 제안을 거두는 문은 여기 없고, 없는 것이 맞다 — 재 보고 적는다.** 채팅 입력줄의 거들기는
 * 라벨이 내 것이라 내가 안 지우면 아무도 안 지웠고 그게 결함이었다(`43128efe`). 여기는 회색
 * 글씨를 플랫폼이 그리고 플랫폼이 거둔다: `InlineCompletionDocumentListener.
 * documentChangedNonBulk` 가 `hideInlineCompletion` 과 `collectTypedCharOrInvalidateSession` 을
 * 부르고, 세션 매니저에 `Invalidated.Reason.UnclassifiedDocumentChange` 라는 갈래가 따로 있다
 * (2026.1.5 바이트코드에서 확인). **「플랫폼이 해 주겠지」로 통과시킨 게 아니라 그 문을 찾아서
 * 통과시킨 것**이고, 이 문단이 있는 이유가 그것이다 — 맞는 답과 맞는 근거는 다른 말이라,
 * 안 적어 두면 다음 사람이 같은 자리를 「거두는 갈래가 없다」로 읽고 없는 결함을 고친다.
 */
class MagiInlineCompletion : InlineCompletionProvider {

    override val id = InlineCompletionProviderID("magi")

    /**
     * 사람이 치는 동안에만 낸다. `InlineCompletionEvent.DocumentChange` 만 받는 이유는, 커서를
     * 움직이거나 창을 옮길 때마다 모델을 부르면 초 단위 호출이 화면 이동에 실려 오기 때문이다.
     */
    override fun isEnabled(event: InlineCompletionEvent): Boolean = event is InlineCompletionEvent.DocumentChange

    override suspend fun getSuggestion(request: InlineCompletionRequest): InlineCompletionSuggestion {
        val doc = request.document
        val offset = request.endOffset
        // 문서를 읽는 것은 읽기 액션 안에서. 밖에서 읽으면 편집 중 스냅샷이 찢어진다.
        val (prefix, suffix, path) = readAction {
            Triple(
                doc.getText(com.intellij.openapi.util.TextRange(0, offset)),
                doc.getText(com.intellij.openapi.util.TextRange(offset, doc.textLength)),
                request.file.virtualFile?.path ?: request.file.name,
            )
        }
        val sock = Workspace(request.editor.project ?: return empty()).socket() ?: return empty()
        // 소켓은 블로킹이라 IO 로 보낸다. 이 호출은 모델 호출이라 초 단위이고, 그동안 EDT 를
        // 잡고 있으면 타이핑이 선다.
        val text = withContext(Dispatchers.IO) {
            runCatching { Assist({ DaemonClient.connect(sock) }).completeCode(path, prefix, suffix) }.getOrNull()
        }
        if (text.isNullOrEmpty()) return empty()
        return InlineCompletionSingleSuggestion.build { emit(InlineCompletionGrayTextElement(text)) }
    }

    /** 낼 것이 없을 때. 빈 제안은 에러가 아니다 — 모델이 할 말이 없는 것이 보통이다. */
    private fun empty(): InlineCompletionSuggestion = InlineCompletionSuggestion.Empty
}
