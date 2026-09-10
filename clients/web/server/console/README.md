# 콘솔 빌드 산출물 배포 디렉토리

본 디렉토리는 기본적으로 빈 상태로 커밋되어 유지됩니다. 여기에 배치되는 파일은 `clients/web/ui`를 GWT로 컴파일한 웹 콘솔 정적 자산이며, 빌드에는 JDK 및 Gradle 환경이 필요합니다. Go 기반 저장소를 클론한 사용자가 별도의 Java 도구체인 없이도 바이너리를 빌드할 수 있도록 콘솔 정적 자산 조립은 **CI 파이프라인에서 수행**합니다.

```sh
cd clients/web/ui && ./gradlew assembleConsole
cp -R clients/web/ui/build/console/. clients/web/server/console/
```

위와 같이 산출물을 복사한 후 `go build ./clients/web/server`를 실행하면 콘솔 자산이 바이너리 내에 임베드됩니다. 산출물을 배치하지 않고 빌드해도 **성공적으로 빌드**되며, 바이너리는 독립 BFF(Backend For Frontend) 서버로 온전히 동작합니다. 콘솔 자산이 없는 경우 `/` 접근 시 콘솔이 미포함된 빌드임을 안내합니다. 자산이 비어 있는 상태는 오류가 아니며 정상적으로 지원되는 모드입니다(`internal/webassets`의 아이콘 스프라이트 정책과 동일).

로컬 개발 환경에서는 콘솔을 바이너리에 매번 임베드하지 않고 디스크 경로에서 직접 서빙하는 방식을 권장합니다:

```sh
magi-web -console clients/web/ui/build/console
```

본 README 파일은 Go의 `go:embed` 지시자가 빈 디렉토리를 임베드하지 못해 발생하는 빌드 실패를 방지하는 자리지기(placeholder) 역할도 겸합니다.
