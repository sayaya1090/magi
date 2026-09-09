package dev.sayaya.magi.client;

import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.BrowserTongue;
import dev.sayaya.magi.client.usecase.TongueSource;

/** 포트 하나에 브라우저를 문다 — 이 페이지가 바깥에 묻는 것은 그 하나뿐이다. */
@Module
public abstract class LandingModule {
    @dagger.Binds
    abstract TongueSource tongues(BrowserTongue impl);
}
