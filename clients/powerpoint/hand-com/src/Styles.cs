namespace Magi.Ppt.Hand;

/// <summary>
/// PowerPoint 붙박이 표 스타일의 GUID. COM 의 Table.ApplyStyle 은 이름이 아니라 이 id 를 받는다(Office.js 는 이름).
/// 표는 MS 문서 「TableStyle.Id」의 것 — ⚠ 실물 PowerPoint 2021 에서 아직 안 재 봤다. 여기 없는 이름은 거절한다;
/// 이름을 대충 맞춰 엉뚱한 스타일을 입히는 것보다 「모른다」가 낫다.
/// </summary>
public static class TableStyles
{
    public static readonly IReadOnlyDictionary<string, string> ById = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase)
    {
        ["NoStyleNoGrid"] = "{2D5ABB26-0587-4C30-8999-92F81FD0307C}",
        ["NoStyleTableGrid"] = "{5940675A-B579-460E-94D1-54222C63F5DA}",
        ["ThemedStyle1Accent1"] = "{3C2FFA5D-87B4-456A-9821-1D502468CF0F}",
        ["ThemedStyle1Accent2"] = "{284E427A-3D55-4303-BF80-6455036E1DE7}",
        ["ThemedStyle1Accent3"] = "{69C7853C-536D-4A76-A0AE-DD22124D55A5}",
        ["ThemedStyle1Accent4"] = "{775DCB02-9BB8-47FD-8907-85C794F793BA}",
        ["ThemedStyle1Accent5"] = "{35758FB7-9AC5-4552-8A53-C91805E547FA}",
        ["ThemedStyle1Accent6"] = "{08FB837D-C827-4EFA-A057-4D05807E0F7C}",
        ["ThemedStyle2Accent1"] = "{D7AC3CCA-C797-4891-BE02-D94E43425B78}",
        ["ThemedStyle2Accent2"] = "{69CF1AB2-1976-4502-BF36-3FF5EA218861}",
        ["ThemedStyle2Accent3"] = "{8A107856-5554-42FB-B03E-39F5DBC370BA}",
        ["ThemedStyle2Accent4"] = "{0505E3EF-67EA-436B-97B2-0124C06EBD24}",
        ["ThemedStyle2Accent5"] = "{22838BEF-8BB2-4498-84A7-C5851F593DF1}",
        ["ThemedStyle2Accent6"] = "{7E9639D4-E3E2-4D34-9284-5A2195B3D0D7}",
        ["LightStyle1"] = "{9D7B26C5-4107-4FEC-AEDC-1716B250A1EF}",
        ["LightStyle1Accent1"] = "{3B4B98B0-60AC-42C2-AFA5-B58CD77FA1E5}",
        ["LightStyle2"] = "{7E9639D4-E3E2-4D34-9284-5A2195B3D0D8}",
        ["LightStyle2Accent1"] = "{5FD0F851-EC5A-4D38-B0AD-8093EC10F338}",
        ["MediumStyle1"] = "{616DA210-FB5B-4158-BA91-B3EF6ED9CE71}",
        ["MediumStyle1Accent1"] = "{B301B821-A1FF-4177-AEE7-76D212191A09}",
        ["MediumStyle2"] = "{073A0DAA-6AF3-43AB-8588-CEC1D06C72B9}",
        ["MediumStyle2Accent1"] = "{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}",
        ["MediumStyle2Accent2"] = "{21E4AEA4-8DFA-4A89-87EB-49C32662AFE0}",
        ["MediumStyle2Accent3"] = "{F5AB1C69-6EDB-4FF4-983F-18BD219EF322}",
        ["MediumStyle2Accent4"] = "{00A15C55-8517-42AA-B614-E9B94910E393}",
        ["MediumStyle2Accent5"] = "{7DF18680-E054-41AD-8BC1-D1AEF772440D}",
        ["MediumStyle2Accent6"] = "{93296810-A885-4BE3-A3E7-6D5BEEA58F35}",
        ["MediumStyle3"] = "{8EC20E35-A176-4012-BC5E-935CFFF8708E}",
        ["MediumStyle4"] = "{D7AC3CCA-C797-4891-BE02-D94E43425B79}",
        ["DarkStyle1"] = "{E8034E78-7F5D-4C2E-B375-FC64B27BC917}",
        ["DarkStyle2"] = "{5202B0CA-FC54-4496-8BCA-5EF66A818D29}",
    };
}
