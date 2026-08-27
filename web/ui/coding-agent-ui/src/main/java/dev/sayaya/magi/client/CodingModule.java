package dev.sayaya.magi.client;

import dagger.Module;
import dagger.Provides;
import dev.sayaya.magi.bridge.Demo;
import dev.sayaya.magi.client.interfaces.api.BridgeCompanionSource;
import dev.sayaya.magi.client.interfaces.api.DemoCompanionSource;
import dev.sayaya.magi.client.interfaces.api.DemoWorkspaceSource;
import dev.sayaya.magi.client.interfaces.api.FetchWorkspaceSource;
import dev.sayaya.magi.client.usecase.CompanionSource;
import dev.sayaya.magi.client.usecase.WorkspaceSource;

import javax.inject.Provider;
import javax.inject.Singleton;

/** 데모면 이 모듈 제 목을 문다 — 목은 그 모듈과 함께 실려 그 배포에 따라붙는다. */
@Module
public abstract class CodingModule {
    @Provides
    @Singleton
    static CompanionSource source(Provider<BridgeCompanionSource> live, Provider<DemoCompanionSource> demo) {
        return Demo.on() ? demo.get() : live.get();
    }

    @Provides
    @Singleton
    static WorkspaceSource workspace(Provider<FetchWorkspaceSource> live, Provider<DemoWorkspaceSource> demo) {
        return Demo.on() ? demo.get() : live.get();
    }
}
