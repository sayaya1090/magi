package dev.sayaya.magi.domain;
import dev.sayaya.magi.client.domain.CompanionType;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
class CompanionTypeTest {
 @Test void applicationsUseSharedChat() {
  for(String role:new String[]{"figma","mail","slack","powerpoint","word","excel"}) {
   CompanionType type=CompanionType.byId(role);
   assertEquals(role,type.id);assertEquals("application",type.module);assertEquals("type."+role,type.labelKey);
  }
 }
 @Test void unknownAndCodingRemainCompatible() {
  assertSame(CompanionType.CODING,CompanionType.byId("1"));
  assertSame(CompanionType.CODING,CompanionType.byId("coding"));
  assertSame(CompanionType.CODING,CompanionType.byId(null));
  assertSame(CompanionType.CODING,CompanionType.byId("unknown"));
 }
}
