# 앱 대화 모듈

웹 콘솔에서 앱 컴패니언의 대화·컴포저 슬롯을 제공하는 GWT 자식 모듈입니다. `Conversation.gwt.xml`을 통해 기존 대화 코드를 공유하며 파일·Git 작업공간은 마운트하지 않습니다. 승인·질문·레이아웃·모듈 전환은 부모가 관리합니다.

`clients/web/ui`에서 `./gradlew :application-ui:gwtCompile :companion-ui:gwtCompile :coding-agent-ui:gwtCompile :shell-ui:test`로 빌드합니다. `clients/web/e2e` 의존성을 설치한 뒤 저장소 루트에서 `node clients/web/ui/application-ui/test/compiled-playwright.mjs`를 실행합니다. 브라우저 검증은 실제 컴파일 모듈과 모의 콘솔 데이터를 1440px·390px에서 사용합니다. 타입 전환, 늦은 코딩 모듈 로드, 코딩으로 돌아왔을 때 파일 컨트롤 복구, 앱의 파일 요청 0건을 확인합니다.

배포 자산에는 `assembleConsole`이 이 모듈을 포함합니다. 소스를 변경하면 다시 컴파일해야 합니다. 구형 미지 콘솔 타입의 코딩 폴백은 유지하며, 도구 권한은 대상 데몬의 역할 제한으로 강제합니다. 이 테스트는 실제 앱 계정 연결을 검증하지 않습니다.
