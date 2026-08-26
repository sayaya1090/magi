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
        modules = listOf("dev.sayaya.magi.Shell", "dev.sayaya.magi.ShellTest")
        war = file("src/test/webapp")
    }
    generateJsInteropExports = true
    compiler { strict = true }
    test { webPort = 18091 }
    modules = listOf("dev.sayaya.magi.Shell")
}

// 테스트 페이지의 머티리얼 번들·콘솔 CSS는 기존 콘솔의 단일 원천에서 복사한다.
val copyTestAssets = tasks.register<Copy>("copyTestAssets") {
    from("../../../cmd/magi-web/vendor/material.js") { into("js") }
    // 스크림·레일 지오메트리는 CSS가 만든다 — 없으면 스크림이 0크기라 누를 수도 없다.
    from("../../../cmd/magi-web/page.css") { into("css"); rename { "console.css" } }
    from("src/main/webapp/shell.css") { into("css") }
    into("src/test/webapp")
}
tasks.named("processTestResources") { dependsOn(copyTestAssets) }
