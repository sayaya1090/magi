# 이 디렉토리에는 조립된 콘솔이 들어온다

빈 채로 커밋되어 있다. 여기 실리는 것은 `web/ui`를 GWT로 컴파일한 산출물이고, 그것을 만드는
데는 JDK와 gradle이 필요하다 — Go 저장소를 클론한 사람이 그것까지 갖춰야 빌드가 되는 것은
`go build`가 지켜 온 약속을 깨는 일이라, 조립은 **CI가 한다**.

    cd web/ui && ./gradlew assembleConsole
    cp -R web/ui/build/console/. cmd/magi-web/console/

그렇게 채운 뒤 `go build ./cmd/magi-web` 하면 콘솔이 바이너리 안으로 들어간다. 채우지 않고
빌드해도 **성공한다** — 그 바이너리는 BFF로서 온전히 동작하고, `/`만이 "이 빌드에는 콘솔이
들어 있지 않다"고 제 입으로 말한다. 비어 있는 것은 실패가 아니라 지원되는 상태다(같은 규칙이
`internal/webassets`의 아이콘 스프라이트에도 걸린다).

개발 중이라면 조립본을 굽지 말고 디스크에서 바로 서빙하는 편이 빠르다:

    magi-web -console web/ui/build/console

이 README 자신은 `go:embed`가 디렉토리를 실을 수 있게 하는 자리지기이기도 하다. Go는 빈
디렉토리를 임베드하지 못하고, 파일이 하나도 없으면 **빌드가 깨진다**.
