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
        modules = listOf("dev.sayaya.magi.Access", "dev.sayaya.magi.AccessTest")
        war = file("src/test/webapp")
    }
    generateJsInteropExports = true
    compiler { strict = true }
    test { webPort = 18096 }
    modules = listOf("dev.sayaya.magi.Access")
}

val copyTestAssets = tasks.register<Copy>("copyTestAssets") {
    from("../../../cmd/magi-web/vendor/material.js") { into("js") }
    from("../../../cmd/magi-web/page.css") { into("css"); rename { "console.css" } }
    into("src/test/webapp")
}
tasks.named("processTestResources") { dependsOn(copyTestAssets) }
