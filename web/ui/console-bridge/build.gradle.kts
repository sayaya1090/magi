plugins {
    kotlin("jvm")
    id("dev.sayaya.gwt")
}
dependencies {
    implementation(libs.bundles.sayaya.web)
    annotationProcessor(libs.lombok)
}
gwt {
    gwtVersion = "2.13.0"
    sourceLevel = "auto"
    generateJsInteropExports = true
    compiler { strict = true }
    modules = listOf("dev.sayaya.magi.ConsoleBridge")
}

// GWT 라이브러리 규약(handbook activity와 동일): 소비 모듈의 gwtCompile이 이 모듈의
// .gwt.xml과 자바 소스를 클래스패스에서 찾으므로, jar에 소스 전체를 싣는다.
tasks.jar {
    from(sourceSets.main.get().allSource)
    duplicatesStrategy = DuplicatesStrategy.WARN
}
