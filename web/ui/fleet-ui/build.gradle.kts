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
        modules = listOf("dev.sayaya.magi.Fleet", "dev.sayaya.magi.FleetTest")
        war = file("src/test/webapp")
    }
    generateJsInteropExports = true
    compiler { strict = true }
    test { webPort = 18090 }
    modules = listOf("dev.sayaya.magi.Fleet")
}

// 테스트 페이지의 머티리얼 번들은 기존 콘솔의 단일 원천에서 복사한다 — 스냅샷 드리프트 없음.
val copyTestJs = tasks.register<Copy>("copyTestJs") {
    from("../../../cmd/magi-web/vendor/material.js")
    into("src/test/webapp/js")
}
tasks.named("processTestResources") { dependsOn(copyTestJs) }
