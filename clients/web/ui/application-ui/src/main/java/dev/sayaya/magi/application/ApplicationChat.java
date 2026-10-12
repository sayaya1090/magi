package dev.sayaya.magi.application;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.PaneSharing;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.client.CodingComponent;
import dev.sayaya.magi.client.DaggerCodingComponent;

/** Application companions share conversation mechanics, with no workspace pane. */
public final class ApplicationChat implements EntryPoint {
    @Override public void onModuleLoad() {
        CodingComponent component = DaggerCodingComponent.create();
        PaneSharing.nextFor("application", "centre", (Render) frame -> {
            component.conversation().mount(frame);
            return true;
        });
        PaneSharing.nextFor("application", "dock", (Render) frame -> {
            component.conversation().mountComposer(frame);
            return true;
        });
    }
}
