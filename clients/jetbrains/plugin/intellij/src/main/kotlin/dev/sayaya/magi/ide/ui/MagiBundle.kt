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
 * **자바의 폴백 규칙을 그대로 두면 안 된다.** `ResourceBundle` 의 기본 컨트롤은 청한 로케일에서
 * 못 찾으면 **`Locale.getDefault()` 로** 한 번 더 간다. 이 기계의 JVM 기본이 `ko_KR` 이라,
 * 영어를 청해도 `_ko` 파일이 있으면 그것이 뽑혔다 — IDE 의 나머지 화면은 전부 영어인데 우리
 * 판만 한국어였다(사용자 지적: "다른 설정은 다 영문인데 우리꺼만 한글이야"). 인텔리제이 자체
 * 번들은 `_ko` 를 안 실어서 그 함정에 안 걸린다. 그래서 `getNoFallbackControl` 을 쓴다 —
 * **이 기전은 `BundleFallbackTest` 가 잰다**(두 컨트롤의 차이로).
 *
 * ⚠ 아래 [locale] 의 언어팩 갈래는 **그 결함의 원인이 아니었다.** 한동안 원인을 「언어팩이
 * 없으면 IDE 로케일이 JVM 기본으로 샌다」로 적어 두었는데, 라이브 로그가 그것을 뒤집었다:
 * `magi: UI language = en (language pack: 3)` — 팩이 셋 있는 기계에서 IDE 로케일은 정확히
 * `en` 이었다. 즉 이 기계에서 그 갈래는 늘 `getLocale()` 쪽으로만 간다. 팩이 0개인 경우는
 * 아직 아무도 안 봤으므로 갈래를 지우지는 않되, **근거 없이 「실측」이라 적어 두지도 않는다.**
 * 로그 한 줄이 두 입력(고른 언어·팩 수)을 다 적으므로 다음 사람은 어느 갈래가 돌았는지 안다.
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
        // **인자 유무로 갈리지 않는다.** 갈라 두면 값 안의 홑따옴표가 「인자를 받는 값이냐」에
        // 따라 뜻을 바꾸고, 그 규칙은 파일 어디에도 안 적힌다 — 누가 기존 값에 `{0}` 을 하나
        // 넣는 순간 옆에 있던 `magi's` 의 따옴표가 그 자리표시자를 조용히 먹는다(리뷰 R10).
        // 한 규칙으로 못박는다: 값은 언제나 MessageFormat 을 지나고, 따옴표는 두 번 적는다.
        return java.text.MessageFormat.format(raw, *params)
    }
}
