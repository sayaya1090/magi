package dev.sayaya.magi.client;

import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.BrowserTongue;
import dev.sayaya.magi.client.interfaces.api.FetchWords;
import dev.sayaya.magi.client.usecase.TongueSource;
import dev.sayaya.magi.client.usecase.WordSource;

/** 포트 둘에 브라우저를 문다 — 이 페이지가 바깥에 묻는 것은 이 브라우저가 고른 말과, 그 말의 팩뿐이다. */
@Module
public abstract class LandingModule {
    @dagger.Binds
    abstract TongueSource tongues(BrowserTongue impl);

    @dagger.Binds
    abstract WordSource words(FetchWords impl);
}
