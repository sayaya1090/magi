package dev.sayaya.magi.client;

import dagger.Component;
import dev.sayaya.magi.client.interfaces.SettingsElement;

import javax.inject.Singleton;

@Singleton
@Component(modules = SettingsModule.class)
public interface SettingsComponent {
    SettingsElement settingsElement();
}
