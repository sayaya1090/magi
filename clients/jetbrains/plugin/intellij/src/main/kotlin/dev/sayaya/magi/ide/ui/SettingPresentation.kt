package dev.sayaya.magi.ide.ui

import dev.sayaya.magi.ide.model.ConfigItem

/** Presentation only: daemon keys and values remain unchanged when saving. */
internal object SettingPresentation {
    private val known = setOf("embed_model", "autocomplete.code_profile", "autocomplete.composer_profile", "templates.commit", "templates.pr")
    fun label(item: ConfigItem): String =
        if (item.key in known) MagiBundle.msg("setting.${item.key}.label") else item.key
    fun description(item: ConfigItem): String? =
        if (item.key in known) MagiBundle.msg("setting.${item.key}.description") else item.doc?.takeIf { it.isNotBlank() }
    fun applies(value: String): String = when (value) {
        "next start" -> MagiBundle.msg("setting.applies.restart")
        "now" -> MagiBundle.msg("setting.applies.now")
        else -> value
    }
    fun source(value: String): String = when (value) {
        "global", "project", "companion", "env", "default" -> MagiBundle.msg("setting.source.$value")
        else -> value
    }
}
