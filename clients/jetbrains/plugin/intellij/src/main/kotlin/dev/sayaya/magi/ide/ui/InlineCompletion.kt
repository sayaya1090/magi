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
 * 커서 위치에 회색 텍스트(Gray Text) 인라인 자동완성 제안을 제공한다.
 *
 * 백엔드 데몬의 `complete` 엔드포인트(`internal/adapter/daemon/client.go`의 `Client.CompleteCode`)를 호출하며,
 * 데몬의 `[autocomplete]` 설정과 연동된다. 플러그인은 별도의 독립 토글을 생성하지 않고 데몬 및 로컬 환경 설정을 따른다 (설계 문서 §5).
 * 커서 전후 문맥을 모두 반영하기 위해 접두사(prefix)와 접미사(suffix)를 함께 전달하여 괄호 중복 생성 등을 방지한다.
 *
 * 제안 무효화(Invalidation) 수명주기:
 * 인라인 완성 제안의 폐기는 플러그인이 아닌 플랫폼 엔진이 직접 관리한다.
 * IntelliJ 플랫폼의 `InlineCompletionDocumentListener.documentChangedNonBulk`가 `hideInlineCompletion`과
 * `collectTypedCharOrInvalidateSession`을 호출하며, 세션 매니저의 `Invalidated.Reason.UnclassifiedDocumentChange`를 통해
 * 문서 변경 시 기존 제안이 즉시 무효화된다.
 * (검증 환경: `idea-2026.1.5-aarch64`, 2026-08-29 바이트코드 실측)
 *
 * ```
 * javap -p -c -cp <IDEA>/lib/intellij.platform.lang.impl.jar \
 *   com.intellij.codeInsight.inline.completion.listeners.typing.InlineCompletionDocumentListener
 * ```
 */
class MagiInlineCompletion : InlineCompletionProvider {

    override val id = InlineCompletionProviderID("magi")

    /**
     * 사용자의 실제 타이핑 입력([InlineCompletionEvent.DocumentChange]) 시에만 완성을 활성화한다.
     * 커서 이동이나 창 전환 등 화면 네비게이션 시 불필요한 LLM 추론 호출을 방지한다.
     */
    override fun isEnabled(event: InlineCompletionEvent): Boolean = event is InlineCompletionEvent.DocumentChange

    override suspend fun getSuggestion(request: InlineCompletionRequest): InlineCompletionSuggestion {
        val doc = request.document
        val offset = request.endOffset
        // 문서 텍스트 스냅샷의 일관성을 위해 ReadAction 내에서 접두사 및 접미사를 획득한다.
        // 커서 근처만 읽는다. 통째로 읽으면 4만 줄 파일에서 **타건이 멈출 때마다** 버퍼 전체가
        // ReadAction 안에서 복사되고 그대로 소켓에 실리는데, 코어는 한쪽 24KB 만 쓰고 나머지를
        // 버린다(`internal/app/complete.go` 의 `completeCap`, 그 주석이 이 비용을 이름 댄다).
        val cap = dev.sayaya.magi.ide.usecase.Assist.SIDE_CAP
        val (prefix, suffix, path) = readAction {
            Triple(
                doc.getText(com.intellij.openapi.util.TextRange(maxOf(0, offset - cap), offset)),
                doc.getText(com.intellij.openapi.util.TextRange(
                    offset, minOf(doc.textLength, offset + cap))),
                request.file.virtualFile?.path ?: request.file.name,
            )
        }
        val project = request.editor.project ?: return empty()
        // 프로젝트 로컬 자동완성 스위치(LocalPrefs.complete)가 꺼져 있으면 데몬 호출을 건너뛴다.
        if (!LocalPrefs.complete(project)) return empty()
        val sock = Workspace(project).socket() ?: return empty()
        // 모델 추론 및 소켓 통신으로 인한 UI 스레드 블로킹을 방지하기 위해 IO 디스패처에서 실행한다.
        val text = withContext(Dispatchers.IO) {
            runCatching { Assist({ DaemonClient.connect(sock) }).completeCode(path, prefix, suffix) }.getOrNull()
        }
        if (text.isNullOrEmpty()) return empty()
        return InlineCompletionSingleSuggestion.build { emit(InlineCompletionGrayTextElement(text)) }
    }

    /** 제안할 완성이 없을 때 반환하는 빈 제안 객체. */
    private fun empty(): InlineCompletionSuggestion = InlineCompletionSuggestion.Empty
}
