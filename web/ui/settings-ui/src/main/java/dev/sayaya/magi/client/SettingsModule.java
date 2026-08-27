package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.FetchSettingsSource;
import dev.sayaya.magi.client.usecase.SettingsSource;

import javax.inject.Singleton;

@Module
public abstract class SettingsModule {
    @Singleton
    @Binds
    abstract SettingsSource source(FetchSettingsSource impl);
}
