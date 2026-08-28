import org.jetbrains.intellij.platform.gradle.IntelliJPlatformType

plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.intellij)
}

kotlin { jvmToolchain(21) }

// 릴리스 레인이 -PpluginVersion=… 으로 태그에서 넘긴다. 붙이지 않으면 dev 다 — 손으로 만든
// 빌드가 릴리스처럼 보이지 않게(Makefile 의 VERSION 이 같은 이유로 --dirty 를 단다).
version = (findProperty("pluginVersion") as String?) ?: "0.0.0-dev"

repositories {
    mavenCentral()
    intellijPlatform { defaultRepositories() }
}

dependencies {
    // 한 방향이다. core 는 이쪽을 모른다.
    implementation(project(":core"))
    intellijPlatform { intellijIdea(libs.versions.idea.get()) }
}

intellijPlatform {
    pluginConfiguration {
        // 바닥은 UnixDomainSocketAddress(JDK 16)가 아니라 SDK 쪽이 정한다. 2026.1 = 261.
        ideaVersion {
            sinceBuild = "261"
            untilBuild = provider { null }
        }
    }

    // 이 플러그인이 IDEA 전용이 아니라는 것을 **재서** 안다.
    //
    // plugin.xml 이 `com.intellij.modules.platform` 에만 의존하므로 모든 JetBrains IDE 에 설치된다
    // — 는 선언에서 유도한 말이지 잰 말이 아니었다. 컴파일은 IDEA SDK 에 대고 하니까(상위집합이라
    // 그렇게 한다) IDEA 에서 도는 것만 확인되고, PyCharm 에만 없는 클래스를 실수로 쓰면 그날 처음
    // 안다. Plugin Verifier 가 그것을 잡는다: 선언한 sinceBuild 범위의 각 IDE 에 대고 바이트코드가
    // 부르는 것이 다 있는지 본다.
    //
    // PyCharm 을 고른 이유는 그것이 IDEA 에서 가장 먼 흔한 IDE 라서다 — 자바가 없다. 여기를
    // 통과하면 WebStorm·GoLand 도 사실상 통과한다.
    //
    // `PyCharmCommunity` 가 아니라 `PyCharm` 인 것은 IDEA 에서 배운 것과 같은 함정이다:
    // Community 판은 2025.3(253)부터 따로 배포되지 않는다. 검증기가 그 사실을 에러로 알려 준다
    // ("PyCharm Community (PC) is no longer published since 2025.3"). 제품 둘이 그러니 규칙으로
    // 읽는 편이 낫다 — 이 트리가 대는 IDE 는 통합판 이름을 쓴다.
    pluginVerification {
        ides {
            recommended()
            create(IntelliJPlatformType.PyCharm, libs.versions.idea.get())
        }
    }
}
