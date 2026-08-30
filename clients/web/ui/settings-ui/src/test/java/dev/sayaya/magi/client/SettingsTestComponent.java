package dev.sayaya.magi.client;

import dagger.Component;
import dev.sayaya.magi.client.interfaces.SettingsElement;

import javax.inject.Singleton;

@Singleton
@Component(modules = SettingsTestModule.class)
public interface SettingsTestComponent {
    SettingsElement settingsElement();
}
