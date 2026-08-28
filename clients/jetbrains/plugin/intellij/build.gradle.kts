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
}
