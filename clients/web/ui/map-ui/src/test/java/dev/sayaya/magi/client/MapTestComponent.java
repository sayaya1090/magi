package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.MapElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = MapTestModule.class)
public interface MapTestComponent {
    MapElement mapElement();
}
