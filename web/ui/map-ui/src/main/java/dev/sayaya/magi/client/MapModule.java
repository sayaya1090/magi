package dev.sayaya.magi.client;

import dagger.Module;
import dagger.Provides;
import dev.sayaya.magi.bridge.Demo;
import dev.sayaya.magi.client.interfaces.api.DemoMapSource;
import dev.sayaya.magi.client.interfaces.api.FetchMapSource;
import dev.sayaya.magi.client.usecase.MapSource;

import javax.inject.Provider;
import javax.inject.Singleton;

/** 포트에 회선을 문다 — 데모면 이 모듈이 함께 싣는 제 목을 문다(테스트는 가짜를 문다). */
@Module
public abstract class MapModule {
    @Provides
    @Singleton
    static MapSource source(Provider<FetchMapSource> live, Provider<DemoMapSource> demo) {
        return Demo.on() ? demo.get() : live.get();
    }
}
