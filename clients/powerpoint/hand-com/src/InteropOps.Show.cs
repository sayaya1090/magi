using System.Runtime.Versioning;
using Office = Microsoft.Office.Core;
using PowerPoint = Microsoft.Office.Interop.PowerPoint;

namespace Magi.Ppt.Hand;

/// <summary>
/// 발표 설정 — 화면 전환·장 숨기기·슬라이드 크기·구역·PDF. Office.js(PowerPointApi)에는 이 넷의 길이 없고 COM 에는 있다.
/// 그래서 2021 의 이 손이 365 작업창보다 더 하는 자리다(2026-09-27 사용자 요청: 「2021 에서 추가 연결 가능한 도구」).
/// </summary>
[SupportedOSPlatform("windows")]
public sealed partial class InteropOps
{
    /// <summary>화면 전환 이름 → PowerPoint 효과. 방향이 있는 효과는 PowerPoint 의 기본 방향 하나로 둔다. Hand.TransitionEffects 와 같은 이름들(시험이 대조한다).</summary>
    internal static readonly IReadOnlyDictionary<string, PowerPoint.PpEntryEffect> TransitionMap = new Dictionary<string, PowerPoint.PpEntryEffect>
    {
        ["none"] = PowerPoint.PpEntryEffect.ppEffectNone, ["cut"] = PowerPoint.PpEntryEffect.ppEffectCut,
        ["fade"] = PowerPoint.PpEntryEffect.ppEffectFadeSmoothly, ["push"] = PowerPoint.PpEntryEffect.ppEffectPushUp,
        ["wipe"] = PowerPoint.PpEntryEffect.ppEffectWipeLeft, ["split"] = PowerPoint.PpEntryEffect.ppEffectSplitVerticalOut,
        ["reveal"] = PowerPoint.PpEntryEffect.ppEffectRevealSmoothLeft, ["cover"] = PowerPoint.PpEntryEffect.ppEffectCoverLeft,
        ["uncover"] = PowerPoint.PpEntryEffect.ppEffectUncoverLeft, ["random_bars"] = PowerPoint.PpEntryEffect.ppEffectRandomBarsHorizontal,
        ["shape"] = PowerPoint.PpEntryEffect.ppEffectCircleOut, ["zoom"] = PowerPoint.PpEntryEffect.ppEffectZoomIn,
        ["dissolve"] = PowerPoint.PpEntryEffect.ppEffectDissolve, ["flash"] = PowerPoint.PpEntryEffect.ppEffectFlashbulb,
        ["blinds"] = PowerPoint.PpEntryEffect.ppEffectBlindsVertical, ["checkerboard"] = PowerPoint.PpEntryEffect.ppEffectCheckerboardAcross,
        ["clock"] = PowerPoint.PpEntryEffect.ppEffectWheel1Spoke, ["vortex"] = PowerPoint.PpEntryEffect.ppEffectVortexLeft,
        ["ripple"] = PowerPoint.PpEntryEffect.ppEffectRippleCenter, ["honeycomb"] = PowerPoint.PpEntryEffect.ppEffectHoneycomb,
        ["glitter"] = PowerPoint.PpEntryEffect.ppEffectGlitterDiamondLeft, ["shred"] = PowerPoint.PpEntryEffect.ppEffectShredStripsIn,
        ["switch"] = PowerPoint.PpEntryEffect.ppEffectSwitchLeft, ["flip"] = PowerPoint.PpEntryEffect.ppEffectFlipLeft,
        ["gallery"] = PowerPoint.PpEntryEffect.ppEffectGalleryLeft, ["cube"] = PowerPoint.PpEntryEffect.ppEffectCubeLeft,
        ["doors"] = PowerPoint.PpEntryEffect.ppEffectDoorsVertical, ["box"] = PowerPoint.PpEntryEffect.ppEffectBoxLeft,
        ["orbit"] = PowerPoint.PpEntryEffect.ppEffectOrbitLeft, ["pan"] = PowerPoint.PpEntryEffect.ppEffectPanUp,
        ["ferris_wheel"] = PowerPoint.PpEntryEffect.ppEffectFerrisWheelLeft, ["conveyor"] = PowerPoint.PpEntryEffect.ppEffectConveyorLeft,
        ["rotate"] = PowerPoint.PpEntryEffect.ppEffectRotateLeft, ["window"] = PowerPoint.PpEntryEffect.ppEffectWindowVertical,
        ["random"] = PowerPoint.PpEntryEffect.ppEffectRandom,
    };

    public Transition ReadTransition(int n)
    {
        var t = pres.Slides[n].SlideShowTransition;
        var effect = TransitionMap.FirstOrDefault(kv => kv.Value == t.EntryEffect).Key
            ?? "other:" + t.EntryEffect.ToString().Replace("ppEffect", ""); // 사람이 손으로 건 방향 변형 — 이름을 지어내지 않고 그대로 댄다
        return new Transition(effect, Math.Round(t.Duration, 2), t.AdvanceOnClick == Office.MsoTriState.msoTrue,
            t.AdvanceOnTime == Office.MsoTriState.msoTrue ? Math.Round(t.AdvanceTime, 2) : null);
    }

    public void SetTransition(int n, Transition want)
    {
        var t = pres.Slides[n].SlideShowTransition;
        if (!want.Effect.StartsWith("other:", StringComparison.Ordinal)) t.EntryEffect = TransitionMap[want.Effect];
        if (want.Effect != "none" && want.Duration > 0) t.Duration = (float)want.Duration;
        t.AdvanceOnClick = want.OnClick ? Office.MsoTriState.msoTrue : Office.MsoTriState.msoFalse;
        if (want.AdvanceAfter is double s) { t.AdvanceOnTime = Office.MsoTriState.msoTrue; t.AdvanceTime = (float)s; }
        else t.AdvanceOnTime = Office.MsoTriState.msoFalse;
    }

    public bool IsHidden(int n) => pres.Slides[n].SlideShowTransition.Hidden == Office.MsoTriState.msoTrue;
    public void SetHidden(int n, bool hidden) => pres.Slides[n].SlideShowTransition.Hidden = hidden ? Office.MsoTriState.msoTrue : Office.MsoTriState.msoFalse;

    public (double Width, double Height) SlideSize() => (Math.Round(pres.PageSetup.SlideWidth, 2), Math.Round(pres.PageSetup.SlideHeight, 2));
    public void SetSlideSize(double width, double height)
    {
        // PowerPoint 는 크기를 바꾸면 도형을 비율대로 옮기고 키운다(맞춤) — 사람이 대화 상자에서 「맞춤 확인」을 고른 것과 같다.
        pres.PageSetup.SlideWidth = (float)width;
        pres.PageSetup.SlideHeight = (float)height;
    }

    public IReadOnlyList<SectionInfo> Sections()
    {
        var sp = pres.SectionProperties; var list = new List<SectionInfo>();
        for (var i = 1; i <= sp.Count; i++) list.Add(new SectionInfo(sp.Name(i), sp.SlidesCount(i) > 0 ? sp.FirstSlide(i) : 0, sp.SlidesCount(i)));
        return list;
    }

    public bool AddSection(int n, string name)
    {
        var sp = pres.SectionProperties;
        for (var i = 1; i <= sp.Count; i++)
            if (sp.SlidesCount(i) > 0 && sp.FirstSlide(i) == n) { sp.Rename(i, name); return false; }
        sp.AddBeforeSlide(n, name);
        return true;
    }

    public void DeleteSection(int index) => pres.SectionProperties.Delete(index, false);

    public string? FilePath => string.IsNullOrEmpty(pres.Path) ? null : pres.FullName;

    // SaveCopyAs 는 덱의 경로를 안 바꾼다(SaveAs 와 다르다) — 사람이 쓰던 파일은 그대로 그 파일이다.
    public void ExportPdf(string path) => pres.SaveCopyAs(path, PowerPoint.PpSaveAsFileType.ppSaveAsPDF);
}
