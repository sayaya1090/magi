package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.MapSource;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

@Singleton
public class FetchMapSource implements MapSource {
    @Inject
    public FetchMapSource() {}

    @Override
    public void fleet(Consumer<Object> cb) { Console.fetchList("/fleet", cb::accept); }

    @Override
    public void handoffs(Consumer<Object> cb) { Console.fetchList("/handoffs", cb::accept); }
}
