package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.CompanionElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = CompanionModule.class)
public interface CompanionComponent {
    CompanionElement companionElement();
}
