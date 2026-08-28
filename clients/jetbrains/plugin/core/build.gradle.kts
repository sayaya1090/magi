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

tasks.test { useJUnitPlatform() }
