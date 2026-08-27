package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.FetchMeetingSource;
import dev.sayaya.magi.client.usecase.MeetingSource;

import javax.inject.Singleton;

/** 포트에 회선을 문다 — 테스트는 같은 자리에 가짜를 문다. */
@Module
public abstract class MeetingModule {
    @Singleton
    @Binds
    abstract MeetingSource source(FetchMeetingSource impl);
}
