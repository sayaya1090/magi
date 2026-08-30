package dev.sayaya.magi.ide.ui

import com.intellij.DynamicBundle
import org.jetbrains.annotations.Nls
import org.jetbrains.annotations.PropertyKey
import java.util.Locale
import java.util.ResourceBundle

private const val PATH = "messages.MagiBundle"

/**
 * 화면 글자의 한 자리. **IDE 가 정한 언어를 따른다** — 기본은 영어, 한국어 언어팩이 깔린
 * IDE 만 한국어.
 *
 * **자바의 폴백 규칙을 그대로 두면 안 된다(실측).** 언어팩이 없으면 IDE 로케일이 JVM 기본으로
 * 떨어지는데, 이 기계의 JVM 기본은 `ko_KR` 이라 `_ko` 파일이 선택됐다 — IDE 의 나머지 화면은
 * 전부 영어인데 우리 판만 한국어였다(사용자 지적: "다른 설정은 다 영문인데 우리꺼만 한글이야").
 * 인텔리제이 자체 번들은 `_ko` 를 안 실어서 그 함정에 안 걸린다.
 *
 * 그래서 **언어팩이 실제로 있는가**를 보고 정한다: 없으면 로케일을 영어로 못박고, 있으면 그
 * 팩의 로케일을 쓴다. 폴백도 끈다(`getNoFallbackControl`) — 켜 두면 다시 JVM 기본으로 샌다.
 *
 * 상속이 아니라 위임인 이유는 JetBrains 권장(플랫폼 규약 대조표 §2).
 */
object MagiBundle {

    private val delegate = DynamicBundle(MagiBundle::class.java, PATH)

    /** 언어팩이 깔려 있으면 그 로케일, 아니면 영어. JVM 기본은 근거가 아니다. */
    private fun locale(): Locale {
        val pack = runCatching { DynamicBundle.LanguageBundleEP.EP_NAME.extensionList.isNotEmpty() }
            .getOrDefault(false)
        return if (pack) DynamicBundle.getLocale() else Locale.ENGLISH
    }

    private val LOG = com.intellij.openapi.diagnostic.Logger.getInstance(MagiBundle::class.java)

    private val bundle: ResourceBundle? by lazy {
        // 어느 언어로 그리는지 한 번 적어 둔다 — 「왜 한글이지?」를 화면만 보고는 못 가른다
        // (언어팩 때문인지 JVM 기본이 샌 것인지). 사용자가 그 질문을 실제로 했다.
        LOG.info("magi: UI language = " + locale().language + " (language pack: " +
            runCatching { DynamicBundle.LanguageBundleEP.EP_NAME.extensionList.size }.getOrDefault(-1) + ")")
        runCatching {
            ResourceBundle.getBundle(
                PATH, locale(), MagiBundle::class.java.classLoader,
                ResourceBundle.Control.getNoFallbackControl(ResourceBundle.Control.FORMAT_PROPERTIES),
            )
        }.getOrNull()
    }

    @Nls
    fun msg(@PropertyKey(resourceBundle = PATH) key: String, vararg params: Any): String {
        val raw = runCatching { bundle?.getString(key) }.getOrNull()
            ?: return delegate.getMessage(key, *params) // 못 찾으면 플랫폼 경로가 답하게 둔다
        return if (params.isEmpty()) raw else java.text.MessageFormat.format(raw, *params)
    }
}
