package dev.sayaya.magi.ide.ui

import com.intellij.DynamicBundle
import org.jetbrains.annotations.Nls
import org.jetbrains.annotations.PropertyKey
import java.util.Locale
import java.util.ResourceBundle

private const val PATH = "messages.MagiBundle"

/**
 * 플러그인 UI 리소스 번들의 진입점. IDE가 지정한 언어 설정을 준수한다 (기본 영문, 한국어 언어팩 활성화 시 한국어).
 *
 * 자바 `ResourceBundle`의 기본 폴백 메커니즘을 그대로 적용할 경우, 요청된 로케일에서 키를 찾지 못하면
 * `Locale.getDefault()`(호스트 시스템 JVM 로케일)로 재차 폴백한다. 시스템 JVM 기본값이 `ko_KR`인 환경에서는
 * IDE 로케일이 영문으로 설정되어 있어도 `_ko` 번들이 우선 선택되어 UI 텍스트가 한국어로 노출되는 결함이 발생했다
 * (실측 사용자 피드백: "다른 설정은 다 영문인데 우리꺼만 한글이야"). 플랫폼 내장 번들은 `_ko` 리소스를 포함하지
 * 않아 이 문제가 발생하지 않았으므로, 본 플러그인에서는 `ResourceBundle.Control.getNoFallbackControl()`을 지정하여
 * JVM 기본값으로의 의도치 않은 누출을 차단한다. 해당 동작은 `BundleFallbackTest`를 통해 검증된다.
 *
 * 과거 언어팩 부재 시 로케일 누출 방지를 위해 `DynamicBundle.LanguageBundleEP`를 사용하던 갈래는 제거되었다:
 * 1. 라이브 로그 실측(`magi: UI language = en (language pack: 3)`) 결과, 언어팩이 설치된 환경에서도 IDE 로케일은 정확히 `en`으로 보고되어 해당 갈래가 호출되지 않았다.
 * 2. 해당 확장점은 IntelliJ 내부 API(Internal API)로 분류되어 릴리스 검증 단계(`verifyPlugin`)에서 `INTERNAL_API_USAGES` 위반으로 차단된다.
 * 따라서 플랫폼 공식 API인 `DynamicBundle.getLocale()`을 따르고 번들 레벨의 폴백 차단(`getNoFallbackControl`)에 일원화한다.
 *
 * 클래스 상속 대신 [DynamicBundle] 인스턴스에 대한 위임(Delegation) 방식을 취하는 것은 JetBrains 공식 권장 사항이다 (플랫폼 규약 대조표 §2).
 */
object MagiBundle {

    private val delegate = DynamicBundle(MagiBundle::class.java, PATH)

    /** IDE 환경에 설정된 현재 로케일을 반환한다. 조회가 실패할 경우 영문(Locale.ENGLISH)을 기본값으로 사용한다. */
    fun locale(): Locale =
        runCatching { DynamicBundle.getLocale() }.getOrDefault(Locale.ENGLISH)

    private val LOG = com.intellij.openapi.diagnostic.Logger.getInstance(MagiBundle::class.java)

    private val bundle: ResourceBundle? by lazy {
        // 런타임 언어 결정 원인 추적을 위해 로케일 정보를 로깅한다.
        LOG.info("magi: UI language = " + locale().toLanguageTag() +
            " (IDE locale; JVM default is " + Locale.getDefault().toLanguageTag() + ")")
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
            ?: return delegate.getMessage(key, *params) // 조회 실패 시 플랫폼 번들 처리 경로로 위임
        // 인자 존재 여부와 무관하게 모든 텍스트에 MessageFormat을 일관되게 적용한다.
        // 인자 유무에 따라 포맷팅 경로를 분기할 경우 홑따옴표(')의 이스케이프 해석 규칙이 달라져, 향후 매개변수({0})
        // 추가 시 기존 텍스트(예: magi's)의 홑따옴표가 파라미터를 무효화하는 결함이 발생할 수 있다 (리뷰 R10).
        // 따라서 모든 리소스 텍스트는 일관되게 MessageFormat 규약(홑따옴표는 ''로 이스케이프)을 준수한다.
        return java.text.MessageFormat.format(raw, *params)
    }
}
