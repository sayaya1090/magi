// 두 모듈로 나눈 이유는 취향이 아니라 시험 가능성이다. core 는 IntelliJ SDK 를 모르므로
// 화면 없이 계약을 시험할 수 있고, SDK 를 못 받는 자리에서도 컴파일된다.
// 의존은 intellij → core 한 방향뿐이고, 그 반대가 생기면 core 가 SDK 를 끌어온다.
rootProject.name = "magi-ide"

include("core", "intellij")
