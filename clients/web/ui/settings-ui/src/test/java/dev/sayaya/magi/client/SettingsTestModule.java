package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.SettingsSource;

import javax.inject.Singleton;

@Module
public abstract class SettingsTestModule {
    @Singleton
    @Binds
    abstract SettingsSource source(FakeSettingsSource impl);
}
