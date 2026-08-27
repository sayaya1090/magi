package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dagger.Provides;
import dev.sayaya.magi.bridge.Demo;
import dev.sayaya.magi.client.interfaces.FrameElement;
import dev.sayaya.magi.client.interfaces.RailElement;
import dev.sayaya.magi.client.interfaces.api.DemoRosterSource;
import dev.sayaya.magi.client.interfaces.api.FetchRosterSource;
import dev.sayaya.magi.client.interfaces.api.ScriptModuleLoader;
import dev.sayaya.magi.client.usecase.FrameView;
import dev.sayaya.magi.client.usecase.ModuleLoader;
import dev.sayaya.magi.client.usecase.RailView;
import dev.sayaya.magi.client.usecase.RosterSource;

import javax.inject.Provider;
import javax.inject.Singleton;

/** 유스케이스 포트 → interfaces 구현 바인딩. 데모면 이 모듈 제 목을, 테스트는 가짜를 문다. */
@Module
public abstract class ShellModule {
    @Binds abstract ModuleLoader loader(ScriptModuleLoader impl);
    @Binds abstract RailView rail(RailElement impl);
    @Binds abstract FrameView frame(FrameElement impl);

    /**
     * 명단과 스트림의 주인은 셸이다 — 그래서 그 목도 여기 하나뿐이다. 데모에서 다른 화면들이
     * 명단을 보는 것은 진짜 콘솔에서와 같은 길(RosterSharing)이다.
     */
    @Provides
    @Singleton
    static RosterSource roster(Provider<FetchRosterSource> live, Provider<DemoRosterSource> demo) {
        return Demo.on() ? demo.get() : live.get();
    }
}
