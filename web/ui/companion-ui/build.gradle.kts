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
}
gwt {
    gwtVersion = "2.13.0"
    sourceLevel = "auto"
    generateJsInteropExports = true
    compiler { strict = true }
    modules = listOf("dev.sayaya.magi.Companion")
}
