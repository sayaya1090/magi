plugins {
    kotlin("jvm")
    id("dev.sayaya.gwt")
}
dependencies {
    implementation(project(":console-bridge"))
    implementation(project(":ui-components"))
    implementation(libs.bundles.sayaya.web)
    annotationProcessor(libs.lombok)
    annotationProcessor(libs.dagger.compiler)
    testImplementation(libs.bundles.test.web)
    testAnnotationProcessor(libs.dagger.compiler)
}
gwt {
    gwtVersion = "2.13.0"
    sourceLevel = "auto"
    devMode {
        modules = listOf("dev.sayaya.magi.Companion", "dev.sayaya.magi.CompanionTest")
        war = file("src/test/webapp")
    }
    generateJsInteropExports = true
    compiler { strict = true }
    test { webPort = 18092 }
    modules = listOf("dev.sayaya.magi.Companion")
}

// 테스트 페이지의 머티리얼 번들·콘솔 CSS는 기존 콘솔의 단일 원천에서 복사한다.
// companion.css 는 이 모듈 자신의 것 — 원천이 src/main/webapp 이다.
val copyTestAssets = tasks.register<Copy>("copyTestAssets") {
    from("../../../cmd/magi-web/vendor/material.js") { into("js") }
    from("../../../cmd/magi-web/page.css") { into("css"); rename { "console.css" } }
    from("src/main/webapp/companion.css") { into("css") }
    into("src/test/webapp")
}
tasks.named("processTestResources") { dependsOn(copyTestAssets) }
