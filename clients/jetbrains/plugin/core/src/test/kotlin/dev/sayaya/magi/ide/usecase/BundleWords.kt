package dev.sayaya.magi.ide.usecase

import java.io.File
import java.util.Properties

/**
 * 시험용 [Rows.Words] - 실제 플러그인 번들(`intellij/src/main/resources/messages`)을 읽고 `MagiBundle.msg` 처럼
 * `MessageFormat` 으로 짓는다. core 는 플랫폼 없이 도는 모듈이라 번들을 직접 못 부르고, 시험은 파일을 읽는다.
 */
object BundleWords {
    fun of(lang: String): Rows.Words {
        val name = if (lang == "en") "MagiBundle.properties" else "MagiBundle_$lang.properties"
        val f = File("../intellij/src/main/resources/messages/$name")
        check(f.isFile) { "${f.absolutePath} 가 없다" }
        val p = Properties().apply { f.reader(Charsets.UTF_8).use { load(it) } }
        return Rows.Words { key, args ->
            val raw = p.getProperty(key) ?: error("$name 에 $key 가 없다")
            java.text.MessageFormat.format(raw, *args)
        }
    }
}
