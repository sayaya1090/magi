package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.LandingElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = LandingModule.class)
public interface LandingComponent {
    LandingElement landing();
}
