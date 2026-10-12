package dev.sayaya.magi.domain;
import dev.sayaya.magi.client.domain.CompanionType;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
class CompanionTypeTest {
 @Test void applicationsUseSharedChat() {
  for(String role:new String[]{"design","communication","presentation","document","spreadsheet"}) {
   CompanionType type=CompanionType.byId(role);
   assertEquals(role,type.id);assertEquals("application",type.module);assertEquals("type."+role,type.labelKey);
  }
 }
 @Test void applicationClientsResolveToBusinessKinds() {
  assertSame(CompanionType.COMMUNICATION,CompanionType.byId("mail"));
  assertSame(CompanionType.COMMUNICATION,CompanionType.byId("slack"));
  assertSame(CompanionType.DESIGN,CompanionType.byId("figma"));
  assertSame(CompanionType.PRESENTATION,CompanionType.byId("powerpoint"));
  assertSame(CompanionType.DOCUMENT,CompanionType.byId("word"));
  assertSame(CompanionType.SPREADSHEET,CompanionType.byId("excel"));
 }
 @Test void unknownAndCodingRemainCompatible() {
  assertSame(CompanionType.CODING,CompanionType.byId("1"));
  assertSame(CompanionType.CODING,CompanionType.byId("coding"));
  assertSame(CompanionType.CODING,CompanionType.byId(null));
  assertSame(CompanionType.CODING,CompanionType.byId("unknown"));
 }
}
