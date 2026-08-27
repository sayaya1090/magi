package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.MeetingSource;

import javax.inject.Singleton;

/** 테스트 그래프: 회선 자리에 가짜를 문다. */
@Module
public abstract class MeetingTestModule {
    @Singleton
    @Binds
    abstract MeetingSource source(FakeMeetingSource impl);
}
