plugins {
    id("java")
    kotlin("jvm") version "2.3.0" apply false
    id("dev.sayaya.gwt") version "2.2.9.5" apply false
}

// 이 콘솔은 안쪽도 모듈로 나뉜다 — 그래서 모듈마다 같은 빌드 스크립트를 베껴 두지 않고,
// 여기서 한 벌로 구성한다. 모듈이 대는 것은 **제 이름 두 개**(GWT 모듈, 테스트 모듈)뿐이고
// 나머지(의존성·GWT 설정·테스트 포트·테스트 자산 복사)는 규약이다.
//
// 규약이라 새 화면을 더할 때 고칠 곳이 하나다: 아래 표에 한 줄. 포트는 그 순서로 매겨져
// 두 모듈이 같은 포트를 잡는 일이 없다(옛 방식에서 손으로 세던 것).
// 버전 카탈로그(gradle/libs.versions.toml) — 타입 생성 접근자(libs.…)는 이 스크립트에서
// 서브프로젝트에 쓸 수 없어 이름으로 찾는다. 버전이 적히는 곳은 여전히 한 곳이다.
val cat = extensions.getByType<VersionCatalogsExtension>().named("libs")

val libraries = listOf("console-bridge", "ui-components")

// 화면 모듈 → 그 모듈이 컴파일하는 GWT 모듈 이름(테스트 모듈은 여기에 "Test"가 붙는다).
// 이름이 규약을 벗어나는 곳(map: MapScreen/MapTest)은 짝을 명시한다.
val screens = linkedMapOf(
    "shell-ui" to ("dev.sayaya.magi.Shell" to "dev.sayaya.magi.ShellTest"),
    "companion-ui" to ("dev.sayaya.magi.Companion" to "dev.sayaya.magi.CompanionTest"),
    "coding-agent-ui" to ("dev.sayaya.magi.Coding" to "dev.sayaya.magi.CodingTest"),
    "knowledge-ui" to ("dev.sayaya.magi.Knowledge" to "dev.sayaya.magi.KnowledgeTest"),
    "board-ui" to ("dev.sayaya.magi.Board" to "dev.sayaya.magi.BoardTest"),
    "map-ui" to ("dev.sayaya.magi.MapScreen" to "dev.sayaya.magi.MapTest"),
    "access-ui" to ("dev.sayaya.magi.Access" to "dev.sayaya.magi.AccessTest"),
    "meeting-ui" to ("dev.sayaya.magi.Meeting" to "dev.sayaya.magi.MeetingTest"),
    "settings-ui" to ("dev.sayaya.magi.Settings" to "dev.sayaya.magi.SettingsTest"),
)

subprojects {
    group = "dev.sayaya.magi"
    version = "0.0.1"
    tasks.withType<Test> { useJUnitPlatform() }
}

// 공통 GWT 설정 — 라이브러리와 화면이 같이 쓰는 부분.
//
// 타입 없이 설정한다(withGroovyBuilder): 이 스크립트는 플러그인을 적용하지 않는 루트라
// 확장의 타입이 클래스패스에 없다. 이름으로 부르는 대신 얻는 것은 파일 하나로 끝나는
// 구성이고, 잃는 것은 오타를 컴파일이 잡아 주는 일 — 그래서 빌드가 전 모듈을 실제로
// 컴파일하는지로 검증한다(./gradlew build).
fun Project.gwtCommon(modules: List<String>) {
    extensions.getByName("gwt").withGroovyBuilder {
        setProperty("gwtVersion", "2.13.1")
        setProperty("sourceLevel", "auto")
        setProperty("generateJsInteropExports", true)
        setProperty("modules", modules)
        "compiler" {
            setProperty("strict", true)
        }
    }
}

// 라이브러리 모듈: 소비 모듈의 gwtCompile이 .gwt.xml과 자바 소스를 클래스패스에서 찾으므로
// jar에 소스를 싣는다(GWT 규약, handbook activity와 동일).
libraries.forEachIndexed { i, name ->
    project(":$name") {
        apply(plugin = "org.jetbrains.kotlin.jvm")
        apply(plugin = "dev.sayaya.gwt")
        dependencies {
            if (name != "console-bridge") add("implementation", project(":console-bridge"))
            add("implementation", cat.findBundle("sayaya-web").get())
            add("annotationProcessor", cat.findLibrary("lombok").get())
        }
        gwtCommon(listOf(if (name == "console-bridge") "dev.sayaya.magi.ConsoleBridge"
                         else "dev.sayaya.magi.UiComponents"))
        tasks.named<Jar>("jar") {
            from(project.the<SourceSetContainer>()["main"].allSource)
            duplicatesStrategy = DuplicatesStrategy.WARN
        }
    }
}

// 화면 모듈: 의존성·GWT·테스트 포트·테스트 자산이 전부 여기서 온다.
screens.entries.forEachIndexed { i, (name, both) ->
    val (module, testModule) = both
    project(":$name") {
        apply(plugin = "org.jetbrains.kotlin.jvm")
        apply(plugin = "dev.sayaya.gwt")
        dependencies {
            add("implementation", project(":console-bridge"))
            add("implementation", project(":ui-components"))
            add("implementation", cat.findBundle("sayaya-web").get())
            add("annotationProcessor", cat.findLibrary("lombok").get())
            add("annotationProcessor", cat.findLibrary("dagger-compiler").get())
            add("testImplementation", cat.findBundle("test-web").get())
            add("testAnnotationProcessor", cat.findLibrary("dagger-compiler").get())
        }
        gwtCommon(listOf(module))
        val warDir = file("src/test/webapp")
        extensions.getByName("gwt").withGroovyBuilder {
            "devMode" {
                setProperty("modules", listOf(module, testModule))
                setProperty("war", warDir)
            }
            // 포트는 표의 순서로 — 손으로 세던 시절 두 모듈이 같은 포트를 잡은 적이 있다.
            "test" {
                setProperty("webPort", 18090 + i)
            }
        }
        // 테스트 페이지의 자산은 단일 원천에서 매 빌드 복사한다(스냅샷 드리프트 없음):
        // 머티리얼 번들과 콘솔 CSS, 그리고 이 모듈이 제 것으로 둔 스타일시트가 있으면 그것도.
        val copyTestAssets = tasks.register<Copy>("copyTestAssets") {
            from("${rootDir}/../../cmd/magi-web/vendor/material.js") { into("js") }
            // 스토어는 RxJS 위에 산다 — 테스트 페이지도 실제 번들을 문다(목이 아니라 그 파일).
            from("${rootDir}/../../cmd/magi-web/vendor/rxjs.js") { into("js") }
            from("${rootDir}/../../cmd/magi-web/page.css") { into("css"); rename { "console.css" } }
            val own = file("src/main/webapp")
            if (own.isDirectory) from(own) { include("*.css"); into("css") }
            into("src/test/webapp")
        }
        tasks.named("processTestResources") { dependsOn(copyTestAssets) }
    }
}

// 모든 모듈의 GWT 산출물을 한 서빙 루트로 모은다 — web/server -ui 의 기본 경로가 여기다.
tasks.register<Copy>("assembleConsole") {
    dependsOn(subprojects.map { "${it.path}:gwtCompile" })
    mustRunAfter(subprojects.map { "${it.path}:gwtTestCompile" })
    // 화면 모듈이 제 것으로 둔 자산(css 등)도 함께.
    subprojects.forEach { p -> from(p.projectDir.resolve("src/main/webapp")) }
    // 팔레트·표 CSS는 기존 콘솔의 단일 원천에서 매 빌드 복사한다.
    from("../../cmd/magi-web/page.css") { rename { "console.css" } }
    subprojects.forEach { p -> from(p.layout.buildDirectory.dir("gwt/war")) }
    into(layout.buildDirectory.dir("console"))
    duplicatesStrategy = DuplicatesStrategy.EXCLUDE
}
