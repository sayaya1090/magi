plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kotlin.serialization)
}

// IDE 가 도는 JDK 를 따른다. UnixDomainSocketAddress 가 16 부터라 바닥은 훨씬 낮지만,
// 목표는 이 코드가 IntelliJ 안에서 도는 것이므로 그쪽에 맞춘다.
kotlin { jvmToolchain(21) }

dependencies {
    implementation(libs.serialization.json)
    testImplementation(libs.junit)
    testRuntimeOnly(libs.junit.launcher)
}

tasks.test {
    useJUnitPlatform()
    // `SourceTextTest` 는 클래스가 아니라 **소스 글자**를 읽는다. 그게 입력으로 안 걸려 있으면
    // gradle 은 딴 모듈의 .kt 가 바뀌어도 이 작업을 UP-TO-DATE 로 건너뛰고, 그러면 검사는
    // 초록인데 **돈 적이 없다.** 실제로 그 상태를 한 번 만들어 봤다: 창 코드에 결함을 도로
    // 넣었는데 통과했다. 그래서 읽는 것을 그대로 입력으로 적는다.
    inputs.files(
        fileTree(rootProject.projectDir) {
            include("**/src/**/*.kt")
            exclude("**/build/**")
        }
    ).withPropertyName("scannedSources").withPathSensitivity(PathSensitivity.RELATIVE)
}
