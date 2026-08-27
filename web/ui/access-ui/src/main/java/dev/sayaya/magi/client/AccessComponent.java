package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.AccessElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = AccessModule.class)
public interface AccessComponent {
    AccessElement accessElement();
}
