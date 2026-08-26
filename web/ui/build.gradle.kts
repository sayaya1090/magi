plugins {
    id("java")
    kotlin("jvm") version "2.3.0" apply false
    id("dev.sayaya.gwt") version "2.2.9.5" apply false
}
subprojects {
    group = "dev.sayaya.magi"
    version = "0.0.1"
    // 테스트 공통: JUnit Platform (kotest 러너 포함)
    tasks.withType<Test> { useJUnitPlatform() }
}

// 모든 화면 모듈의 GWT 산출물을 한 서빙 루트로 모은다.
// web/server -ui 의 기본 경로가 여기(build/console)다: console.html + shell/ + fleet/ + companion/ …
tasks.register<Copy>("assembleConsole") {
    dependsOn(subprojects.map { "${it.path}:gwtCompile" })
    // gwtTestCompile도 같은 war 디렉토리를 출력으로 선언한다 — 지금은 테스트 모듈이 없어 산출물이
    // 없지만, Gradle 검증은 순서 선언을 요구한다. 산출물이 생기면 EXCLUDE보다 앞서 걸러야 한다.
    mustRunAfter(subprojects.map { "${it.path}:gwtTestCompile" })
    from("shell-ui/src/main/webapp")
    // 팔레트·플릿 CSS는 기존 콘솔의 단일 원천에서 매 빌드 복사한다 — 스냅샷 드리프트 없음.
    // (theme.css 생성 원장 행이 해결되면 이 복사는 그 산출물로 대체된다.)
    from("../../cmd/magi-web/page.css") { rename { "console.css" } }
    subprojects.forEach { p -> from(p.layout.buildDirectory.dir("gwt/war")) }
    into(layout.buildDirectory.dir("console"))
    duplicatesStrategy = DuplicatesStrategy.EXCLUDE
}
