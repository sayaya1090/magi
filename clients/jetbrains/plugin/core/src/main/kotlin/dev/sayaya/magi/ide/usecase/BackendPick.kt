package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.ProviderChoice

/**
 * 정보 창 「백엔드」 콤보의 규칙. 항목은 두 종류다 — `[llm.profiles.*]` 의 이름(`use-profile`)과,
 * 지금 서빙 중인 프로바이더의 주소(`use-backend`). 주소를 데이터로 들고 이름으로 그린다: 같은 이름의
 * 프로필이 있어도 두 길이 섞이지 않게.
 */
object BackendPick {
    /** 주소 항목인가 — 그러면 `use-backend` 로 간다. */
    fun isProvider(entry: String): Boolean =
        entry.startsWith("http://") || entry.startsWith("https://")

    /** 프로필 뒤에 프로바이더 주소. 같은 것은 한 번만. */
    fun entries(profiles: List<String>, providers: List<ProviderChoice>): List<String> =
        (profiles + providers.map { it.base }.filter { it.isNotBlank() }).distinct()

    /** 지금 골라져 있어야 할 항목: 프로필이 있으면 그것, 없으면 지금 백엔드와 같은 프로바이더. */
    fun current(profile: String?, backend: String?, providers: List<ProviderChoice>): String {
        profile?.takeIf { it.isNotBlank() }?.let { return it }
        val now = backend?.trimEnd('/')?.takeIf { it.isNotBlank() } ?: return ""
        return providers.firstOrNull { it.base.trimEnd('/') == now }?.base ?: ""
    }

    /** 그릴 글자: 프로바이더는 그 이름, 빈 칸은 [fallback], 나머지는 그대로. */
    fun label(entry: String?, providers: List<ProviderChoice>, fallback: String): String {
        val e = entry.orEmpty()
        if (e.isBlank()) return fallback
        return providers.firstOrNull { it.base == e }?.name?.takeIf { it.isNotBlank() } ?: e
    }
}
